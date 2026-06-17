# Cortex Quick Start Guide

## What You Need

- A computer (Windows, Mac, or Linux)
- A folder with code in it (your project)
- An AI coding tool like opencode or Cursor (optional, but recommended)

---

## Step 1: Download Cortex

**Option A: Download the binary**

Go to the Releases page on GitHub and download the file for your computer:
- `cortex-linux-amd64` → Linux
- `cortex-darwin-amd64` → Mac (Intel)
- `cortex-darwin-arm64` → Mac (Apple Silicon / M1/M2/M3)
- `cortex-windows-amd64.exe` → Windows

Rename it to just `cortex` (or `cortex.exe` on Windows).

**Option B: Build from source**

You'll need Go installed. Then run:

```bash
git clone https://github.com/EBTURKgit/cortex.git
cd cortex
make build
```

---

## Step 2: Open a Terminal

**Windows:** Search for "Command Prompt" or "PowerShell"
**Mac:** Search for "Terminal"
**Linux:** Open your terminal app

Navigate to your project folder:

```bash
cd path/to/your/project
```

---

## Step 3: Initialize Cortex

Run this command in your project folder:

```bash
/path/to/cortex init
```

Replace `/path/to/cortex` with the actual location of the cortex file.

For example:
```bash
# If cortex is in your Downloads folder:
~/Downloads/cortex init

# If cortex is in the current folder:
./cortex init
```

This creates a file called `cortex.yaml` in your project.

---

## Step 4: Index Your Code

Tell Cortex to read your code and build a map:

```bash
/path/to/cortex import .
```

The `.` means "the current folder". Cortex will scan all your files.

You'll see something like:
```
✅ Import complete: my-project
   Files scanned: 42
   Entities found: 356
   Graph contains 412 nodes and 128 edges
```

---

## Step 5: Check the Map

Ask Cortex what it found:

```bash
/path/to/cortex query stats
```

You'll see a summary:
```
Graph Stats:
  Total nodes:  412
  Total edges:  128
  Node types:
    File: 42
    Function: 250
    Class: 30
    ...
```

---

## Step 6: Start the Web Dashboard (Optional)

This opens a visual map in your browser:

```bash
/path/to/cortex serve
```

Open `http://127.0.0.1:8741` in your browser.

You'll see:
- **Dashboard tab** — Numbers and stats about your project
- **Tasks tab** — Shows tasks (empty until you create some)
- **Graph tab** — An interactive map of your code. Click any dot to see details
- **Activity tab** — Shows your project history

Press `Ctrl+C` in the terminal to stop the server.

---

## Step 7: Connect Your AI Tool

### For opencode

1. Make sure opencode is installed
2. Tell opencode about Cortex by creating a config file:

```bash
mkdir -p ~/.config/opencode
```

Create a file at `~/.config/opencode/opencode.jsonc` with this content:

```json
{
  "mcp": {
    "cortex": {
      "type": "local",
      "command": ["/full/path/to/cortex", "mcp"],
      "cwd": "/full/path/to/your/project",
      "enabled": true
    }
  }
}
```

Replace `/full/path/to/cortex` and `/full/path/to/your/project` with your actual paths.

3. Verify it works:

```bash
opencode mcp list
```

You should see:
```
●  ✓ cortex  connected
```

### For Cursor

1. Open Cursor settings
2. Go to "MCP Servers"
3. Add a new server:
   - Name: `cortex`
   - Command: `/full/path/to/cortex mcp`
4. Click "Save"

### For Claude Code

Claude Code supports MCP natively. Configure it to run:

```bash
/full/path/to/cortex mcp
```

---

## Step 8: Use It

Now when you use your AI tool, it can ask Cortex about your code.

### Example Prompts

Try these in your AI tool:

> "Use cortex to find all functions in main.go"

> "Query cortex for the project structure"

> "Search cortex for any class named UserService"

> "Ask cortex what functions are in the backtest file"

The AI will use Cortex's MCP tools to answer your questions about the code.

---

## Common Problems

**"command not found"**
→ You're not using the right path. Use the full path like `/home/user/Downloads/cortex`

**"Permission denied"**
→ Make the file executable: `chmod +x /path/to/cortex`

**"No graph available"**
→ You didn't run `cortex import .` yet. Run it first.

**"Port already in use"**
→ Another program is using port 8741. Either stop that program, or change the port in `cortex.yaml`

**"Go not found"**
→ You need Go installed to build from source. Or download the pre-built binary from Releases.

---

## Quick Reference

```
cortex init              Set up a new project
cortex import .          Index your code
cortex query stats       See what's in the graph
cortex query CreateUser  Search for a function
cortex serve             Start the dashboard
cortex mcp               Connect your AI tool
cortex doctor            Check everything is working
```
