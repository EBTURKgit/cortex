<p align="center">
  <img src="https://img.shields.io/badge/version-0.1.0-blue" alt="Version">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="License">
  <img src="https://img.shields.io/badge/Go-1.22%2B-purple" alt="Go">
  <img src="https://img.shields.io/badge/tests-29%20passing-brightgreen" alt="Tests">
</p>

<h1 align="center">🧠 Cortex</h1>
<p align="center"><strong>Persistent project memory for AI coding tools.</strong></p>
<p align="center">Give your AI assistants a permanent brain that remembers your codebase across every session.</p>

---

## The Problem

AI coding tools (opencode, Cursor, Claude Code, etc.) **forget everything when you close a session.** Ask an AI to build a feature today, and tomorrow it has zero memory of what it built. It re-reads your entire codebase from scratch. Every. Single. Time.

## The Solution

Cortex indexes your codebase into a **knowledge graph** — a permanent map of every function, class, file, and their connections. Your AI tools query this map instead of re-reading your files. Results are instant. Context is permanent.

```
Without Cortex:  AI reads 1000 files to answer "what does this function do?"
With Cortex:     AI asks Cortex → instant answer from the graph
```

## What Makes Cortex Different

| Feature | Cortex | Other tools |
|---------|--------|-------------|
| **Persistent memory** | ✅ Remembers across all sessions | ❌ Forget after each session |
| **Runs locally** | ✅ Single binary, zero deps | ❌ Often needs cloud APIs |
| **Universal** | ✅ Works with any MCP-compatible tool | ❌ Locked to one editor |
| **Project map** | ✅ Functions, classes, callers, callees | ❌ Just file content |
| **Real-time updates** | ✅ WebSocket + autosave | ❌ Manual refresh |
| **Built-in dashboard** | ✅ Interactive graph visualization | ❌ CLI only |
| **Task planning** | ✅ AI generates and tracks task plans | ❌ No planning |
| **Free** | ✅ No subscriptions, no API costs | ❌ Often paid |

## Quick Start

```bash
# Download or build, then:
cortex init               # Set up your project
cortex import .           # Index your codebase into the graph
cortex query stats        # See what Cortex found

# Start the web dashboard
cortex serve              # Open http://127.0.0.1:8741

# Connect your AI tool
cortex mcp                # MCP server for opencode/Cursor/Claude
```

## Features

### 📊 Code Indexing
- Scans 8 languages (Go, Python, PHP, JavaScript/TypeScript, Rust, Java, Ruby, C/C++)
- Extracts functions, classes, methods, interfaces, modules
- Creates CALLS, CONTAINS, IMPORTS edges between code entities
- Respects `.gitignore` and `.cortexignore`
- File watcher for auto-reindexing on changes

### 🔍 Smart Querying
- Search by symbol name, type, or file path
- MCP tools for AI assistants: `search_code`, `context_for_file`, `context_for_symbol`
- REST API for custom integrations
- Natural language query via `cortex query`

### 🖥️ Web Dashboard
- Interactive force-directed graph visualization
- Click any node to see properties + connected edges
- Kanban task board for project planning
- Real-time activity feed
- Stats overview

### 🤖 AI Agent Framework
- Built-in LLM clients for Ollama, OpenAI, Anthropic
- Specialist agents: architect, backend, frontend, database, QA, DevOps, security, docs
- Docker sandbox for secure agent execution
- Automated Git workflow (branch, commit, PR)
- `cortex goal` — AI plans and tracks your entire project

### 📝 Runtime Ingestion
- Log and trace ingestion endpoints
- Automatic error-to-function linking
- `get_recent_errors` MCP tool for debugging

## Documentation

| Document | Description |
|----------|-------------|
| [Why Cortex?](docs/WHY_CORTEX.md) | What this project does and how it's different |
| [Use Cases](docs/USE_CASES.md) | 10 scenarios for using Cortex |
| [Quick Start](docs/QUICKSTART.md) | Step-by-step guide for everyone |
| [Technical Reference](docs/TECHNICAL.md) | Architecture, commands, API, config |
| [opencode Integration](docs/opencode-integration.md) | Connect Cortex to opencode |
| [AI Implementation Guide](docs/AI_SETUP.md) | For AI agents building/maintaining Cortex |

## Installation

### From Source

```bash
git clone https://github.com/EBTURKgit/cortex.git
cd cortex
make build
```

### Pre-built Binaries

Download from [Releases](https://github.com/EBTURKgit/cortex/releases):
- `cortex-linux-amd64` — Linux
- `cortex-darwin-amd64` — macOS (Intel)
- `cortex-darwin-arm64` — macOS (Apple Silicon)
- `cortex-windows-amd64.exe` — Windows

## Commands

```
cortex init              Create a new cortex project
cortex import <path>     Index a codebase into the graph
cortex serve             Start the graph server + dashboard
cortex mcp               Run MCP server (for AI tools)
cortex query <question>  Query the graph
cortex goal "<goal>"     Generate a project plan
cortex watch [path]      Watch for changes and auto-reindex
cortex status            Show project overview
cortex task list/approve  Manage tasks
cortex agent run/status  Run and monitor AI agents
cortex project status    Show project info
cortex config get/set    View or change configuration
cortex doctor            Check system dependencies
cortex progress          Show build progress
```

## Use Cases

- **Solo developers** — AI remembers your project across weeks of development
- **Teams** — Shared Cortex server gives all AI tools the same context
- **Rapid prototyping** — AI builds features that match your existing patterns
- **Learning codebases** — Visual graph explorer helps understand unfamiliar projects
- **Debugging** — Runtime errors linked to the functions that caused them
- **Code review** — AI can analyze PRs with full project context

[See all 10 use cases →](docs/USE_CASES.md)

## Tech Stack

- **Language:** Go (1.22+)
- **Storage:** In-memory + JSON file persistence
- **Protocols:** MCP (Model Context Protocol), REST, WebSocket
- **Frontend:** D3.js (embedded in binary)
- **Dependencies:** Zero at runtime. Single 13MB binary.

## Project Status

Cortex is in active development. All core features are functional. See [to-do.md](to-do.md) for the roadmap.

## License

[MIT](LICENSE)

---

<p align="center">Built by <a href="https://github.com/EBTURKgit">EBTURKgit</a></p>
