I've finished a thorough review of the whole codebase. Here's my assessment.

## Overview

This is a genuinely ambitious and well-organized project — ~9,100 lines of Go, clean package boundaries (`graph`, `indexer`, `server`, `mcp`, `agent`, `ingestion`, `git`, `progress`, `logging`), good doc comments, a working build, `go vet` clean, and 29 passing tests. The README/docs are strong. For a v0.1.0 written in a day, the architecture is impressive.

That said, I found several **real bugs** (some verified with reproducers), a data race, and a number of design issues that will bite you as the project grows. I verified the most important ones rather than just eyeballing them.

---

## 🔴 Bugs (confirmed)

### 1. Data race: `GetNode` returns the live internal `*Node`
**`internal/graph/engine.go:257-269`**

`GetNode` returns a pointer straight out of `g.nodes` under an `RLock`. Meanwhile `UpdateNode` (line 317) mutates `node.Properties` in place. So any concurrent `GET /nodes/{uuid}` racing a `PUT /nodes/{uuid}` is undefined behavior. I confirmed this with the race detector:

```
WARNING: DATA RACE
Write at ... graph.(*GraphEngine).UpdateNode() engine.go:318
Previous read at ... runtime.mapiterinit()  (iterating Properties map)
```

**Fix:** `GetNode` (and `FindNodesByType`, `FindNodeByName`, `Stats`) must return **deep copies**, not pointers into the live store. The `Stats()` method already does this correctly for the maps — apply the same pattern to nodes. This is the single most important fix.

### 2. Duplicate CALLS edges — indexer runs the same logic twice
**`internal/indexer/indexer.go:420-456` and `463-523`**

`indexFile()` already creates CALLS edges inline (lines 420–456), then `Scan()` calls `extractCallsEdges()` (line 246) which **re-walks every file and creates them again**. My reproducer: one file with one function call produced **4 CALLS edges**. The graph grows quadratically with re-imports.

**Fix:** Pick one. Delete the inline CALLS block in `indexFile`, or delete `extractCallsEdges()` and the call to it in `Scan`.

### 3. "Dynamic call tracking" is silently broken
**`internal/indexer/indexer.go:436`**

```go
idx.graph.CreateEdge(fileNodes[0].UUID, "", graph.EdgeCalls, ...)
```

It passes `""` as the target UUID. `CreateEdge` validates that the target node exists and rejects it. So unresolved/dynamic calls are never recorded — the feature described in the README doesn't work, and the error is swallowed.

**Fix:** Create an "Unresolved" node first, or skip dynamic edges, or relax `CreateEdge`. But don't silently drop.

### 4. `dev up` mangles its arguments
**`cmd/root.go:299-302`**

```go
args = append([]string{"serve"}, os.Args[3:]...)
proc, err := os.StartProcess(os.Args[0], append([]string{"cortex", "serve"}, args...), ...)
```

The final argv becomes `cortex serve serve <os.Args[3:]>` — "serve" is duplicated, and it ignores `cfg.Server.Port` entirely (the spawned `serve` will re-read config, so the printed port may be wrong). It also spawns via `os.StartProcess` with all stdio set to `nil`, so the background server's logs vanish. `processRunning(pid)` using `signal 0` also can't distinguish "our server" from "any reused PID" — on a busy machine `dev down` could signal an unrelated process.

**Fix:** Just run `exec.Command(os.Args[0], "serve", "--port", port)` with a log file for stdio, and consider storing a checksum/cookie alongside the PID to verify ownership.

### 5. `devLogsCmd` always returns an error
**`cmd/root.go:1276-1287`**

The "logs" command body literally just returns an error explaining logs aren't written to a file. A stub that always fails is worse than not having the command — either implement log redirection or remove it.

### 6. `goal` and `agent run` panic when run outside a project
**`cmd/root.go:816` and `942`**

`PersistentPreRun` leaves `cfg == nil` when there's no `cortex.yaml`. But `goalCmd` dereferences `cfg.Server.Host` directly:
```go
ServerURL: fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port),
```
and `agentRunCmd` does `cfg.LLM[...]`. Running `cortex goal "..."` outside an initialized project → nil pointer panic. Same for `agent run`.

**Fix:** Nil-check `cfg` (fall back to `DefaultConfig()`) in every command that uses it, like `serveCmd` already does.

### 7. Server binds to `127.0.0.1` regardless of config
**`internal/server/server.go:123`**

```go
addr := fmt.Sprintf("127.0.0.1:%d", s.port)
```

`s.port` is honored but the host is hardcoded. So `cortex config set server.host 0.0.0.0` does nothing, despite being a documented setting. The `Server` struct doesn't even store the host.

**Fix:** Pass host into `New()` and use `cfg.Server.Host`.

### 8. CORS allows only one hard-coded origin
**`internal/server/server.go:818`**

`Access-Control-Allow-Origin: http://127.0.0.1:8741` is hardcoded. Any dashboard served from a different port/host, or any browser-based MCP/REST client, gets blocked. Make it configurable.

### 9. Custom `min()` duplicates the builtin
**`cmd/root.go:1547`, `internal/agent/agent.go:513`, `internal/mcp/mcp.go:617`**

You declared `func min(a, b int) int` in three places. Since Go 1.21 `min` is a builtin (you're on 1.22). These are harmless shadowing now, but will bite if you ever go to 1.21+ generics and it's just noise. Delete them.

---

## 🟠 Concurrency / correctness concerns

### 10. `emitEvent` is called while holding `g.mu`
**`internal/graph/engine.go:246, 332, 438`**

Every mutation calls `emitEvent` *inside* the `g.mu.Lock()`. `emitEvent` takes `g.subMu.RLock()` and sends on channels (non-blocking, good). Currently no deadlock because `Subscribe` doesn't take `g.mu`. But it's a latent hazard — any future code that locks `g.mu` from within an event handler or subscriber path will deadlock. Better: collect the event, release `g.mu`, then emit.

### 11. `Unsubscribe` closes the channel, but `emitEvent` can still write to it
**`internal/graph/engine.go:553-568` vs `586-595`**

`emitEvent` iterates `g.subscribers[topic]` under `subMu.RLock()` and does a non-blocking send. `Unsubscribe` takes `subMu.Lock()` and closes the channel. These are mutually exclusive, so no direct race — but the `eventBridge` in the server does `defer s.engine.Unsubscribe(...)` while another goroutine is reading from the same channel. If the unsubscribe+close happens between a bridge read and the next, fine — but the ordering across the graph is fragile. Worth a careful second look and maybe not closing the channel in `Unsubscribe`.

### 12. WebSocket broadcast holds the lock across a network write
**`internal/server/server.go:198-210`**

`broadcastToSubscribers` calls `conn.WriteMessage` (a blocking network write) while holding `wsMu.RLock()`. One slow/dead client stalls broadcasts to all clients. Comment says "remove failed connections outside the lock to prevent deadlock" — but the *write* is still inside the lock. Use a per-connection send channel + writer goroutine.

---

## 🟡 Design / robustness

### 13. The indexer is regex-based and will produce a lot of false edges
You acknowledge tree-sitter is the plan. Some current regex problems:
- The Go `MethodPattern` and `FunctionPattern` are nearly identical, so every method is **also** counted as a function → double-counted entities.
- The JS `MethodPattern` `(\w+)\s*\([^)]*\)\s*\{` matches `if (x) {` and `for (...) {` — tons of false methods.
- `callPattern` `([a-zA-Z_]\w*)\s*\(` matches `if (`, `for (`, type casts, macro args, etc. The `isKeyword` denylist is too small (no `while`, `return`, `sizeof`, `print`, etc. for other languages).
- `ImportPattern` for JS matches any quoted string, not just imports.

This is the core value proposition of the product — regex is going to give users a noisy, misleading graph. **Prioritize tree-sitter (or at least `go/parser` for Go).**

### 14. `truncate` slices by bytes, not runes
**`internal/server/server.go:809`** — `s[:n]` on UTF-8 (your UI uses emoji/🧠) can split a multibyte char and emit invalid JSON. Use `utf8`/`[]rune`.

### 15. Error responses are inconsistent
Sometimes `404` is returned for bad UUIDs and `400` for parse errors — but `handleUpdateNode` returns `404` for *any* update error including version conflicts (`UpdateNode` failure), and `handleDeleteNode` returns `404` for a malformed UUID that's really a `400`. Map errors to proper status codes.

### 16. `dockerSandbox.ReadFile/WriteFile` path check is bypassable
**`internal/agent/docker.go:155, 168`**

```go
if !strings.HasPrefix(filepath.Clean(fullPath), filepath.Clean(s.workDir)) {
```
`HasPrefix` is the wrong check: workdir `/project` would wrongly accept `/project-evil/x`. Use `filepath.Rel` + check it doesn't start with `..`. The same pattern is in `sandbox.go:189`. Since these gates run code an LLM generates, get this right.

### 17. `Sandbox.resolvePath` defeats the sandbox for absolute paths
**`internal/agent/sandbox.go:159-165`**

```go
if filepath.IsAbs(clean) {
    return filepath.Join(s.workDir, clean)   // /etc/passwd -> workDir/etc/passwd
}
```
Joining an absolute path *after* `workDir` doesn't append it in Go's `filepath.Join` — `Join("/work", "/etc/passwd")` returns `/work/etc/passwd`, so it's contained, but the intent reads like the author thought absolute paths get rejected. `ValidatePathInSandbox` then re-validates. It's not directly exploitable but the two functions disagree on semantics and it's confusing. Consolidate to one canonical resolver.

### 18. JSON persistence will lose data on concurrent save / crash
`SaveToFile` writes directly to the target path (`os.WriteFile`). A crash mid-write corrupts `graph.json` and you lose the whole memory layer. Write to a temp file in the same dir, then `os.Rename` (atomic on POSIX). Given the product is *literally about not losing memory*, this matters.

### 19. MCP server isn't spec-compliant in ways that matter
- `protocolVersion` is `2024-11-05` (old). Current is `2025-06-18`.
- No `notifications/initialized` handling, no `ping`, no `resources/*`, no `prompts/*`.
- The stdio scanner buffers up to 1MB per line — fine — but you don't handle JSON-RPC **notifications** (requests with no `id`); you'll try to send a response with a null id.
- Tool result wrapping is correct (content array), good.

It'll work with permissive clients but may fail with stricter ones.

### 20. Hardcoded `github.com/EBTURKgit/cortex` strings in user-facing output
**`internal/git/workflow.go:37`** — `TaskBranch` prefixes branches with `github.com/EBTURKgit/cortex/`. That's *your* fork's path, baked into every branch name for every user. Use a neutral prefix like `cortex/`.

### 21. `bin/cortex` is a 13MB committed binary
It's not in git (good — gitignore catches it), but it's sitting in the working tree. Minor, just flagging.

---

## 🟢 Style / minor

- **gofmt not applied**: `gofmt -l` flags 16 of your 20 Go files (misaligned struct tags, wrong backtick spacing in `Long:` strings). CI runs `go vet` but not `gofmt -l`. Add `gofmt -l .` (or `gofumports`/`gofumpt`) as a CI step and run `gofmt -w .`.
- **`min` redefined** in 3 files (see #9).
- **Unused import workaround**: `internal/ingestion/ingestion.go:294` has `var _ = uuid.New` — you don't use the uuid package there, just delete the import and the blank assignment.
- **`Stats()` returns a struct by value but embeds maps** — you copy the maps (good), but callers still see `NodeTypes`/`EdgeTypes` mutated if... actually you copy them, fine. Just keep that pattern.
- **Logging is DEBUG by default in `main.go:42`** and extremely chatty (trace entry/exit on every graph op). For a server that may index thousands of files, this floods stderr. Default to INFO, add `--verbose`/`-v`.
- **`agent.Run` busy-loops on `default:`** (`agent.go:248`): the `select` has a `default` that calls `ReadMessage()` (blocking), so it's not a true spin — but mixing `default` with blocking reads in a `select` is brittle and starves the `ctx.Done()`/signal cases while blocked. Restructure to read in its own goroutine and feed a channel.
- **`cortex.yaml` committed with `name: test`** — fine for now, but consider whether the repo's own config should ship.
- **`docker-compose.yml`** has `version: "3.8"` which Compose v2 ignores (deprecated field) and the main service is entirely commented out — slightly confusing as a starting point.
- **`processRunning`** uses `signal 0` which returns success for zombie processes owned by you; combine with a `/proc/<pid>/cmdline` check on Linux.
- The README says "tests 29 passing" as a badge — that number will drift; consider generating it from CI instead.

---

## Suggested priority order

1. **#1 (data race)** — return copies from graph reads. Foundational.
2. **#18 (atomic save)** + **#2 (duplicate edges)** — data integrity / correctness of the core product.
3. **#6, #7, #4** — nil-deref panics and `dev`/host bugs that hurt first-run experience.
4. **#13 (indexer accuracy)** — the graph quality *is* the product.
5. **#10/#12 (concurrency in events/WebSocket)** — scales poorly.
6. Everything else (MCP compliance, sandbox hardening, gofmt, logging level).

---

implement + add regression tests.
