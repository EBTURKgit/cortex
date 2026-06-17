# Cortex — AI Implementation Guide

This file is written for **AI coding agents** (like yourself) to understand and implement the Cortex system. Read this if you need to build, extend, or maintain Cortex.

## What Is Cortex?

Cortex is a Go application that provides **persistent project memory for AI coding tools.** It indexes source code into a knowledge graph and exposes it via MCP protocol, allowing AI assistants to query project context without re-reading files.

## Core Architecture

### Three Layers

1. **Graph Engine** (`internal/graph/`) — In-memory labeled property graph
   - Nodes: 16 types (Project, File, Function, Class, etc.)
   - Edges: 17 types (CONTAINS, CALLS, IMPORTS, etc.)
   - CRUD operations with optimistic locking
   - Event pub/sub for change data capture
   - JSON file persistence (save/load)

2. **Server** (`internal/server/`) — HTTP + WebSocket
   - REST API for CRUD + queries
   - WebSocket for real-time events
   - Ingestion endpoints for logs/traces
   - Web dashboard (D3.js force graph + task board + activity feed)

3. **MCP Server** (`internal/mcp/`) — Model Context Protocol
   - 6 tools: graph_query, context_for_file, context_for_symbol, search_code, get_project_structure, get_recent_errors
   - JSON-RPC 2.0 over stdio
   - Used by opencode, Cursor, Claude Code

### Supporting Systems

- **Indexer** (`internal/indexer/`) — Regex-based code scanner for 8 languages
- **Agent Framework** (`internal/agent/`) — LLM clients, sandbox, specialist agents
- **Ingestion** (`internal/ingestion/`) — Log/trace processing pipeline
- **Git** (`internal/git/`) — Git operations + PR workflow automation
- **Config** (`internal/config/`) — YAML config + .env + validation
- **Logging** (`internal/logging/`) — Structured leveled logging with Trace entry/exit

## Data Flow

```
User runs "cortex import ."
  → Indexer scans files, creates nodes + edges
  → Graph saved to .cortex/graph.json

User runs "cortex serve"
  → Loads graph from disk
  → Serves REST API on :8741
  → WebSocket for live events
  → Autosaves every 60s

AI tool connects via MCP
  → User configures tool to run "cortex mcp"
  → MCP server receives JSON-RPC requests
  → Queries graph engine for answers
  → Returns structured results

User runs "cortex goal 'build a blog'"
  → ManagerAgent plans using LLM
  → Creates task tree in graph
  → Specialist agents execute tasks
  → Progress tracked in graph
```

## Key Types

```go
// Node — single entity in the graph
type Node struct {
    UUID       string
    Type       string  // "Function", "File", "Class", etc.
    Properties map[string]interface{}
    CreatedAt  time.Time
    UpdatedAt  time.Time
    Version    int
}

// Edge — directed relationship between two nodes
type Edge struct {
    SourceUUID string
    TargetUUID string
    Type       string  // "CALLS", "CONTAINS", etc.
    Properties map[string]interface{}
    CreatedAt  time.Time
}

// ChangeEvent — emitted on every mutation
type ChangeEvent struct {
    Type       string  // "create", "update", "delete"
    NodeType   string
    EdgeType   string
    EntityID   string
}
```

## Extending Cortex

### Adding a New Node Type

1. Add constant in `internal/graph/engine.go`
2. Add to `validNodeTypes()` map
3. Add indexer support in `internal/indexer/indexer.go`
4. Add to `ListSpecialists()` if agent-facing

### Adding a New MCP Tool

1. Add `MCPTool` entry in `handleToolsList()` in `internal/mcp/mcp.go`
2. Add handler function (e.g., `handleNewTool()`)
3. Add case in `handleToolsCall()` switch
4. Wrap result in `{ content: [{ type: "text", text: ... }] }` format

### Adding a New Language to the Indexer

1. Add file extension mapping in `SupportedLanguages` map
2. Add parser with regex patterns in `registerParsers()`
3. Patterns needed: Function, Class, Method, Interface (optional), Import (optional), Module (optional)

### Adding a New Agent Specialist

1. Add entry in `SpecialistRegistry` in `internal/agent/specialist.go`
2. Define system prompt and allowed tools
3. The base agent framework handles the rest

## Testing

- Unit tests: `go test ./internal/graph/...` (15 tests)
- Integration tests: `go test ./tests/...` (14 tests — HTTP, MCP, WebSocket, graph CRUD)
- Tests use `httptest.NewServer` for HTTP, `srv.SetIO` for MCP
- Benchmark: `BenchmarkGraphCreateNodes`, `BenchmarkGraphFindByName`

## Build Tags

- No build tags currently used
- Tree-sitter integration planned but blocked by missing C library
- CGO not required — pure Go binary

## Common Patterns

### Creating a CLI Command

```go
var myCmd = &cobra.Command{
    Use:   "mycommand <arg>",
    Short: "One-line description",
    RunE: func(cmd *cobra.Command, args []string) error {
        // Implementation
        return nil
    },
}

func init() {
    RootCmd.AddCommand(myCmd)
}
```

### Adding a REST Endpoint

```go
// In setupRoutes():
s.router.Get("/my-endpoint", s.handleMyEndpoint)

// Handler:
func (s *Server) handleMyEndpoint(w http.ResponseWriter, r *http.Request) {
    defer logging.Trace("handleMyEndpoint")()
    result := doSomething()
    writeJSON(w, http.StatusOK, result)
}
```

### Querying the Graph

```go
// Find by type
nodes := engine.FindNodesByType(graph.NodeTypeFunction)

// Find by name (case-insensitive)
nodes := engine.FindNodeByName("CreateUser")

// Get edges
edges := engine.GetNodeEdges(nodeUUID, "outgoing", graph.EdgeCalls)

// Subscribe to changes
ch := engine.Subscribe("all")
event := <-ch
```

### Saving/Loading Graph

```go
// Save
engine.SaveToFile(".cortex/graph.json")

// Load
engine, _ := graph.LoadFromFile(".cortex/graph.json")
```
