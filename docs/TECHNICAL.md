# Cortex Technical Reference

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                     AI Coding Tools                         │
│  (opencode / Cursor / Claude Code / Custom)                 │
└─────────────────────┬───────────────────┬───────────────────┘
                      │ MCP Protocol      │ WebSocket / REST
                      ▼                   ▼
┌────────────────────────────┐  ┌────────────────────────────┐
│     MCP Server (stdio)     │  │  HTTP + WebSocket Server   │
│   - 6 tools for context    │  │  - REST API (CRUD nodes)   │
│   - JSON-RPC 2.0           │  │  - WebSocket (real-time)   │
│   - 0 external deps        │  │  - Port 8741 (default)     │
└────────────┬───────────────┘  └────────────┬────────────────┘
             │                               │
             ▼                               ▼
┌────────────────────────────────────────────────────────────┐
│                    Graph Engine                             │
│  16 node types, 15 edge types, in-memory + JSON persistence│
│  MaxNodes: 1,000,000 | Optimistic locking | Event pub/sub  │
├────────────────────────────────────────────────────────────┤
│  Nodes: Project, File, Module, Function, Class, Method,    │
│         Variable, Interface, Endpoint, DatabaseSchema,     │
│         LogEntry, TraceSpan, Task, Decision, Commit, Agent │
│  Edges: CONTAINS, IMPORTS, CALLS, INHERITS, IMPLEMENTS,   │
│         REFERENCES, DEFINES_ENDPOINT, MAPS_TO_TABLE,       │
│         TRIGGERED_BY, BELONGS_TO_TRACE, ASSIGNED_TO,      │
│         DEPENDS_ON, MOTIVATES, CHANGED_IN, PROPOSES_CHANGE,│
│         HAS_ERROR, RESOLVED_BY                             │
└──────────────────────────┬─────────────────────────────────┘
                           │
          ┌────────────────┼────────────────┐
          ▼                ▼                ▼
┌────────────────┐ ┌──────────────┐ ┌────────────────┐
│   Indexer      │ │  Ingestion   │ │  Agents        │
│  (regex-based) │ │  (log/trace) │ │  (optional)    │
│  - 6 languages │ │  - POST path │ │  - LLM clients │
│  - fsnotify    │ │  - error     │ │  - Docker sbox │
│  - timeout     │ │    linking   │ │  - Git auto    │
│  - .gitignore  │ │              │ │  - Specialist  │
└────────────────┘ └──────────────┘ └────────────────┘
```

## Command Reference

### Project Management
| Command | Description |
|---------|-------------|
| `cortex init <name>` | Create cortex.yaml in current directory |
| `cortex import <path>` | Index a codebase into the graph (full scan) |
| `cortex watch [path]` | Watch filesystem and auto-reindex on changes |
| `cortex project status` | Show current project info |
| `cortex project create <name>` | Create a Project node in the graph |
| `cortex goal "<description>"` | Generate a project plan from a natural language goal |

### Server
| Command | Description |
|---------|-------------|
| `cortex serve` | Start the HTTP + WebSocket server on port 8741 |
| `cortex dev up` | Start server in background |
| `cortex dev down` | Stop background server |
| `cortex dev status` | Check if server is running |

### Querying
| Command | Description |
|---------|-------------|
| `cortex query stats` | Graph statistics |
| `cortex query <name>` | Search indexed nodes by name |
| `cortex query <type>` | List all nodes of a type (Function, Class, etc.) |
| `cortex graph nodes [type]` | List all nodes with UUIDs |
| `cortex status` | Full project overview with stats |

### Tasks
| Command | Description |
|---------|-------------|
| `cortex task list` | List all tasks |
| `cortex task approve <id>` | Mark task as approved |
| `cortex task reject <id> [reason]` | Mark task as rejected |

### Agents
| Command | Description |
|---------|-------------|
| `cortex agent run <type>` | Start an agent (backend, frontend, database, etc.) |
| `cortex agent status` | List connected agents |

### Integration
| Command | Description |
|---------|-------------|
| `cortex mcp` | Start MCP server on stdio (for AI tools) |
| `cortex config get/set` | View/change configuration |
| `cortex doctor` | Check system dependencies |
| `cortex progress` | Show build progress |
| `cortex version` | Print version |

## MCP Tools (for AI Coding Assistants)

When an AI tool connects via MCP, it gets these tools:

| Tool | Description |
|------|-------------|
| `graph_query` | Query by type, name, or "stats" |
| `context_for_file` | Get functions/classes in a file |
| `context_for_symbol` | Get definition + callers + callees |
| `search_code` | Full-text search across indexed code |
| `get_project_structure` | Module/file tree overview |
| `get_recent_errors` | Recent ERROR-level log entries |

## REST API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Server health check |
| GET | `/stats` | Graph statistics |
| POST | `/nodes` | Create a node |
| GET | `/nodes/{uuid}` | Get a node |
| PUT | `/nodes/{uuid}` | Update a node |
| DELETE | `/nodes/{uuid}` | Delete a node |
| GET | `/nodes?type=X&name=Y` | Find nodes |
| POST | `/edges` | Create an edge |
| GET | `/edges?type=X` | Get edges by type |
| GET | `/nodes/{uuid}/edges` | Get edges for a node |
| GET | `/query?q=X` | Query by name/type/stats |
| POST | `/ingest/log` | Ingest a log entry |
| POST | `/ingest/trace` | Ingest a trace span |
| GET | `/graph-data` | All nodes + edges (for visualizer) |
| GET | `/tasks-data` | All tasks (for dashboard) |
| GET | `/events-data` | Recent activity events |
| GET | `/ws` | WebSocket (real-time events) |
| GET | `/` or `/graph` | Web dashboard |

## Configuration

### cortex.yaml
```yaml
project:
  name: my-project
  root_path: "."
  language: auto
  ignore: [.git, node_modules, vendor, .cortex, bin, dist]

server:
  host: 127.0.0.1
  port: 8741
  storage: memory
  db_path: .cortex/data

agents:
  manager:    { enabled: false, model: default }
  backend:    { enabled: false, model: default }
  frontend:   { enabled: false, model: default }
  # ...

llm:
  default:
    provider: ollama          # ollama, openai, anthropic
    model: codellama:7b
    endpoint: http://localhost:11434
```

## Environment Variables
- `CORTEX_LLM_<NAME>_API_KEY` — API keys for LLM providers
- `.env` file — loaded automatically at startup

## Build & Test

```bash
make build              # Build for current platform
make build-all          # Build for all platforms (linux/macos/windows)
make test               # Run all tests
make clean              # Clean build artifacts

# Cross-compile
make build-linux        # Linux amd64
make build-macos        # macOS amd64
make build-macos-arm    # macOS arm64 (Apple Silicon)
make build-windows      # Windows amd64
```

## Dependencies

- Go 1.22+ (build-time only)
- Runtime: zero external dependencies (single binary)
- Optional: Docker (for agent sandboxing), Ollama (for local LLM)

## Data Flow

1. `cortex import .` → Indexer scans files, creates nodes + edges in memory
2. Graph saved to `.cortex/graph.json` (JSON persistence)
3. `cortex serve` loads graph from disk, serves REST + WebSocket
4. AI tools connect via MCP (`cortex mcp`) and query the graph
5. Changes to the graph emit events via WebSocket to all subscribers
6. `cortex serve` autosaves every 60s and on graceful shutdown

## Edge Types

Node connections are tracked as directed edges:

- **CONTAINS** — File contains Function, Class contains Method
- **CALLS** — Function calls another function (static or dynamic)
- **IMPORTS** — File imports a module
- **INHERITS** — Class inherits from another class
- **IMPLEMENTS** — Class implements an interface
- **DEPENDS_ON** — Task depends on another task
- **TRIGGERED_BY** — Log entry was emitted by a function
- **HAS_ERROR** — Error-level log linked to a function
- **PROPOSES_CHANGE** — Agent proposes a code modification

## Indexer Language Support

| Language | Extensions | Extracts |
|----------|------------|----------|
| Go | .go | Functions, methods, interfaces, imports |
| PHP | .php | Functions, classes, methods, interfaces, namespaces |
| Python | .py | Functions, classes, methods, imports |
| JavaScript/TypeScript | .js, .jsx, .ts, .tsx | Functions, classes, methods, interfaces, imports |
| Rust | .rs | Functions, methods |
| Java | .java | Functions, classes, methods |
| Ruby | .rb | Functions, classes, methods |
| C/C++ | .c, .cpp, .h, .hpp | Functions |

## Security Features

- Docker sandbox: `--network none`, `--read-only`, `--cap-drop ALL`
- Path traversal protection in sandbox ReadFile/WriteFile
- Request body size limits (10MB max)
- WebSocket read limits (10MB max)
- Optimistic locking prevents stale writes
- .gitignore and .cortexignore respected during indexing
- Per-file indexing timeout (30s) prevents hangs
