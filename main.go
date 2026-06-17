// Project Cortex — Graph-based memory layer for AI coding tools.
//
// Cortex provides persistent, graph-based project memory that plugs into
// your existing AI coding tools (opencode, Cursor, Claude Code) via MCP,
// REST API, and WebSocket.
//
// Architecture:
//   main.go          — Entrypoint, wires everything together
//   cmd/             — CLI commands (init, serve, dev, import, query, mcp)
//   internal/graph/  — In-memory labeled property graph engine
//   internal/server/ — HTTP + WebSocket server with REST API
//   internal/mcp/    — MCP protocol server for AI tool integration
//   internal/indexer/— Source code scanner and entity extractor
//   internal/config/ — Configuration management (cortex.yaml)
//   internal/logging/— Structured logging with debug tracing
//   internal/progress/— Build checklist and verification tracker
//
// Build:
//   go build -o bin/cortex .
//
// Usage:
//   cortex init              Create a new project
//   cortex import ./project  Index an existing codebase
//   cortex serve             Start the graph server
//   cortex mcp               Run MCP server for AI tools
//   cortex query "..."       Query the graph
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/EBTURKgit/cortex/cmd"
	"github.com/EBTURKgit/cortex/internal/config"
	"github.com/EBTURKgit/cortex/internal/logging"
	"github.com/EBTURKgit/cortex/internal/progress"
)

func main() {
	// Initialize structured logging
	logging.SetLevel(logging.DEBUG)
	logging.Info("Cortex starting", map[string]interface{}{
		"version": "0.1.0",
		"pid":     os.Getpid(),
	})

	// Load .env file if present
	config.LoadEnvFile(".env")

	// Load or create progress checklist
	checklistPath := findChecklistPath()
	cl, err := progress.LoadChecklist(checklistPath)
	if err != nil {
		cl = progress.NewChecklist(checklistPath)
	}
	// Register build steps if checklist is empty (first run)
	if len(cl.List()) == 0 {
		progress.RegisterBuildSteps(cl)
		cl.Save()
	}

	// Log current build progress at startup
	summary := cl.Summary()
	logging.Debug("Build progress", map[string]interface{}{
		"total":           summary["total"],
		"completed":       summary["completed"],
		"verified":        summary["verified"],
		"next_pending":    cl.NextPending(),
	})

	// Execute the CLI command
	cmd.Execute()

	logging.Info("Cortex exiting")
}

// findChecklistPath locates the progress checklist file.
func findChecklistPath() string {
	// Try current directory's .cortex directory first
	if cwd, err := os.Getwd(); err == nil {
		path := filepath.Join(cwd, ".cortex", "progress.json")
		if _, err := os.Stat(filepath.Dir(path)); err == nil {
			return path
		}
		// Also check if cortex.yaml exists here
		if _, err := os.Stat(filepath.Join(cwd, "cortex.yaml")); err == nil {
			os.MkdirAll(filepath.Dir(path), 0755)
			return path
		}
	}

	// Fall back to user home directory
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot find home dir: %s\n", err)
		return ""
	}
	path := filepath.Join(home, ".cortex", "progress.json")
	os.MkdirAll(filepath.Dir(path), 0755)
	return path
}
