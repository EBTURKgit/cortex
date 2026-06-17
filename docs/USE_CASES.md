# Cortex Use Cases

## 1. Solo Developer Working on a Large Project

**Scenario:** You have a codebase with hundreds of files. You use opencode or Cursor to help you code.

**Without Cortex:** Every time you ask the AI for help, it reads your files from scratch. A simple question like "where is the user authentication logic?" takes seconds while the AI scans your project.

**With Cortex:** You import your project once. Now when you ask the AI anything, it queries Cortex's map instantly. The AI already knows where everything is.

**Setup:** `cortex import .` — one command.

---

## 2. Team Using AI Coding Tools

**Scenario:** Multiple developers use AI assistants on the same project. Each AI session starts blind.

**Without Cortex:** Each developer's AI reads files independently. Developer A's AI doesn't know what Developer B's AI built yesterday.

**With Cortex:** Run a shared Cortex server. All AI tools connect to the same memory. Every developer's AI has full project awareness.

**Setup:** `cortex serve` on a shared machine, team configures their tools to connect.

---

## 3. Rapid Prototyping

**Scenario:** You want to build a new feature quickly. You describe it to the AI, and it generates code.

**Without Cortex:** The AI generates code that might not match your project's patterns. It might create duplicate functions or use wrong conventions.

**With Cortex:** The AI queries Cortex first. It sees your existing patterns, naming conventions, and project structure. The generated code fits right in.

**Setup:** Already indexed. Just ask the AI your question.

---

## 4. Learning an Unfamiliar Codebase

**Scenario:** You just joined a project and need to understand the code.

**Without Cortex:** You open files randomly, search for keywords, and try to piece together how things work.

**With Cortex:** Open the dashboard at `http://localhost:8741`. You see the entire project as an interactive map. Click any function to see what it calls and what calls it. The Activity tab shows the project's history.

**Setup:** `cortex import .` then open the dashboard.

---

## 5. Debugging Runtime Errors

**Scenario:** Your app crashes with an error. You need to find the root cause.

**Without Cortex:** You grep for the error message, open files, and trace the call stack manually.

**With Cortex:** If your app sends logs to Cortex's ingestion endpoint, every error is linked to the function that caused it. Ask your AI: "what errors happened in the last hour?" — it queries Cortex and gets the answer.

**Setup:** Configure your app to send logs to `POST http://localhost:8741/ingest/log`.

---

## 6. Automated Code Review

**Scenario:** You want an AI to review pull requests and understand the full context.

**Without Cortex:** The AI sees only the diff. It doesn't know how the changed code connects to the rest of the project.

**With Cortex:** The AI queries Cortex for all functions and files related to the change. It can tell you: "This change also affects the payment module because the function `calculateTotal` is called from there."

**Setup:** Connect your CI/CD tool to Cortex's MCP server.

---

## 7. Building a Project from Scratch (AI generates the whole thing)

**Scenario:** You want the AI to build an entire project from a description.

**Without Cortex:** The AI generates code, but has no way to plan or track progress. It might build things in the wrong order.

**With Cortex:** Run `cortex goal "Build a blog with Python and SQLite"`. Cortex plans the architecture, creates a task list, and assigns tasks. Your AI can then execute each task, updating progress in the graph.

**Setup:** `cortex goal "<your description>"` — then review the plan and confirm.

---

## 8. Data Science / Analysis Projects

**Scenario:** You have data files (CSV, JSON) and Python scripts for analysis.

**Without Cortex:** The AI doesn't know what data files exist or what analyses have been done.

**With Cortex:** Index your project. The AI knows about your data files, your analysis scripts, and can build on previous work.

**Setup:** `cortex import .` — works with Python, R, CSV, JSON files.

---

## 9. CI/CD Pipeline Intelligence

**Scenario:** You want your CI/CD pipeline to understand code changes and make intelligent decisions.

**Without Cortex:** CI/CD runs predefined scripts. It can't reason about the impact of changes.

**With Cortex:** Before deployment, the CI system queries Cortex: "What files changed? What functions are affected? What tests should run?" Results in smarter, faster pipelines.

**Setup:** Add `cortex query` calls to your CI/CD scripts.

---

## 10. Teaching / Mentoring

**Scenario:** You're teaching someone a codebase.

**Without Cortex:** You explain things verbally and point to files.

**With Cortex:** Open the graph visualization. It's like a mind map of your project. You can zoom in on modules, click functions to see what they do, and show how everything connects.

**Setup:** Run `cortex serve` and open the dashboard.
