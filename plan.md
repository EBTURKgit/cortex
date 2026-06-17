Here is a complete, hyper‑detailed blueprint for **Project Cortex**, written as a Markdown document that an AI coding agent can follow to build the entire system in one go. It contains no code, only exhaustive architectural and algorithmic explanations.

---

# Project Cortex: Full Construction Blueprint

## 1. Vision and Purpose
Project Cortex is a **shared, real‑time, graph‑based memory and coordination layer** that enables a swarm of specialised AI coding agents to collaboratively design, build, test, and deploy production‑grade software. It replaces chaotic chat‑based memory and full‑codebase re‑reads with a structured, continuously updated **project knowledge graph** that all agents treat as the single source of truth.

The system receives high‑level commands (e.g., “Build a forum like phpBB in PHP with MySQL, no framework”), automatically plans the work, spawns specialist agents, assigns tasks, and coordinates their activity via the graph. The output is a fully functional, tested, documented, and deployed application.

---

## 2. High‑Level System Architecture
The system consists of the following major components, each designed to be developed, deployed, and scaled independently:

- **Cortex Graph Server** – the central database and event hub.
- **Static Code Indexer** – parses source code into graph nodes and edges.
- **Runtime Ingestion Pipeline** – captures logs, traces, and metrics and links them to code entities.
- **Agent Runtime Environment** – a container/process that runs one or more specialised agents.
- **Manager Agent** – the high‑level orchestrator and planner.
- **Specialist Agents** – architect, backend developer, frontend developer, database engineer, QA engineer, DevOps engineer, documentation writer, and security auditor.
- **Developer Dashboard** – an optional web UI for human oversight, approval, and visualisation.

All communication between agents, as well as between agents and external world, flows through the Cortex Graph Server. Agents never talk directly to each other; they only read/write the graph and subscribe to change notifications.

---

## 3. The Cortex Graph Server

### 3.1 Core Responsibilities
- Store and serve the unified project knowledge graph.
- Provide a transactional, atomic API for adding, updating, and deleting nodes and edges.
- Publish change events to subscribed agents in real time.
- Enforce advisory locking for task assignments.
- Handle conflict resolution for concurrent modifications.

### 3.2 Technology Choices
Use a **native graph database** with support for high‑write throughput, subscription triggers, and a flexible schema. Neo4j is the recommended primary choice due to its maturity, Cypher query language, and change‑data‑capture (CDC) plugins. Alternatively, SurrealDB can be used for an embedded, multi‑model approach that simplifies deployment but provides graph semantics via relations.

The server itself is a standalone process that exposes:
- A **WebSocket endpoint** for agent connections (persistent, bidirectional).
- A **REST or GraphQL API** for dashboard queries and initial graph bootstrap.

### 3.3 Graph Schema (Semantic Model)
The graph uses a **labelled property graph** model. Every node has a unique identifier (`uuid`), a `type` label, a `created_at` timestamp, an `updated_at` timestamp, and a `version` integer for optimistic concurrency control.

#### 3.3.1 Node Types and Their Properties

**Project**
- *Properties:* `name`, `root_path`, `description`, `default_language`

**File**
- *Properties:* `relative_path`, `language`, `checksum`, `last_indexed_at`
- *Meaning:* Represents a single source file in the project.

**Module**
- *Properties:* `name`, `type` (namespace, package, module)
- *Meaning:* A logical grouping of code; maps to PHP namespaces, Python modules, etc.

**Function**
- *Properties:* `name`, `signature`, `start_line`, `end_line`, `body_hash`, `is_anonymous`
- *Meaning:* Any callable unit (function, method, closure).

**Class**
- *Properties:* `name`, `is_abstract`, `is_final`
- *Meaning:* A class definition.

**Method** (subtype of Function, but tagged differently)
- *Properties:* inherits from Function, plus `visibility` (public, protected, private), `is_static`

**Variable**
- *Properties:* `name`, `kind` (global, local, parameter, field), `type_annotation`

**Interface**
- *Properties:* `name`

**Endpoint**
- *Properties:* `http_method`, `path_pattern`, `description`
- *Meaning:* An exposed HTTP route in the application.

**DatabaseSchema**
- *Properties:* `table_name`, `columns` (JSON array of column definitions), `engine`
- *Meaning:* Represents a database table structure.

**LogEntry**
- *Properties:* `timestamp`, `level` (DEBUG, INFO, WARN, ERROR), `message`, `raw_json`
- *Meaning:* A single log line captured from the running application.

**TraceSpan**
- *Properties:* `trace_id`, `span_id`, `parent_span_id`, `operation_name`, `start_time`, `duration_ms`
- *Meaning:* A single span in a distributed trace.

**Task**
- *Properties:* `title`, `description`, `status` (pending, in_progress, completed, failed), `priority`, `assigned_agent_type`, `locked_by_agent_id`, `result_summary`
- *Meaning:* A unit of work for an agent.

**Decision**
- *Properties:* `statement`, `rationale`, `context_snapshot`
- *Meaning:* An architectural or design choice made by an agent (or human).

**Commit**
- *Properties:* `hash`, `message`, `author`, `timestamp`

**Agent**
- *Properties:* `agent_id`, `agent_type`, `status` (idle, working, offline), `connected_at`

#### 3.3.2 Edge Types and Their Meaning
All edges are directed and have a label, a `created_at` timestamp, and optional properties (e.g., `weight` for dynamic calls).

- **CONTAINS**: `File → Function`, `File → Class`, `Class → Method`, `Module → Class`, etc. – structural ownership.
- **IMPORTS**: `File → Module` or `Module → Module` – dependency via import/include.
- **CALLS**: `Function → Function` or `Method → Method` – static call relationship. Optionally stores a `line` property and `call_type` (static, dynamic).
- **INHERITS**: `Class → Class` – subclass relationship.
- **IMPLEMENTS**: `Class → Interface`.
- **REFERENCES**: `Function/Method → Variable` – a function reads/writes a variable. Edge carries a `access_type` property (read, write, readwrite).
- **DEFINES_ENDPOINT**: `Function → Endpoint` – links a handler function to its HTTP route.
- **MAPS_TO_TABLE**: `Class/Function → DatabaseSchema` – indicates that the code entity interacts with that database table.
- **TRIGGERED_BY**: `LogEntry → Function` – indicates the log was emitted from within that function.
- **BELONGS_TO_TRACE**: `LogEntry → TraceSpan` or `TraceSpan → TraceSpan` (parent‑child).
- **ASSIGNED_TO**: `Task → Agent` – current assignment.
- **DEPENDS_ON**: `Task → Task` – task ordering; the target must be completed before the source can start.
- **MOTIVATES**: `Decision → Function/Class/Module` – explains the reason for creating/modifying that entity.
- **CHANGED_IN**: `Function/Class/File → Commit` – links code entity to a specific version control commit.
- **PROPOSES_CHANGE**: `Agent → CodeEntity` – an agent proposes a modification (diff) to that entity. The edge carries a `diff_payload` property and `status` (pending, approved, rejected).

### 3.4 Indexing Strategy
To ensure fast graph queries even with millions of nodes, the server must maintain:
- A unique index on node `uuid`.
- Composite indexes on `Function(name)` and `Class(name)`.
- Full‑text index on `Task.title` and `Decision.statement`.
- A spatial/temporal index on `LogEntry.timestamp`.
- Edge indexes on all relationship types for quick traversal (e.g., `CALLS`).

### 3.5 Event Streaming (Change‑Data‑Capture)
Every write transaction that modifies the graph must emit a **change event** to a message bus internal to the server. Agents subscribe to **topics** based on project ID and optionally node type. The server uses a publish‑subscribe model:
- Topic hierarchy: `project/{project_id}/node/{node_type}`, `project/{project_id}/edge/{edge_type}`, `project/{project_id}/task/{task_id}`.
- Agents send a subscription filter upon WebSocket handshake.
- The server pushes JSON‑formatted change records containing the type of change (create, update, delete), affected node/edge IDs, and a summary of changed properties.

### 3.6 Conflict Resolution and Concurrency
Multiple agents may attempt to update the same node simultaneously. The system uses a **last‑writer‑wins (LWW) approach with vector clocks** on a per‑property basis.
- Every node carries a vector clock that increments whenever any agent updates it.
- When a write arrives, the server compares the incoming clock with the stored clock. If the incoming clock descends from the stored one, the write is accepted. If concurrent, the server applies a deterministic merge: for scalar properties, the one with the highest timestamp wins; for complex properties (e.g., JSON arrays), it attempts a CRDT‑style merge (e.g., add‑wins set for endpoints attached to a function).
- For structural conflicts (e.g., two agents create a Function with the same name in the same File), the conflict is resolved by the **Manager Agent** asynchronously. The server stores both nodes with a temporary conflict flag and notifies the manager.

---

## 4. Static Code Indexer

### 4.1 Purpose
To scan the project’s source code and populate the graph with `File`, `Module`, `Function`, `Class`, `Method`, `Variable`, `Interface`, and `Endpoint` nodes, as well as `CONTAINS`, `IMPORTS`, `CALLS`, `INHERITS`, `IMPLEMENTS`, `REFERENCES`, and `DEFINES_ENDPOINT` edges. It must support incremental updates triggered by file changes.

### 4.2 Language‑Agnostic Approach
Use **tree‑sitter** as the universal parsing engine. Tree‑sitter provides grammars for PHP, JavaScript, TypeScript, Python, Go, and many others. The indexer contains a **core engine** that walks a generic concrete syntax tree (CST) and a set of **language‑specific query files** that define how to extract the desired entities.

The indexer works in two steps:
1. **Parse**: tree‑sitter produces a CST for a file.
2. **Extract**: run language‑specific queries against the CST to collect nodes, edges, and their locations.

Queries are expressed in the tree‑sitter query language and embedded as resource files. For example, a query for PHP functions might be:
```
(function_definition) @function
```
The indexer captures the node’s byte range, name, and signature.

### 4.3 Incremental Indexing
The indexer watches the file system (using native OS events) for create, modify, and delete events. When a file changes:
- It re‑parses the file.
- It compares the new set of entities with the old set stored in the graph (the old set is retrieved by querying `CONTAINS` edges from the `File` node).
- It computes a diff: nodes/edges that were removed are deleted from the graph; new ones are inserted; modified ones (same identifier but changed body_hash) are updated.
- It updates the `File` node’s `checksum` and `last_indexed_at`.

The indexer must be careful to preserve any manually added edges by agents (e.g., decisions, tasks linked to functions) during re‑indexing. Only auto‑generated structural edges are managed.

### 4.4 Handling Ambiguity and Dynamic Constructs
Static analysis cannot resolve all calls, e.g., `$obj->$method()` in PHP or calls through interfaces. The indexer marks such calls with a `CALLS` edge having `call_type: "dynamic"` and omits the exact target function. Later, runtime ingestion (traces) will provide the concrete target and can add definitive edges with `call_type: "observed"`.

### 4.5 Bootstrapping a New Project
When a project is first connected, the indexer performs a full scan of the codebase, creating all file, module, and code entity nodes in one batch transaction. After that, it switches to incremental mode.

---

## 5. Runtime Ingestion Pipeline

### 5.1 Goal
To capture logs, errors, and request traces from the running application and link them back to the source code graph so that agents can query “what error occurred in which function and under what conditions?”

### 5.2 Instrumentation Layer
A lightweight library (provided for each supported language) is integrated into the application. It:
- Retrieves the unique function/class identifiers from the graph (pre‑computed at build time and embedded in the code as comments or a generated map file).
- Injects these identifiers into log context (e.g., `{"cortex_function_id": "uuid-of-function"}`).
- Creates OpenTelemetry spans with the same identifiers as attributes.

The library sends logs to a collector (e.g., Fluentd or directly to the Cortex Server’s ingestion endpoint).

### 5.3 Ingestion Service
A dedicated service (can be part of the Cortex Graph Server or a microservice) receives logs and traces.
- **Log processing**: Parses incoming JSON logs, extracts `cortex_function_id`, and creates/updates a `LogEntry` node linked via `TRIGGERED_BY` to the Function. It also indexes full‑text searchable fields.
- **Trace processing**: Receives OTLP spans, converts them to `TraceSpan` nodes, and links them to Functions using the `cortex_function_id` attribute. It also creates `BELONGS_TO_TRACE` edges to reconstruct the call tree.
- **Automatic error linking**: When an ERROR log arrives, the ingestion service automatically adds a `HAS_ERROR` edge from the linked Function to the LogEntry, enabling quick queries like “all functions with recent errors.”

### 5.4 Runtime‑to‑Code Feedback Loop
Agents can query the graph for recent log activity (e.g., “get all ERROR logs for function X in the last 1 hour”) and use that information to debug. When an agent fixes a bug, it can update the related `LogEntry` nodes with a `RESOLVED_BY` edge pointing to a commit or task.

---

## 6. Agent Runtime Environment

### 6.1 Agent Process Model
Each agent (Manager, Backend, Frontend, etc.) runs as an independent process (or container) on a dedicated machine (mini PC, server, or cloud VM). The process:
- Loads a local (or remote) LLM via Ollama, vLLM, or an API wrapper.
- Connects to the Cortex Graph Server via WebSocket.
- Authenticates and announces its `agent_type`.
- Enters an event loop: listen for graph changes that affect assigned tasks, process them, and write back results.

### 6.2 Agent Internal Loop
1. **Subscribe** to `project/{id}/task/{my_agent_type}` and `project/{id}/task/{assigned_task_ids}`.
2. On receiving a task assignment event (or on idle poll), the agent fetches the full `Task` node and its dependencies.
3. It builds a **minimal context window** by querying the graph for:
   - The Task description and any linked decisions.
   - The current code entities relevant to the task (e.g., all Functions in the module it must modify).
   - The current task status of dependent tasks.
4. It formulates a prompt for its LLM that includes a system message describing its role, the task, and the retrieved graph context in a structured text format (not raw JSON, but a natural language summary).
5. The LLM generates a plan and then outputs a series of **graph mutation intentions**: new code entities, modified code (diffs), new decisions, or even sub‑tasks.
6. The agent translates these intentions into actual graph API calls (create node, add edge, update property) and sends them to the server.
7. If the task requires running code or tests, the agent can interact with a terminal/sandbox via tool calls (the agent runtime must provide a secure sandbox environment with file system and network access limited to the project).

### 6.3 Agent State and Resilience
- Agents are stateless beyond their LLM conversation context; all critical state is in the graph.
- If an agent crashes mid‑task, the `Task` node still holds its last status. The Manager Agent can detect stale locks (via heartbeat) and reassign the task.
- Heartbeat: every agent periodically updates a `heartbeat_at` property on its own `Agent` node. The manager monitors this.

---

## 7. Manager Agent – The Orchestrator

### 7.1 Core Function
The Manager Agent is the entry point for high‑level user requests. It interprets the request, coordinates a **researcher agent** (optional), creates a comprehensive task plan as a tree of `Task` nodes, and supervises execution.

### 7.2 Planning Phase
1. **Goal input**: “Build a forum like phpBB using PHP and MySQL, no framework.”
2. **Research sub‑phase** (if enabled): Manager spawns a Researcher Agent that fetches the phpBB source code (via git clone or download), indexes it into a temporary sandbox, analyses the architecture, and writes its findings as `Decision` nodes into the project graph (e.g., “phpBB uses a modular system with hooks”, “Database tables: users, posts, topics, forums”).
3. **Architecture design**: Manager combines the research decisions with the original request and prompts its LLM to produce a detailed software architecture: list of modules, database schema, API endpoints, frontend components. Output is written as a set of `Decision` and `Module` nodes, plus a high‑level `Task` hierarchy.
4. **Task tree creation**: Manager decomposes the architecture into small, independent tasks with clear acceptance criteria. Each task is marked with the appropriate specialist type (`backend`, `frontend`, `database`, `qa`, etc.). Tasks are linked with `DEPENDS_ON` edges to enforce order (e.g., “Create database schema” must finish before “Build user registration endpoint”).
5. **Publishing tasks**: Manager writes all `Task` nodes to the graph in a single transaction. This triggers notifications to the relevant specialist agents.

### 7.3 Execution Supervision
The Manager Agent subscribes to all task status changes. It does not micromanage; it only intervenes when:
- A task fails (status `failed`): it analyses the error (from logs or task result) and either retries, creates a fix task, or asks for human input.
- A deadlock is detected (circular dependency or stalled task).
- A task completes, and dependent tasks can now be unlocked by setting their status to `pending` (they were previously blocked).
- The overall goal is achieved (all leaf tasks completed and tested). Manager then signals the DevOps agent to deploy and marks the project as `ready`.

---

## 8. Specialist Agent Designs

Each specialist agent has the same base runtime but different system prompts and tool restrictions.

### 8.1 Architect Agent
- **Role:** Analyses requirements, researches existing systems, creates high‑level design decisions.
- **Output:** `Decision` nodes, `Module` nodes, high‑level task descriptions.
- **Tools:** Web browsing (for research), graph read/write.

### 8.2 Backend Developer Agent
- **Role:** Implements server‑side logic: creates PHP classes/functions, API handlers, business logic.
- **Process:** Receives a task like “Implement user registration endpoint”. It queries the graph for database schema (already defined), existing module structure, and coding conventions (decisions). It then writes code by proposing diffs that are written as `PROPOSES_CHANGE` edges, or directly commits to a working branch (if given write access). It also writes unit tests as part of the task.
- **Tools:** File system (sandbox), terminal (run tests), graph read/write.

### 8.3 Frontend Developer Agent
- **Role:** Creates HTML/CSS/JavaScript or framework‑based components (though the example forbids framework, it will write plain PHP templates and vanilla JS).
- **Tools:** File system, optionally a headless browser for screenshot testing.

### 8.4 Database Engineer Agent
- **Role:** Designs and maintains the database schema. Writes migration scripts.
- **Output:** `DatabaseSchema` nodes, SQL migration files.
- **Tools:** MySQL client (sandbox), file system.

### 8.5 QA Engineer Agent
- **Role:** Runs the application, executes test suites, analyses results, and reports bugs.
- **Process:** Periodically (or on every commit) triggers the test suite, collects logs and traces, and links failures to specific code functions. If a test fails, it creates a new `Task` with type `fix` and assigns it to the appropriate specialist, attaching the log and trace evidence.
- **Tools:** Terminal, API testing tools.

### 8.6 DevOps Engineer Agent
- **Role:** Containerises the application, writes CI/CD pipeline definitions, deploys to staging/production.
- **Tools:** Docker CLI, kubectl (if needed), cloud APIs.

### 8.7 Documentation Writer Agent
- **Role:** Keeps project documentation, README, and API docs up to date with code changes. When a function signature changes, it updates the doc blocks and Markdown files.
- **Tools:** File system.

### 8.8 Security Auditor Agent
- **Role:** Scans code for vulnerabilities (SQL injection, XSS, etc.) and generates fix tasks.
- **Tools:** Static analysis tools (e.g., Psalm, Bandit), graph queries.

---

## 9. The Complete Workflow End‑to‑End

1. **User input:** “Build a forum like phpBB in PHP with MySQL, no framework.”
2. **Manager Agent** receives the command. It creates a `Project` node if not already existing.
3. Manager spawns a **Researcher** (or performs research itself). Researcher clones phpBB, indexes it, writes decisions.
4. Manager designs architecture: modules, database tables, API endpoints, frontend pages. All stored as decisions and a draft task tree.
5. Manager publishes the task tree. The **Database Engineer** picks up the “Create database schema” task, writes SQL, and commits. It updates the graph with `DatabaseSchema` nodes.
6. Once the schema task completes, dependent tasks become active. The **Backend Developer** agents start implementing API endpoints one by one. As they code, the **Static Indexer** automatically updates the graph with new functions and call edges.
7. The **Frontend Developer** builds templates and static assets.
8. The **QA Engineer** runs tests after each commit (or on a schedule). If it finds a failure, it logs a `LogEntry` linked to the offending function and creates a `Task` for the backend agent.
9. The **Documentation Writer** monitors new code and writes docs.
10. The **Security Auditor** scans the code and reports vulnerabilities.
11. When all tasks are complete, the **DevOps Engineer** packages the app into a Docker container and deploys it to a staging environment.
12. The **Runtime Ingestion** pipeline starts receiving logs and traces from staging. The feedback loop ensures any runtime errors are caught and fixed before production.
13. Manager declares the project production‑ready.

All steps are transparently recorded in the graph, forming a complete project history.

---

## 10. Developer Dashboard (Optional but Recommended)

A web application that connects to the Graph Server’s API and provides:
- A visual graph explorer (force‑directed layout) of the code, tasks, and logs.
- A task board (like Trello) that shows task status, assignments, and dependencies.
- A diff viewer for agent‑proposed changes, with approve/reject buttons.
- A log viewer that jumps to the source code.
- A real‑time feed of agent activity.

The dashboard itself can be built by the swarm later in the project lifecycle.

---

## 11. Scaling and Deployment Considerations

### 11.1 Distributed Mini PCs
- Each mini PC runs one or more agent processes. They need only network access to the Cortex Graph Server and to the code repository (e.g., a local Git server or GitHub).
- The Graph Server should be deployed on a more powerful central machine (or a cloud VM) to handle concurrent writes and graph queries. If the swarm grows, the Graph Server can be clustered using Neo4j Aura or a Redis‑backed cache for read‑heavy workloads.

### 11.2 Security and Isolation
- Agents must not have unrestricted internet access unless needed (only Researcher and maybe Manager). Network policies limit what each agent can reach.
- All communication with the Graph Server is encrypted (WSS for WebSocket, HTTPS for REST).
- Agent sandboxes are Docker containers with limited capabilities and no access to host secrets.

### 11.3 Modularity and Swappable Components
- The Graph Server’s database can be replaced with a different graph backend by implementing a storage adapter.
- The LLM backend is abstracted; agents can switch between local models (Ollama) and cloud APIs (Claude, GPT‑4o) via configuration.

---

## 12. Implementation Order (Build Plan)

The AI coder should implement the system in the following sequence, testing each step:

1. **Graph Server Core** (database schema, CRUD API, WebSocket pub/sub, event engine).
2. **Static Indexer** with PHP support (tree‑sitter integration, incremental mode).
3. **Basic Agent Framework** (connect, subscribe, simple task execution loop).
4. **Manager Agent** with planning capabilities (can generate a task tree from a simple goal).
5. **One Specialist Agent** (Backend Developer) that can implement a single function from a task.
6. **Runtime Ingestion** (log collector, function‑id injection).
7. **QA Agent** that runs tests and links failures.
8. **Other specialists** added incrementally.
9. **Dashboard** for human observation.
10. **End‑to‑end forum build scenario** as integration test.

---

## 13. Edge Cases and Failure Handling

- **Parse errors in source code**: Static indexer skips the file, logs a warning, and waits for a fix.
- **Model hallucinates non‑existent graph nodes**: Agents must always verify that referenced node IDs exist before creating edges, using a read‑before‑write check.
- **Infinite task loops**: Manager sets a maximum retry count and a time‑to‑live for tasks.
- **Divergent code**: When two agents modify the same file simultaneously, they work on different branches; conflict is resolved by a human or a dedicated merge agent that analyses both changes and picks the best.

---

## 14. Conclusion
This document describes every component, data structure, interaction pattern, and operational logic required to build Project Cortex. An AI coding agent with access to this blueprint can produce the complete system by following the architecture and sequencing without ambiguity. The result will be a cooperative AI development swarm that remembers everything, wastes no context, and builds reliable software from a single sentence.
