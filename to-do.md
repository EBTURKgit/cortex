# Project Cortex — Build To-Do

> **Audience:** AI coders who want persistent, graph-based project memory for their existing AI coding tools (opencode, Cursor, Claude Code, etc.).
> Cortex is first a **memory/knowledge layer** that plugs into the tools you already use. The autonomous agent swarm is an optional add-on.
> Every component must be usable from a developer's own machine.

---

## Phase 0: Developer Onboarding & Tooling

### 0.1 `cortex` CLI
- [ ] **Init command** — `cortex init` scaffolds `cortex.yaml` in current directory
- [ ] **Import command** — `cortex import <path>` scans an existing codebase and builds the full graph (File, Function, Class, Module, edges). This is the primary onboarding flow for existing projects.
- [ ] **Config management** — `cortex config set/get` for LLM providers, API keys, model choices
- [ ] **Dev mode** — `cortex dev up/down` starts/stops all cortex services locally (Docker Compose wrapper)
- [ ] **Project commands** — `cortex project create/list/status`
- [ ] **Task commands** — `cortex task list/approve/reject/logs`
- [ ] **Agent commands** — `cortex agent status/logs/restart` (only if agent swarm enabled)
- [ ] **Interactive session** — `cortex shell` opens a chat-like interface where developer sends goals and sees agent progress
- [ ] **Query CLI** — `cortex query "what does function X do?"` asks the graph via LLM, returns a natural-language answer

### 0.2 Configuration System
- [ ] **`cortex.yaml`** — project-level config file
  - LLM provider(s) per agent role (e.g., `agents.backend.model: claude-sonnet-4`)
  - Graph server connection (local vs remote)
  - Agent enable/disable flags
  - Resource limits (max parallel agents, max cost per session)
- [ ] **`~/.cortex/config.yaml`** — user-level config (API keys, defaults)
- [ ] **`.env` support** — for secrets (API keys, DB passwords)
- [ ] **Config validation** — schema validation on load

### 0.3 Local Development Environment
- [ ] **`docker-compose.yml`** — single-machine deployment
  - Cortex Graph Server (Neo4j or SurrealDB + API layer)
  - Agent runtime(s)
  - Dashboard (optional)
- [ ] **`cortex dev up`** — one-command local startup
- [ ] **`cortex dev logs`** — tail all service logs
- [ ] **Health check** — `cortex dev status` shows which services are up
- [ ] **Graceful shutdown** — `cortex dev down` saves state

### 0.4 Installer
- [ ] **One-line install** — `curl -fsSL https://cortex.dev/install.sh | sh`
- [ ] **Pre-built binaries** — Linux, macOS (Windows WSL2)
- [ ] **System dependencies check** — Docker, git, required runtimes

---

## Phase 1: Graph Server Core

### 1.1 Graph Database & Schema
- [ ] **Neo4j/SurrealDB adapter** with connection pooling
- [ ] **Node types** — Project, File, Module, Function, Class, Method, Variable, Interface, Endpoint, DatabaseSchema, LogEntry, TraceSpan, Task, Decision, Commit, Agent
- [ ] **Edge types** — CONTAINS, IMPORTS, CALLS, INHERITS, IMPLEMENTS, REFERENCES, DEFINES_ENDPOINT, MAPS_TO_TABLE, TRIGGERED_BY, BELONGS_TO_TRACE, ASSIGNED_TO, DEPENDS_ON, MOTIVATES, CHANGED_IN, PROPOSES_CHANGE
- [ ] **Indexes** — uuid, Function(name), Class(name), full-text on Task/Decision, LogEntry timestamp
- [ ] **CRUD API** — create/read/update/delete nodes and edges (JSON over WebSocket + REST)

### 1.2 API Layer
- [ ] **WebSocket endpoint** — persistent bidirectional communication for agents
  - Authentication handshake
  - Topic subscription (`project/{id}/node/{type}`, `project/{id}/task/{id}`)
  - JSON change events (create/update/delete + diff summary)
- [ ] **REST/GraphQL API** — for dashboard, CLI queries, initial bootstrap
  - List projects, tasks, agents
  - Query graph by node type, edge type
- [ ] **OpenAPI spec** — auto-generated documentation

### 1.3 Event Streaming (CDC)
- [ ] **Write transaction emitter** — every mutation publishes a change event
- [ ] **Internal pub-sub bus** — topic hierarchy
- [ ] **Subscription manager** — tracks agent subscriptions, delivers events
- [ ] **Retry/delivery guarantee** — at-least-once delivery with dedup window

### 1.4 Concurrency & Conflict Resolution
- [ ] **Optimistic locking** — version field on every node
- [ ] **Vector clocks** — per-property last-writer-wins
- [ ] **Conflict detection** — concurrent writes flagged, stored with `conflict` marker
- [ ] **ManagerAgent conflict resolver** — async review of conflicted nodes
- [ ] **Read-before-write checks** — agents must verify node existence before creating edges

### 1.5 Locking & Task Assignment
- [ ] **Advisory locks** — `locked_by_agent_id` + `locked_at` + TTL on Task nodes
- [ ] **Heartbeat monitor** — agent heartbeat updates, stale lock detection
- [ ] **Lock expiration** — auto-release after timeout, notify Manager

---

## Phase 1.5: MCP Server & Third-Party Integrations

> Cortex is a memory layer first. This phase makes it usable from your existing AI coding tools.

### 1.5.1 MCP Server (Model Context Protocol)
- [ ] **MCP stdio transport** — for CLI-based tools (opencode, Claude Code)
- [ ] **MCP SSE transport** — for persistent connections (Cursor, VS Code extensions)
- [ ] **Tool definitions** — expose these MCP tools:
  - `graph_query` — run Cypher/natural-language queries against project graph
  - `context_for_file` — returns related functions, classes, and dependencies for a file
  - `context_for_symbol` — returns definition, references, callers/callees for a function/class
  - `search_code` — full-text search across indexed code entities
  - `get_recent_errors` — recent runtime errors linked to functions
  - `get_project_structure` — high-level module/file tree
- [ ] **Resource definitions** — expose file contents, function bodies, task details as MCP resources
- [ ] **Authentication** — same auth as graph server
- [ ] **Auto-discovery** — Cortex advertises MCP endpoint so tools find it automatically

### 1.5.2 opencode Integration
- [ ] **opencode agent skill** — a skill file that lets opencode agents query Cortex for project context
- [ ] **`cortex opencode install`** — auto-configures opencode to use the MCP server
- [ ] **Context enrichment** — when opencode opens a project, it asks Cortex for symbol relationships, recent changes, task history

### 1.5.3 Cursor Integration
- [ ] **Cursor rules file** — `.cursor/rules/cortex.mdc` that tells Cursor's agent to query Cortex MCP for context
- [ ] **Auto-context injection** — Cursor automatically fetches related code from the graph when editing a file

### 1.5.4 Claude Code / Other Tooling
- [ ] **CLAUDE.md generation** — `cortex export claude` generates a project context file from the graph
- [ ] **Generic MCP client support** — any MCP-compatible tool can connect

---

## Phase 2: Static Code Indexer

### 2.1 Core Engine
- [ ] **Tree-sitter integration** — generic CST walker
- [ ] **Language query files** — tree-sitter queries for each supported language
- [ ] **Parse + extract** pipeline
- [ ] **Entity extraction** — File, Module, Function, Class, Method, Variable, Interface, Endpoint
- [ ] **Edge extraction** — CONTAINS, IMPORTS, CALLS, INHERITS, IMPLEMENTS, REFERENCES, DEFINES_ENDPOINT
- [ ] **Dynamic call marking** — `call_type: "dynamic"` for unresolvable calls

### 2.2 File Watcher
- [ ] **Native FS events** (inotify/FSEvents)
- [ ] **Incremental re-index** — diff old graph vs new parse
- [ ] **Preserve manual edges** — only managed edges are touched on re-index
- [ ] **Batch transaction** — group file changes into single graph write

### 2.3 Bootstrapping
- [ ] **Full project scan** — initial batch index
- [ ] **Progress reporting** — `cortex index status` shows progress
- [ ] **Ignore patterns** — respect `.gitignore`, `cortexignore`

### 2.4 Error Handling
- [ ] **Parse error handling** — skip file, log warning, create `LogEntry` for review
- [ ] **Timeout protection** — per-file indexing timeout
- [ ] **Fallback to regex** — for languages without tree-sitter grammars (basic mode)

---

## Phase 3: Agent Runtime Framework

### 3.1 Base Agent Process
- [ ] **WebSocket client** — connect, authenticate, subscribe to topics
- [ ] **Event loop** — listen for task assignments, process, write results
- [ ] **LLM abstraction layer** — unified interface for Ollama / vLLM / API-based models
- [ ] **Sandboxed file system** — per-project working directory, restricted access
- [ ] **Secure terminal** — sandboxed shell for running tests/commands

### 3.2 Agent Internal Loop
- [ ] **Task fetch** — receive task event, fetch full Task + dependencies from graph
- [ ] **Context builder** — query graph for relevant code, decisions, schemas, logs
  - Natural language summary (not raw JSON)
  - Token budget management
- [ ] **Prompt constructor** — system message (role) + task + context
- [ ] **LLM response parser** — extract graph mutations (create/update/delete), diffs, sub-tasks
- [ ] **Graph write executor** — batch mutations to server
- [ ] **Task result writer** — update task status, result summary

### 3.3 Agent State & Resilience
- [ ] **Stateless design** — all state in graph
- [ ] **Heartbeat sender** — periodic `heartbeat_at` update
- [ ] **Crash recovery** — Manager detects stale lock, reassigns task
- [ ] **Graceful shutdown** — finish current mutation, update status to idle

### 3.4 Per-Agent Config
- [ ] **Model assignment** — `cortex.yaml: agents.{role}.model`
- [ ] **Resource limits** — max tokens, max cost per task
- [ ] **Tool enable/disable** — which sandbox tools each agent role can use
- [ ] **Override system prompt** — user can customize agent behavior

---

## Phase 4: Manager Agent

### 4.1 Goal Input
- [ ] **Natural language goal parser** — interprets user request
- [ ] **Project context fetcher** — existing code, previous decisions
- [ ] **Goal clarification** — asks user questions when ambiguous

### 4.2 Planning Phase
- [ ] **Research sub-phase** — spawn Researcher Agent to study reference implementations
- [ ] **Architecture design** — LLM generates modules, DB schema, API endpoints, frontend pages
- [ ] **Decision writer** — writes `Decision` nodes with rationale and context snapshot
- [ ] **Task tree generator** — decomposes architecture into small, independent tasks
  - `DEPENDS_ON` edges for ordering
  - Acceptance criteria per task
  - Appropriate `assigned_agent_type` per task
- [ ] **Task publisher** — batch write all tasks to graph

### 4.3 Execution Supervision
- [ ] **Task subscriber** — listen for task status changes
- [ ] **Failed task handler** — analyze error, retry or create fix task
- [ ] **Deadlock detector** — circular dependency detection
- [ ] **Dependency unlocker** — when task completes, unlock dependents
- [ ] **Goal completion detector** — all leaf tasks done + verified
- [ ] **Deployment signaler** — notify DevOps agent
- [ ] **Progress reporter** — `cortex status` shows goal-level progress

### 4.4 Human-in-the-Loop
- [ ] **Approval hooks** — require human approval before destructive operations
- [ ] **Question router** — when Manager is unsure, ask developer via CLI
- [ ] **Override mechanism** — developer can modify task tree, reassign, cancel

---

## Phase 5: Specialist Agents

### 5.1 Architect Agent
- [ ] Requirements analysis prompt
- [ ] Research tool (web fetch, git clone)
- [ ] Decision node writer
- [ ] Module tree designer

### 5.2 Backend Developer Agent
- [ ] Code generation from task + schema
- [ ] PROPOSES_CHANGE edge writer (diff payload)
- [ ] Direct commit capability (optional, configurable)
- [ ] Unit test writer
- [ ] Coding convention adherence (from graph decisions)

### 5.3 Frontend Developer Agent
- [ ] HTML/CSS/JS template generation
- [ ] Framework-aware (React, Vue) if configured
- [ ] Screenshot testing (headless browser)

### 5.4 Database Engineer Agent
- [ ] SQL migration writer
- [ ] DatabaseSchema node creator
- [ ] Index/constraint designer
- [ ] MySQL/PostgreSQL client (sandboxed)

### 5.5 QA Engineer Agent
- [ ] Test suite runner
- [ ] Log + trace collector
- [ ] Failure-to-function linker
- [ ] Bug task creator (with evidence)

### 5.6 DevOps Engineer Agent
- [ ] Dockerfile writer
- [ ] docker-compose.yml generator
- [ ] CI/CD pipeline writer (GitHub Actions, etc.)
- [ ] Deployment executor (staging)

### 5.7 Documentation Writer Agent
- [ ] README updater
- [ ] API doc generator (from endpoints)
- [ ] Inline doc updater (from function signature changes)

### 5.8 Security Auditor Agent
- [ ] Static analysis runner (Psalm, Bandit, etc.)
- [ ] Vulnerability reporter
- [ ] Fix task creator

---

## Phase 6: Runtime Ingestion Pipeline

### 6.1 Instrumentation Library
- [ ] **PHP library** — embeds `cortex_function_id` in log context + OpenTelemetry spans
- [ ] **Node.js library** — same instrumentation
- [ ] **Python library** — same instrumentation
- [ ] **Build-time ID injection** — map file generated by indexer

### 6.2 Ingestion Service
- [ ] **Log ingestion endpoint** — receives JSON logs, creates LogEntry nodes
- [ ] **Trace ingestion (OTLP)** — receives spans, creates TraceSpan nodes
- [ ] **Function linker** — extracts `cortex_function_id`, creates TRIGGERED_BY edges
- [ ] **Error linker** — auto-creates HAS_ERROR edge for ERROR-level logs
- [ ] **Full-text indexing** — log messages searchable

### 6.3 Runtime-to-Code Feedback
- [ ] **Query API** — `get recent errors for function X`
- [ ] **RESOLVED_BY edge** — agent marks log entries as resolved
- [ ] **Log-to-task linking** — bug tasks include relevant log evidence

---

## Phase 7: Developer UI & Dashboard

### 7.1 CLI Dashboard (Priority)
- [ ] **`cortex status`** — project overview, task progress, agent activity
- [ ] **`cortex task list`** — filterable task table (status, agent, priority)
- [ ] **`cortex task approve <id>`** — approve proposed change
- [ ] **`cortex task reject <id> --reason`** — reject with feedback
- [ ] **`cortex diff <task-id>`** — show proposed code changes
- [ ] **`cortex log tail`** — real-time log stream
- [ ] **`cortex graph query <cypher>`** — ad-hoc graph queries

### 7.2 Web Dashboard (Optional)
- [ ] **Graph visualizer** — force-directed layout
- [ ] **Task board** — kanban-style task management
- [ ] **Diff viewer** — side-by-side code review
- [ ] **Activity feed** — real-time agent actions
- [ ] **Log viewer** — searchable, linked to source code

---

## Phase 8: Git Integration

### 8.1 Agent Git Workflow
- [ ] **Auto-branching** — each goal/task gets its own branch
- [ ] **Commit agent** — writes meaningful commit messages referencing task IDs
- [ ] **PR creation** — when task completes, create PR with description
- [ ] **Merge on approval** — auto-merge when human approves

### 8.2 Conflict Resolution
- [ ] **Divergent branch detection** — detect when two agents modify same file
- [ ] **Merge agent** — resolves merge conflicts via LLM
- [ ] **Human fallback** — if merge agent fails, ask developer

---

## Phase 9: Scaling & Deployment

### 9.1 Distributed Mode
- [ ] **Multi-host agent deployment** — agents run on separate machines
- [ ] **Graph server clustering** — Neo4j Aura / Redis cache layer
- [ ] **Network policies** — per-agent access restrictions

### 9.2 Security
- [ ] **WSS/HTTPS** — encrypted communication
- [ ] **Agent sandboxing** — Docker containers with minimal capabilities
- [ ] **API key management** — secure storage, rotation
- [ ] **Audit log** — all agent actions recorded

---

## Phase 10: System Testing & QA

### 10.1 Unit Tests (Cortex Itself)
- [ ] Graph server CRUD tests
- [ ] Indexer parse tests (PHP, Python, JS fixtures)
- [ ] Agent event loop tests
- [ ] Manager planner tests (task tree generation)
- [ ] Conflict resolution tests

### 10.2 Integration Tests
- [ ] **End-to-end forum build** — full phpBB-like project from goal to deploy
- [ ] **Incremental indexing** — file change → graph update
- [ ] **Multi-agent coordination** — two agents working on same project
- [ ] **Failure recovery** — agent crash → task reassignment

### 10.3 Test Fixtures
- [ ] Sample PHP project with classes, functions, endpoints
- [ ] Sample Python project
- [ ] Sample Node.js project
- [ ] Pre-built graph states for test scenarios

---

## Phase 11: Documentation

### 11.1 User Documentation
- [ ] **Getting Started** — install, first project, first goal
- [ ] **Configuration reference** — all `cortex.yaml` options
- [ ] **CLI reference** — all commands and flags
- [ ] **Agent roles** — what each specialist does
- [ ] **Human-in-the-loop guide** — how to review, approve, override

### 11.2 Developer Documentation
- [ ] **Architecture overview** — high-level system diagram
- [ ] **How to add a language** — tree-sitter query guide
- [ ] **How to add an agent** — agent interface documentation
- [ ] **API reference** — all WebSocket/REST endpoints

---

## Milestones

| Milestone | Description | Phases |
|-----------|-------------|--------|
| **M1: cortex CLI + local dev** | Developer can install, init project, start local services | 0 |
| **M2: Graph Server** | Graph store + API + events working | 1 |
| **M3: MCP + Integrations** | Any AI tool (opencode, Cursor) can query Cortex for context | 1.5 |
| **M4: Indexer + Import** | `cortex import` builds full graph from existing codebase | 2 |
| **M5: Memory Layer MVP** | Developer can import a project and query it from their AI tool — this is the core product | 0, 1, 1.5, 2 |
| **M6: Agent Framework** | Base agent connects, receives tasks, writes to graph | 3 |
| **M7: Manager + 1 Specialist** | End-to-end: goal → plan → backend code → tests | 4, 5 |
| **M8: Runtime Ingestion** | App logs/traces linked to code | 6 |
| **M9: Full Swarm** | All specialists operational | 5 (all) |
| **M10: Dashboard + Git + Docs** | Web UI, PR workflow, documentation | 7, 8, 11 |

---

> **Next step:** Review this to-do, edit anything you want, then tell me which milestone to start on.
