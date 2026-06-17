# Why Cortex?

## The Problem

When you use AI coding tools (like opencode, Cursor, or Claude Code), they have a big problem: **they forget everything when you close the session.**

Ask an AI to build a feature, and it reads your files, writes code, and you're happy. But ask it again tomorrow, and it has zero memory of what it built. It re-reads everything from scratch. Every time.

This is incredibly wasteful. The AI spends most of its time re-learning your codebase instead of actually building things.

## What Cortex Does

Cortex is a **memory layer for AI coding tools.** It gives AI agents a permanent brain that remembers your project across sessions.

Here's how it works:

1. **Index your codebase** — Point Cortex at your project. It reads every file and builds a map of your code (functions, classes, files, and how they connect).

2. **Your AI tool connects to Cortex** — When your AI coding assistant needs context, it asks Cortex instead of re-reading files.

3. **Cortex answers instantly** — "What functions are in this file?", "Who calls this function?", "What does this module contain?" — all answered from the map.

4. **The AI builds with real context** — Instead of guessing, the AI knows exactly what exists and where.

## How It's Different from Other Tools

### vs. Cursor's Context
Cursor remembers your conversation within a session, but when you start a new one, it's blank. Cortex remembers **forever** — across sessions, across days, across weeks.

### vs. opencode's Sessions
opencode creates sessions that can be continued, but the AI still re-reads files to understand your project. Cortex gives it a pre-built map so it doesn't need to.

### vs. GitHub Copilot
Copilot looks at the file you're currently editing. Cortex knows your **entire project** — all files, all functions, all connections.

### vs. Building Your Own
You could build a vector database with embeddings. That takes weeks of work and needs expensive API calls. Cortex is a single binary that runs on your machine with no external services.

### vs. Claude Projects / Custom GPTs
Those let you upload files once. But they don't update when your code changes, and they can't query your codebase dynamically. Cortex re-indexes when files change and answers specific questions.

## The Core Idea

**Every AI coding tool needs persistent project memory.** Without it, every session starts from zero. With Cortex, the AI knows your project the same way you do — because it remembers.

Cortex is:
- **Persistent** — remembers across all sessions, forever
- **Local** — runs on your machine, your code never leaves
- **Fast** — answers in microseconds, not seconds
- **Universal** — works with any AI tool that supports MCP
- **Free** — no API costs, no subscriptions
