# Cortex + opencode Integration Guide

Connect Cortex's graph-based project memory to opencode so your AI agents have persistent, cross-session context about your codebase.

## How It Works

```
┌─────────────────────────────────────────────┐
│  opencode                                   │
│  ┌──────────────────────────────────────┐   │
│  │  Agent (DeepSeek, Claude, GPT-4, etc)│   │
│  │  calls MCP tools to query the graph  │   │
│  └──────────────┬───────────────────────┘   │
│                 │                            │
│  MCP Protocol   │  cortex_search_code        │
│  (stdio)        │  cortex_graph_query        │
│                 │  cortex_context_for_file   │
│                 │  cortex_context_for_symbol │
│                 │  cortex_get_project_struct │
│                 ▼                            │
│  ┌──────────────────────────────────────┐   │
│  │  cortex mcp (subprocess)             │   │
│  │  Loads graph from .cortex/graph.json │   │
│  └──────────────┬───────────────────────┘   │
└─────────────────┼───────────────────────────┘
                  │
                  ▼
         ┌──────────────────┐
         │  .cortex/        │
         │  ├── graph.json  │  ← Your indexed codebase
         │  ├── config.yaml │
         │  └── progress.json│
         └──────────────────┘
```

## Prerequisites

- **Go 1.22+** (for building Cortex)
- **opencode** (v1.17+ recommended)
- Your source code project

## Installation

### Step 1: Build Cortex

```bash
git clone https://github.com/your-org/cortex.git
cd cortex
make build
```

This creates `bin/cortex` — a 13MB standalone binary with no dependencies.

### Step 2: (Optional) Install system-wide

```bash
sudo cp bin/cortex /usr/local/bin/
```

Or add to your PATH:
```bash
export PATH="$PATH:$PWD/bin"
```

### Step 3: Verify

```bash
cortex version
# Cortex v0.1.0
```

## Configuration

### Option A: Automatic (recommended)

Run the setup in your project directory:

```bash
cd /path/to/your/project

# Initialize cortex project
cortex init my-project

# Index your codebase into the graph
cortex import .

# Verify the graph has data
cortex query stats
```

This creates:
- `cortex.yaml` — project configuration
- `.cortex/graph.json` — the indexed knowledge graph

### Option B: Manual configuration

Create `cortex.yaml` in your project root:

```yaml
project:
  name: my-project
  root_path: "."
  language: auto
  ignore:
    - .git
    - node_modules
    - vendor
    - .cortex
    - dist

server:
  host: 127.0.0.1
  port: 8741
  storage: memory

llm:
  default:
    provider: ollama
    model: codellama:7b
    endpoint: http://localhost:11434
```

Then import:
```bash
cortex import .
```

## opencode MCP Setup

### Step 1: Configure opencode to use Cortex

Create or edit `~/.config/opencode/opencode.jsonc`:

```jsonc
{
  "$schema": "https://opencode.ai/config.json",

  // MCP (Model Context Protocol) servers provide additional
  // tools and context to opencode agents.
  "mcp": {
    // Cortex: Graph-based project memory for AI coding tools.
    "cortex": {
      "type": "local",
      "command": ["/path/to/cortex", "mcp"],
      "cwd": "/path/to/your/project",
      "enabled": true
    }
  }
}
```

**Replace `/path/to/cortex`** with the actual path to your `cortex` binary
(e.g., `/home/user/cortex/bin/cortex`).

**Replace `/path/to/your/project`** with your project root
(e.g., `/home/user/my-project`).

### Step 2: Verify opencode detects Cortex

```bash
opencode mcp list
```

Expected output:
```
┌  MCP Servers
│
●  ✓ cortex  connected
│      /path/to/cortex mcp
│
└  1 server(s)
```

If you see a red `✗` instead of a green `✓`, check the path in your config.

### Step 3: Test a tool call

```bash
cd /path/to/your/project
opencode run "Use the cortex MCP graph_query tool with query 'stats'"
```

Expected output:
```
The graph contains 247 nodes: 1 Project, 9 Files, 141 Functions, 96 Methods.
```

## Available MCP Tools

Once configured, opencode agents can call these Cortex tools automatically:

### `cortex_graph_query`

Query the knowledge graph by type or symbol name.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| `query` | string | yes | `"stats"`, a node type (`"Function"`), or a symbol name |

**Examples the LLM might use:**
- `"stats"` → Returns graph statistics
- `"Function"` → All functions in the project
- `"UserService"` → Find nodes matching that name

### `cortex_search_code`

Full-text search across all indexed code entities.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| `query` | string | yes | Search term |

**Example result:**
```json
{
  "count": 4,
  "query": "CreateNode",
  "results": [
    {"name": "CreateNode", "type": "Function", "language": "go"},
    {"name": "handleCreateNode", "type": "Method", "language": "go"}
  ]
}
```

### `cortex_context_for_symbol`

Get definition, callers, and callees for a function or class.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| `symbol_name` | string | yes | Function, class, or method name |

**Example result:**
```json
{
  "symbol": {"name": "CreateNode", "type": "Method"},
  "callers": [],
  "callees": []
}
```

*Note: CALLS edges are populated when using tree-sitter level parsing.*

### `cortex_context_for_file`

Get all functions, classes, and dependencies in a file.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| `file_path` | string | yes | Relative path (e.g., `"internal/graph/engine.go"`) |

### `cortex_get_project_structure`

Get the high-level module and file tree.

**Parameters:** None

## Usage Examples

### During a coding session

Open opencode in your project:

```bash
opencode
```

The agent can now query Cortex automatically. Example prompts:

> "What functions are in `internal/server/server.go`?"
> → Cortex returns the function list from the graph.

> "Find all functions named `CreateNode` in the codebase."
> → Cortex searches the graph and returns matches.

> "Show me the project structure."
> → Cortex returns the file/module tree.

> "What functions call `handleCreateNode`?"
> → Cortex checks CALLS edges in the graph.

### CLI-only queries

You don't need opencode to query the graph:

```bash
cortex query stats
cortex query Function
cortex query CreateNode
cortex graph nodes
cortex status
```

## Re-indexing

### After code changes

Cortex saves the graph to `.cortex/graph.json`. To update it after code changes:

```bash
cortex import .
```

This re-scans the codebase and rebuilds the graph. The file watcher for automatic re-indexing is available (`cortex watch`).

### Multiple projects

Each project has its own `.cortex/` directory. Just run `cortex import .` in each project.

## Troubleshooting

### MCP server shows "disconnected"

Check the command path in `~/.config/opencode/opencode.jsonc`:
```bash
ls -la /path/to/cortex  # Does this file exist?
/path/to/cortex version # Does it run?
```

### Graph is empty after import

```bash
cortex import . --verbose
cortex query stats       # Verify nodes were created
ls -la .cortex/graph.json # Verify file was saved
```

### opencode doesn't show Cortex tools

```bash
opencode mcp list        # Is cortex listed?
cat ~/.config/opencode/opencode.jsonc  # Is syntax correct?
```

### "Invalid schema" errors

Make sure you're running the latest built version:
```bash
cd /path/to/cortex/source
git pull
make build
cp bin/cortex /usr/local/bin/
```

## Architecture Notes

- **Graph data is persisted** to `.cortex/graph.json` as JSON
- **MCP protocol** uses JSON-RPC 2.0 over stdio
- **No server process needed** — `cortex mcp` starts on-demand per tool call
- **Zero external dependencies** — single Go binary
- **No API keys required** for the graph — LLM credentials are separate

## File Reference

| File | Purpose |
|------|---------|
| `~/.config/opencode/opencode.jsonc` | opencode configuration with MCP servers |
| `.cortex/graph.json` | Your project's knowledge graph (auto-generated) |
| `cortex.yaml` | Project configuration (auto-generated by `cortex init`) |
| `bin/cortex` | The Cortex binary |

## Quick Reference Card

```bash
# Setup
cortex init my-project     # Create project config
cortex import .            # Index codebase

# Query
cortex query stats         # Graph statistics
cortex query Function      # All functions
cortex query "myFunc"      # Find by name

# Dashboard
cortex status              # Project overview
cortex task list           # Task list
cortex graph nodes         # All nodes

# Server (for real-time updates)
cortex serve               # Start graph server

# MCP (for opencode)
cortex mcp                 # Run MCP server

# Progress
cortex progress            # Build progress
```
