// Package cmd defines all CLI commands for the cortex tool.
// Commands: init, serve, dev, config, import, query, mcp, progress.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/EBTURKgit/cortex/internal/agent"
	"github.com/EBTURKgit/cortex/internal/config"
	"github.com/EBTURKgit/cortex/internal/graph"
	"github.com/EBTURKgit/cortex/internal/indexer"
	"github.com/EBTURKgit/cortex/internal/logging"
	"github.com/EBTURKgit/cortex/internal/mcp"
	"github.com/EBTURKgit/cortex/internal/progress"
	"github.com/EBTURKgit/cortex/internal/server"

	"github.com/spf13/cobra"
)

// Global state shared across commands.
var (
	cfg        *config.Config
	engine     *graph.GraphEngine
	checklist  *progress.Checklist
	projectDir string
)

// RootCmd is the base command for the cortex CLI.
var RootCmd = &cobra.Command{
	Use:   "cortex",
	Short: "Project Cortex — Graph-based memory for AI coding tools",
	Long: `Cortex provides persistent, graph-based project memory for AI coding tools.
It indexes your codebase into a knowledge graph and exposes it via:
  - MCP server (for opencode, Cursor, Claude Code)
  - REST API + WebSocket (for custom tooling)
  - CLI queries (for terminal use)

Usage:
  cortex init              Create a new cortex project
  cortex serve             Start the graph server
  cortex dev up/down       Start/stop the server in background
  cortex import <path>     Index an existing codebase
  cortex query <question>  Query the graph
  cortex mcp               Run the MCP server (for AI tool integration)
  cortex config get/set    View or change configuration
  cortex progress          Show build progress`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Skip config loading for init and help commands
		if cmd.Name() == "init" || cmd.Name() == "help" || cmd.Name() == "completion" {
			return
		}

		// Find and load project config
		dir, _ := os.Getwd()
		configPath, err := config.FindProjectConfig(dir)
		if err != nil {
			// Not in a cortex project — that's ok for some commands
			logging.Debug("Not in a cortex project", map[string]interface{}{"dir": dir})
			return
		}
		projectDir = filepath.Dir(configPath)

		cfg, err = config.LoadConfig(configPath)
		if err != nil {
			logging.Warn("Failed to load config", map[string]interface{}{"error": err.Error()})
		}
	},
}

// Execute runs the root command.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func init() {
	// Init command
	RootCmd.AddCommand(initCmd)

	// Serve command
	RootCmd.AddCommand(serveCmd)

	// Dev commands (up, down, status)
	RootCmd.AddCommand(devCmd)
	devCmd.AddCommand(devUpCmd)
	devCmd.AddCommand(devDownCmd)
	devCmd.AddCommand(devStatusCmd)

	// Config commands
	RootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)

	// Import command
	RootCmd.AddCommand(importCmd)

	// Query command
	RootCmd.AddCommand(queryCmd)

	// MCP command
	RootCmd.AddCommand(mcpCmd)

	// Agent command
	RootCmd.AddCommand(agentCmd)
	agentCmd.AddCommand(agentRunCmd)
	agentCmd.AddCommand(agentStatusCmd)

	// Goal command (uses manager agent to plan)
	RootCmd.AddCommand(goalCmd)

	// Dev subcommands
	devCmd.AddCommand(devLogsCmd)

	// Status / Task / Graph dashboard commands
	RootCmd.AddCommand(statusCmd)
	RootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskApproveCmd)
	taskCmd.AddCommand(taskRejectCmd)
	RootCmd.AddCommand(graphCmd)
	graphCmd.AddCommand(graphNodesCmd)

	// Index commands
	RootCmd.AddCommand(indexCmd)
	indexCmd.AddCommand(indexStatusCmd)

	// Project commands
	RootCmd.AddCommand(projectCmd)
	projectCmd.AddCommand(projectStatusCmd)
	projectCmd.AddCommand(projectCreateCmd)

	// Watch command
	RootCmd.AddCommand(watchCmd)

	// Doctor command
	RootCmd.AddCommand(doctorCmd)

	// Progress command
	RootCmd.AddCommand(progressCmd)

	// Version flag
	RootCmd.AddCommand(versionCmd)
}

// ============================================================
// cortex init
// ============================================================

var initCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Initialize a new Cortex project",
	Long:  `Creates a cortex.yaml configuration file in the current directory,
setting up the project for indexing and graph storage.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logging.Info("Initializing cortex project")

		name := "my-project"
		if len(args) > 0 {
			name = args[0]
		}

		cfg := config.DefaultConfig()
		cfg.Project.Name = name
		cfg.Project.RootPath = "."

		path := filepath.Join(projectDir, "cortex.yaml")
		if projectDir == "" {
			path = "cortex.yaml"
		}

		if err := config.SaveConfig(path, cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		// Create .cortex directory
		cortexDir := filepath.Join(filepath.Dir(path), ".cortex")
		if err := os.MkdirAll(cortexDir, 0755); err != nil {
			return fmt.Errorf("failed to create .cortex dir: %w", err)
		}

		fmt.Printf("✅ Initialized cortex project '%s'\n", name)
		fmt.Printf("   Config: %s\n", path)
		fmt.Printf("   Run 'cortex import .' to index your codebase\n")
		fmt.Printf("   Run 'cortex serve' to start the graph server\n")

		return nil
	},
}

// ============================================================
// cortex serve
// ============================================================

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Cortex graph server",
	Long:  `Starts the graph server with REST API + WebSocket on port 8741 (default).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logging.Info("Starting cortex server")

		if cfg == nil {
			// Use default config
			cfg = config.DefaultConfig()
		}

		// Create graph engine (load from disk if available)
		engine = graph.New()
		if loaded, err := graph.LoadFromFile(".cortex/graph.json"); err == nil && loaded.Stats().NodeCount > 0 {
			engine = loaded
			logging.Debug("Loaded graph from disk for serve", map[string]interface{}{
				"nodes": loaded.Stats().NodeCount,
			})
		}

		// Create and start server
		srv := server.New(engine, cfg.Server.Port)

		// Handle graceful shutdown + autosave
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		// Periodic autosave every 60s
		autosave := time.NewTicker(60 * time.Second)
		defer autosave.Stop()

		go func() {
			for {
				select {
				case <-sigCh:
					logging.Info("Shutting down...")
					// Save graph before shutdown
					if err := engine.SaveToFile(".cortex/graph.json"); err != nil {
						logging.Error("Failed to save graph on shutdown", map[string]interface{}{"error": err.Error()})
					}
					shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer shutdownCancel()
					if err := srv.Shutdown(shutdownCtx); err != nil {
						logging.Error("Shutdown error", map[string]interface{}{"error": err.Error()})
					}
					return
				case <-autosave.C:
					if engine.Stats().NodeCount > 0 {
						logging.Debug("Autosaving graph...")
						if err := engine.SaveToFile(".cortex/graph.json"); err != nil {
							logging.Error("Autosave failed", map[string]interface{}{"error": err.Error()})
						}
					}
				}
			}
		}()

		fmt.Printf("🧠 Cortex graph server running on http://127.0.0.1:%d\n", cfg.Server.Port)
		fmt.Println("   Press Ctrl+C to stop")

		return srv.Start()
	},
}

// ============================================================
// cortex dev {up,down,status}
// ============================================================

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Manage the local dev server",
}

var devUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Start the cortex server in background",
	RunE: func(cmd *cobra.Command, args []string) error {
		pidPath := pidFilePath()

		// Check if already running
		if pid, err := readPID(pidPath); err == nil {
			if processRunning(pid) {
				fmt.Printf("Cortex is already running (PID %d)\n", pid)
				return nil
			}
		}

		// Start server in background
		port := 8741
		if cfg != nil {
			port = cfg.Server.Port
		}

		args = append([]string{"serve"}, os.Args[3:]...)
		proc, err := os.StartProcess(os.Args[0], append([]string{"cortex", "serve"}, args...), &os.ProcAttr{
			Files: []*os.File{nil, nil, nil},
		})
		if err != nil {
			return fmt.Errorf("start server: %w", err)
		}

		// Save PID
		if err := os.MkdirAll(filepath.Dir(pidPath), 0755); err != nil {
			return fmt.Errorf("create pid dir: %w", err)
		}
		if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", proc.Pid)), 0644); err != nil {
			return fmt.Errorf("write pid file: %w", err)
		}

		fmt.Printf("🧠 Cortex server started (PID %d) on http://127.0.0.1:%d\n", proc.Pid, port)
		fmt.Println("   Run 'cortex dev status' to check, 'cortex dev down' to stop")
		return nil
	},
}

var devDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop the cortex server",
	RunE: func(cmd *cobra.Command, args []string) error {
		pidPath := pidFilePath()
		pid, err := readPID(pidPath)
		if err != nil {
			fmt.Println("Cortex is not running")
			return nil
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			os.Remove(pidPath)
			fmt.Println("Cortex is not running")
			return nil
		}

		if err := proc.Signal(syscall.SIGTERM); err != nil {
			// Try kill
			proc.Kill()
		}

		os.Remove(pidPath)
		fmt.Printf("Cortex server stopped (PID %d)\n", pid)
		return nil
	},
}

var devStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if the cortex server is running",
	RunE: func(cmd *cobra.Command, args []string) error {
		pidPath := pidFilePath()
		pid, err := readPID(pidPath)
		if err != nil || !processRunning(pid) {
			fmt.Println("🧠 Cortex: not running")
			return nil
		}
		fmt.Printf("🧠 Cortex: running (PID %d)\n", pid)
		return nil
	},
}

func pidFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cortex", "cortex.pid")
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func processRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// ============================================================
// cortex config
// ============================================================

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View or change configuration",
}

var configGetCmd = &cobra.Command{
	Use:   "get [key]",
	Short: "Get a configuration value (e.g., 'server.port')",
	RunE: func(cmd *cobra.Command, args []string) error {
		if cfg == nil {
			cfg = config.DefaultConfig()
		}

		if len(args) == 0 {
			// Print all config
			fmt.Printf("Project: %s\n", cfg.Project.Name)
			fmt.Printf("Server:  %s:%d\n", cfg.Server.Host, cfg.Server.Port)
			fmt.Printf("Storage: %s\n", cfg.Server.Storage)
			fmt.Printf("Agents enabled: ")
			first := true
			for name, a := range cfg.Agents {
				if a.Enabled {
					if !first {
						fmt.Print(", ")
					}
					fmt.Print(name)
					first = false
				}
			}
			if first {
				fmt.Print("none")
			}
			fmt.Println()
			return nil
		}

		// Simple key lookup (e.g., "server.port")
		key := args[0]
		switch key {
		case "project.name":
			fmt.Println(cfg.Project.Name)
		case "server.host":
			fmt.Println(cfg.Server.Host)
		case "server.port":
			fmt.Println(cfg.Server.Port)
		case "server.storage":
			fmt.Println(cfg.Server.Storage)
		default:
			return fmt.Errorf("unknown config key: %s", key)
		}
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 2 {
			return fmt.Errorf("usage: cortex config set <key> <value>")
		}

		key := args[0]
		value := args[1]

		if cfg == nil {
			cfg = config.DefaultConfig()
		}

		switch key {
		case "project.name":
			cfg.Project.Name = value
		case "server.host":
			cfg.Server.Host = value
		case "server.port":
			port, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("port must be a number")
			}
			cfg.Server.Port = port
		case "server.storage":
			cfg.Server.Storage = value
		default:
			return fmt.Errorf("unknown config key: %s", key)
		}

		// Save
		configPath, err := config.FindProjectConfig(projectDir)
		if err != nil {
			return fmt.Errorf("no cortex.yaml found: %w", err)
		}
		if err := config.SaveConfig(configPath, cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		fmt.Printf("✅ Set %s = %s\n", key, value)
		return nil
	},
}

// ============================================================
// cortex import
// ============================================================

var importCmd = &cobra.Command{
	Use:   "import [path]",
	Short: "Index a codebase into the Cortex graph",
	Long: `Scans the given directory and creates graph nodes for all source files,
functions, classes, methods, and modules. This is the primary way to
give Cortex knowledge of an existing project.

Usage:
  cortex import ./my-project
  cortex import /home/user/project`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rootPath := "."
		if len(args) > 0 {
			rootPath = args[0]
		}

		// Resolve absolute path
		absPath, err := filepath.Abs(rootPath)
		if err != nil {
			return fmt.Errorf("resolve path: %w", err)
		}

		// Verify path exists
		if info, err := os.Stat(absPath); err != nil {
			return fmt.Errorf("path does not exist: %s", absPath)
		} else if !info.IsDir() {
			return fmt.Errorf("path is not a directory: %s", absPath)
		}

		logging.Info("Importing codebase", map[string]interface{}{"path": absPath})

		// Create graph engine
		engine = graph.New()

		// Create project node
		projectName := filepath.Base(absPath)
		engine.CreateNode(graph.NodeTypeProject, map[string]interface{}{
			"name":     projectName,
			"root_path": absPath,
		})

		// Determine ignore patterns
		ignore := []string{".git", "node_modules", "vendor", ".cortex", "bin", "dist"}
		if cfg != nil {
			ignore = append(ignore, cfg.Project.Ignore...)
		}

		// Create indexer
		idx, err := indexer.New(engine, absPath, ignore)
		if err != nil {
			return fmt.Errorf("create indexer: %w", err)
		}

		// Run scan
		result, err := idx.Scan()
		if err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}

		// Print results
		fmt.Println()
		fmt.Printf("✅ Import complete: %s\n", projectName)
		fmt.Printf("   Files scanned:  %d\n", result.FileCount)
		fmt.Printf("   Entities found: %d\n", result.EntitiesFound)
		fmt.Printf("   Duration:       %s\n", result.Duration.Round(time.Millisecond))
		if len(result.Errors) > 0 {
			fmt.Printf("   Errors:         %d\n", len(result.Errors))
			for _, e := range result.Errors[:min(5, len(result.Errors))] {
				fmt.Printf("     - %s\n", e)
			}
		}

		// Print graph stats
		stats := engine.Stats()
		fmt.Println()
		fmt.Printf("   Graph contains %d nodes and %d edges\n", stats.NodeCount, stats.EdgeCount)
		for ntype, count := range stats.NodeTypes {
			if count > 0 {
				fmt.Printf("     %s: %d\n", ntype, count)
			}
		}

		// Save graph to disk for persistence
		// Save to imported project's .cortex directory
		savePath := filepath.Join(absPath, ".cortex", "graph.json")
		if err := engine.SaveToFile(savePath); err != nil {
			logging.Warn("Failed to save graph to project dir", map[string]interface{}{"error": err.Error()})
		} else {
			fmt.Printf("\n   Graph saved to: %s\n", savePath)
		}
		// Also save to current working directory for easy query access
		if cwd, _ := os.Getwd(); cwd != absPath {
			cwdSavePath := filepath.Join(cwd, ".cortex", "graph.json")
			if err := engine.SaveToFile(cwdSavePath); err != nil {
				logging.Debug("Could not save graph to cwd", map[string]interface{}{"error": err.Error()})
			}
		}

		return nil
	},
}

// ============================================================
// cortex query
// ============================================================

var queryCmd = &cobra.Command{
	Use:   "query <question>",
	Short: "Query the Cortex graph",
	Long: `Ask a question about the indexed codebase. Cortex will search its
graph for relevant information.

Examples:
  cortex query stats
  cortex query Function
  cortex query UserService
  cortex query "What functions does auth.php contain?"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("provide a query string")
		}

		query := strings.Join(args, " ")

		// Try loading graph from disk (cached from last import)
		if engine == nil {
			cwd, _ := os.Getwd()
			candidatePaths := []string{
				filepath.Join(projectDir, ".cortex", "graph.json"),
				".cortex/graph.json",
				filepath.Join(cwd, ".cortex", "graph.json"),
			}
			for _, p := range candidatePaths {
				if loaded, err := graph.LoadFromFile(p); err == nil && loaded.Stats().NodeCount > 0 {
					engine = loaded
					logging.Debug("Loaded graph from disk", map[string]interface{}{"path": p})
					break
				}
			}
		}

		if engine == nil {
			return fmt.Errorf("no graph available. Run 'cortex import .' first to index your codebase")
		}

		return localQuery(query)
	},
}

func localQuery(query string) error {
	query = strings.ToLower(strings.TrimSpace(query))

	switch {
	case query == "stats":
		stats := engine.Stats()
		fmt.Printf("Graph Stats:\n")
		fmt.Printf("  Total nodes:  %d\n", stats.NodeCount)
		fmt.Printf("  Total edges:  %d\n", stats.EdgeCount)
		fmt.Printf("  Node types:\n")
		for ntype, count := range stats.NodeTypes {
			fmt.Printf("    %s: %d\n", ntype, count)
		}
		fmt.Printf("  Edge types:\n")
		for etype, count := range stats.EdgeTypes {
			fmt.Printf("    %s: %d\n", etype, count)
		}

	default:
		// Try as node type
		nodes := engine.FindNodesByType(query)
		if len(nodes) > 0 {
			fmt.Printf("Found %d nodes of type '%s':\n", len(nodes), query)
			for _, n := range nodes {
				name := n.Properties["name"]
				path := n.Properties["relative_path"]
				sig := n.Properties["signature"]
				fmt.Printf("  - %s", name)
				if path != nil {
					fmt.Printf(" (%s)", path)
				}
				if sig != nil {
					fmt.Printf(" %s", sig)
				}
				fmt.Println()
			}
			return nil
		}

		// Try as name search
		nodes = engine.FindNodeByName(query)
		if len(nodes) > 0 {
			fmt.Printf("Found %d nodes matching '%s':\n", len(nodes), query)
			for _, n := range nodes {
				name := n.Properties["name"]
				path := n.Properties["relative_path"]
				fmt.Printf("  [%s] %s", n.Type, name)
				if path != nil {
					fmt.Printf(" (%s)", path)
				}
				fmt.Println()
			}
			return nil
		}

		fmt.Printf("No results found for '%s'\n", query)
	}

	return nil
}

// ============================================================
// cortex mcp
// ============================================================

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run the MCP server (for AI tool integration)",
	Long: `Runs the MCP (Model Context Protocol) server on stdio transport.
This is used by AI coding tools like opencode, Cursor, and Claude Code
to query the Cortex graph for context.

Usage:
  cortex mcp                          # Connect to local graph
  cortex mcp --server localhost:8741  # Connect to remote graph

Tools exposed:
  - graph_query          Natural-language graph queries
  - context_for_file     Get scope for a specific file
  - context_for_symbol   Get definition, callers, callees
  - search_code          Full-text search across code
  - get_project_structure  File/module tree`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logging.Info("Starting MCP server")

		// Create graph engine if not already created
		if engine == nil {
			engine = graph.New()
			// Try loading existing graph data from disk
			candidatePaths := []string{
				filepath.Join(projectDir, ".cortex", "graph.json"),
				".cortex/graph.json",
			}
			for _, p := range candidatePaths {
				if loaded, err := graph.LoadFromFile(p); err == nil && loaded.Stats().NodeCount > 0 {
					engine = loaded
					logging.Debug("Loaded graph from disk for MCP", map[string]interface{}{"path": p})
					break
				}
			}
		}

		// Report status
		stats := engine.Stats()
		if stats.NodeCount == 0 {
			fmt.Fprintf(os.Stderr, "Cortex MCP server ready (graph is empty — run 'cortex import' first)\n")
		} else {
			fmt.Fprintf(os.Stderr, "Cortex MCP server ready (%d nodes, %d edges)\n",
				stats.NodeCount, stats.EdgeCount)
		}

		// Create and run MCP server
		mcpSrv := mcp.New(engine)
		return mcpSrv.Run()
	},
}

// ============================================================
// cortex goal
// ============================================================

var goalCmd = &cobra.Command{
	Use:   "goal <description>",
	Short: "Plan a project from a natural language goal",
	Long: `Uses the Manager Agent (LLM) to generate a complete project plan
from a high-level goal description. The plan includes modules, database
schema, API endpoints, and a task tree with dependencies.

The plan is written into the Cortex graph as Decision, Module, 
DatabaseSchema, Endpoint, and Task nodes.

Examples:
  cortex goal "Build a blog with PHP and MySQL"
  cortex goal "Create a REST API for a todo app in Go"
  cortex goal "Build a forum like phpBB with no framework"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("provide a goal description")
		}

		goal := strings.Join(args, " ")

		// Create/load graph engine
		if engine == nil {
			engine = graph.New()
			// Try loading from disk
			if cfg != nil {
				if loaded, err := graph.LoadFromFile(
					filepath.Join(projectDir, ".cortex", "graph.json")); err == nil {
					engine = loaded
				}
			}
		}

		// Create LLM client (use default config)
		llmCfg := config.LLMConfig{
			Provider: "ollama",
			Model:    "codellama:7b",
			Endpoint: "http://localhost:11434",
		}
		if cfg != nil {
			if c, ok := cfg.LLM["default"]; ok {
				llmCfg = c
			}
		}

		llmClient, err := agent.NewLLMClient(llmCfg)
		if err != nil {
			return fmt.Errorf("create LLM client: %w", err)
		}

		// Create manager agent
		manager := agent.NewManager(agent.AgentConfig{
			ServerURL: fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port),
			LLM:       llmClient,
			Model:     llmCfg.Model,
		})

		fmt.Printf("🧠 Planning from goal: %s\n", goal)
		fmt.Printf("   Using LLM: %s\n", llmClient.Name())
		fmt.Println("   Generating plan... (this may take a moment)")

		// Generate plan
		plan, err := manager.PlanFromGoal(context.Background(), goal)
		if err != nil {
			return fmt.Errorf("plan generation failed: %w", err)
		}

		// Display plan summary
		fmt.Println()
		fmt.Printf("✅ Plan generated!\n")
		fmt.Printf("   Summary: %s\n", plan.Summary)
		fmt.Printf("   Modules: %d\n", len(plan.Modules))
		fmt.Printf("   Tables:  %d\n", len(plan.Schema))
		fmt.Printf("   Endpoints: %d\n", len(plan.Endpoints))
		fmt.Printf("   Tasks:   %d\n", len(plan.Tasks))
		fmt.Println()

		// Show modules
		if len(plan.Modules) > 0 {
			fmt.Println("  Modules:")
			for _, m := range plan.Modules {
				fmt.Printf("    - %s: %s\n", m.Name, m.Description)
			}
		}

		// Show tasks
		if len(plan.Tasks) > 0 {
			fmt.Println()
			fmt.Println("  Task Plan:")
			for _, t := range plan.Tasks {
				deps := ""
				if len(t.DependsOn) > 0 {
					deps = fmt.Sprintf(" (after: %s)", strings.Join(t.DependsOn, ", "))
				}
				fmt.Printf("    [%s] %s%s\n", t.AgentType, t.Title, deps)
			}
		}

		// Ask user if they want to execute the plan
		fmt.Println()
		fmt.Print("Write this plan to the graph? (y/N): ")
		var confirm string
		fmt.Scanln(&confirm)
		if confirm != "y" && confirm != "Y" {
			fmt.Println("Plan discarded.")
			return nil
		}

		// Create project node (only after confirmation)
		projectName := "project"
		if cfg != nil {
			projectName = cfg.Project.Name
		}
		engine.CreateNode(graph.NodeTypeProject, map[string]interface{}{
			"name":        projectName,
			"description": goal,
		})

		// Execute plan into graph
		if err := manager.ExecutePlan(engine, plan); err != nil {
			return fmt.Errorf("execute plan: %w", err)
		}

		// Save graph to disk
		savePath := filepath.Join(projectDir, ".cortex", "graph.json")
		if projectDir == "" {
			savePath = ".cortex/graph.json"
		}
		if err := engine.SaveToFile(savePath); err != nil {
			logging.Warn("Failed to save graph", map[string]interface{}{"error": err.Error()})
		}

		fmt.Println("✅ Plan written to graph!")
		fmt.Println("   Agents will pick up tasks when they connect.")

		return nil
	},
}

// ============================================================
// cortex agent
// ============================================================

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage AI agents",
}

var agentRunCmd = &cobra.Command{
	Use:   "run <type>",
	Short: "Start an AI agent that connects to the graph server",
	Long: `Starts an AI agent process that connects to the Cortex graph server,
subscribes to task assignments, and processes them using an LLM.

Agent types: manager, architect, backend, frontend, database, qa, devops, documentation, security

Examples:
  cortex agent run backend          # Start a backend developer agent
  cortex agent run manager          # Start the manager/orchestrator agent
  cortex agent run qa --llm ollama  # Start QA agent with local LLM`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("provide agent type: manager, architect, backend, frontend, database, qa, devops, documentation, security")
		}

		agentType := agent.AgentType(args[0])

		// Determine server URL
		serverURL := "http://127.0.0.1:8741"
		if cfg != nil {
			serverURL = fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)
		}

		// Create LLM client from config or default
		var llmClient agent.LLMClient
		var err error

		// Check for specific LLM config for this agent type
		llmConfig, ok := cfg.LLM[string(agentType)]
		if !ok {
			// Fall back to default LLM config
			defaultCfg, ok := cfg.LLM["default"]
			if !ok {
				// Use hardcoded default
				defaultCfg = config.LLMConfig{
					Provider: "ollama",
					Model:    "codellama:7b",
					Endpoint: "http://localhost:11434",
				}
			}
			llmConfig = defaultCfg
		}

		llmClient, err = agent.NewLLMClient(llmConfig)
		if err != nil {
			return fmt.Errorf("create LLM client: %w", err)
		}

		fmt.Printf("🧠 Starting %s agent (LLM: %s)\n", agentType, llmClient.Name())
		fmt.Printf("   Connecting to graph server at %s\n", serverURL)

		// Create the agent
		agt := agent.New(agent.AgentConfig{
			Type:      agentType,
			ServerURL: serverURL,
			LLM:       llmClient,
			Model:     llmConfig.Model,
		})

		// Attach local engine if available (for caching)
		if engine != nil {
			agt.SetGraphEngine(engine)
		}

		// Connect to server
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := agt.Connect(ctx); err != nil {
			return fmt.Errorf("agent connect: %w", err)
		}

		fmt.Printf("✅ Agent %s connected (ID: %s)\n", agentType, agt.ID)
		fmt.Println("   Waiting for tasks... Press Ctrl+C to stop")

		// Run event loop (blocks until signal)
		return agt.Run(ctx)
	},
}

var agentStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "List connected agents and their status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if engine == nil {
			if loaded, err := graph.LoadFromFile(".cortex/graph.json"); err == nil && loaded.Stats().NodeCount > 0 {
				engine = loaded
			}
		}
		if engine == nil {
			fmt.Println("No graph loaded. Run 'cortex import .' first.")
			return nil
		}

		agents := engine.FindNodesByType(graph.NodeTypeAgent)
		if len(agents) == 0 {
			fmt.Println("No agents registered.")
			return nil
		}

		fmt.Printf("%-8s %-12s %-8s %s\n", "STATUS", "TYPE", "ID", "CONNECTED")
		fmt.Println(strings.Repeat("-", 60))
		for _, a := range agents {
			status, _ := a.Properties["status"].(string)
			agentType, _ := a.Properties["agent_type"].(string)
			agentID, _ := a.Properties["agent_id"].(string)
			connected, _ := a.Properties["connected_at"].(string)
			fmt.Printf("%-8s %-12s %-8s %s\n", status, agentType, agentID[:min(8, len(agentID))], connected[:19])
		}
		return nil
	},
}

// ============================================================
// cortex index
// ============================================================

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Manage code indexing",
}

var indexStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show indexing status",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Indexing status: use 'cortex import .' to re-index")
		return nil
	},
}

// ============================================================
// cortex progress
// ============================================================

var progressCmd = &cobra.Command{
	Use:   "progress",
	Short: "Show Cortex build progress",
	RunE: func(cmd *cobra.Command, args []string) error {
		checklistPath := filepath.Join(projectDir, ".cortex", "progress.json")
		if projectDir == "" {
			home, _ := os.UserHomeDir()
			checklistPath = filepath.Join(home, ".cortex", "progress.json")
		}

		cl, err := progress.LoadChecklist(checklistPath)
		if err != nil {
			cl = progress.NewChecklist(checklistPath)
			progress.RegisterBuildSteps(cl)
		}

		progress.PrintSummary(cl.List())
		return nil
	},
}

// ============================================================
// cortex status
// ============================================================

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show project overview and graph statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		if engine == nil {
			// Try loading from disk
			candidatePaths := []string{
				filepath.Join(projectDir, ".cortex", "graph.json"),
				".cortex/graph.json",
			}
			for _, p := range candidatePaths {
				if loaded, err := graph.LoadFromFile(p); err == nil && loaded.Stats().NodeCount > 0 {
					engine = loaded
					break
				}
			}
		}

		if engine == nil {
			return fmt.Errorf("no graph available. Run 'cortex import .' first")
		}

		stats := engine.Stats()

		fmt.Println("╔══════════════════════════════════════╗")
		fmt.Println("║        Cortex Project Status         ║")
		fmt.Println("╚══════════════════════════════════════╝")
		fmt.Println()
		fmt.Printf("  Project: %s\n", func() string {
			if cfg != nil {
				return cfg.Project.Name
			}
			return "unknown"
		}())
		fmt.Println()
		fmt.Println("  Graph:")
		fmt.Printf("    Nodes: %d\n", stats.NodeCount)
		fmt.Printf("    Edges: %d\n", stats.EdgeCount)
		if len(stats.NodeTypes) > 0 {
			fmt.Println("    By type:")
			for ntype, count := range stats.NodeTypes {
				fmt.Printf("      %s: %d\n", ntype, count)
			}
		}
		fmt.Println()
		fmt.Println("  Agents:")
		agents := engine.FindNodesByType(graph.NodeTypeAgent)
		if len(agents) > 0 {
			for _, a := range agents {
				status, _ := a.Properties["status"].(string)
				agentType, _ := a.Properties["agent_type"].(string)
				fmt.Printf("    [%s] %s (%s)\n", status, agentType, a.Properties["agent_id"])
			}
		} else {
			fmt.Println("    No agents connected")
		}

		tasks := engine.FindNodesByType(graph.NodeTypeTask)
		if len(tasks) > 0 {
			fmt.Println()
			fmt.Println("  Tasks:")
			pending, working, completed, failed := 0, 0, 0, 0
			for _, t := range tasks {
				switch t.Properties["status"] {
				case "pending":
					pending++
				case "in_progress":
					working++
				case "completed", "verified":
					completed++
				case "failed":
					failed++
				}
			}
			fmt.Printf("    Total: %d | Pending: %d | Working: %d | Done: %d | Failed: %d\n",
				len(tasks), pending, working, completed, failed)
		}

		return nil
	},
}

// ============================================================
// cortex task
// ============================================================

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Manage tasks in the graph",
}

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tasks",
	RunE: func(cmd *cobra.Command, args []string) error {
		if engine == nil {
			loaded, err := graph.LoadFromFile(".cortex/graph.json")
			if err != nil {
				return fmt.Errorf("no graph available: %w", err)
			}
			engine = loaded
		}

		tasks := engine.FindNodesByType(graph.NodeTypeTask)
		if len(tasks) == 0 {
			fmt.Println("No tasks found.")
			return nil
		}

		fmt.Printf("%-8s %-20s %-12s %-10s %s\n", "STATUS", "TITLE", "AGENT", "PRIORITY", "ID")
		fmt.Println(strings.Repeat("-", 80))
		for _, t := range tasks {
			title, _ := t.Properties["title"].(string)
			status, _ := t.Properties["status"].(string)
			agentType, _ := t.Properties["assigned_agent_type"].(string)
			priority, _ := t.Properties["priority"].(float64)
			if len(title) > 20 {
				title = title[:17] + "..."
			}
			fmt.Printf("%-8s %-20s %-12s %-10.0f %s\n",
				status, title, agentType, priority, t.UUID[:8])
		}
		return nil
	},
}

var taskApproveCmd = &cobra.Command{
	Use:   "approve <task-id>",
	Short: "Approve a proposed change",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("provide task UUID")
		}
		if engine == nil {
			loaded, err := graph.LoadFromFile(".cortex/graph.json")
			if err != nil {
				return fmt.Errorf("no graph available: %w", err)
			}
			engine = loaded
		}

		// Find task by UUID prefix
		tasks := engine.FindNodesByType(graph.NodeTypeTask)
		for _, t := range tasks {
			if strings.HasPrefix(t.UUID, args[0]) {
				engine.UpdateNode(t.UUID, map[string]interface{}{
					"status": "approved",
				})
				fmt.Printf("✅ Task %s approved\n", t.UUID[:8])
				return nil
			}
		}
		return fmt.Errorf("task not found: %s", args[0])
	},
}

var taskRejectCmd = &cobra.Command{
	Use:   "reject <task-id>",
	Short: "Reject a proposed change",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("provide task UUID")
		}
		if engine == nil {
			loaded, err := graph.LoadFromFile(".cortex/graph.json")
			if err != nil {
				return fmt.Errorf("no graph available: %w", err)
			}
			engine = loaded
		}

		reason := ""
		for i, a := range args[1:] {
			if i > 0 {
				reason += " "
			}
			reason += a
		}

		tasks := engine.FindNodesByType(graph.NodeTypeTask)
		for _, t := range tasks {
			if strings.HasPrefix(t.UUID, args[0]) {
				engine.UpdateNode(t.UUID, map[string]interface{}{
					"status": "rejected",
					"comment": reason,
				})
				fmt.Printf("❌ Task %s rejected", t.UUID[:8])
				if reason != "" {
					fmt.Printf(": %s", reason)
				}
				fmt.Println()
				return nil
			}
		}
		return fmt.Errorf("task not found: %s", args[0])
	},
}

// ============================================================
// cortex dev logs
// ============================================================

var devLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail cortex server logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		logPath := filepath.Join(projectDir, ".cortex", "server.log")
		if projectDir == "" {
			home, _ := os.UserHomeDir()
			logPath = filepath.Join(home, ".cortex", "server.log")
		}
		return fmt.Errorf("log file not found at %s (server logs are written to stderr by default)", logPath)
	},
}

// ============================================================
// cortex project
// ============================================================

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage projects",
}

var projectStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current project status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if engine == nil {
			if loaded, err := graph.LoadFromFile(".cortex/graph.json"); err == nil && loaded.Stats().NodeCount > 0 {
				engine = loaded
			}
		}
		if engine == nil {
			fmt.Println("No project loaded. Run 'cortex import .' first.")
			return nil
		}
		stats := engine.Stats()
		projects := engine.FindNodesByType(graph.NodeTypeProject)
		for _, p := range projects {
			name, _ := p.Properties["name"].(string)
			desc, _ := p.Properties["description"].(string)
			fmt.Printf("Project: %s\n", name)
			if desc != "" {
				fmt.Printf("  Description: %s\n", desc)
			}
		}
		fmt.Printf("Graph: %d nodes, %d edges\n", stats.NodeCount, stats.EdgeCount)
		return nil
	},
}

var projectCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if engine == nil {
			engine = graph.New()
		}
		node, err := engine.CreateNode(graph.NodeTypeProject, map[string]interface{}{
			"name": args[0],
		})
		if err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		fmt.Printf("✅ Project '%s' created (UUID: %s)\n", args[0], node.UUID[:8])
		return nil
	},
}

// ============================================================
// cortex watch
// ============================================================

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch the filesystem for changes and auto-reindex",
	RunE: func(cmd *cobra.Command, args []string) error {
		rootPath := "."
		if len(args) > 0 {
			rootPath = args[0]
		}
		absPath, err := filepath.Abs(rootPath)
		if err != nil {
			return fmt.Errorf("resolve path: %w", err)
		}

		if engine == nil {
			engine = graph.New()
			if loaded, err := graph.LoadFromFile(filepath.Join(absPath, ".cortex", "graph.json")); err == nil && loaded.Stats().NodeCount > 0 {
				engine = loaded
			}
		}

		ignore := []string{".git", "node_modules", "vendor", ".cortex", "bin", "dist"}
		idx, err := indexer.New(engine, absPath, ignore)
		if err != nil {
			return fmt.Errorf("create indexer: %w", err)
		}

		fmt.Printf("Watching %s for changes... (Ctrl+C to stop)\n", absPath)
		return idx.Watch()
	},
}

// ============================================================
// cortex graph
// ============================================================

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Inspect the knowledge graph",
}

var graphNodesCmd = &cobra.Command{
	Use:   "nodes [type]",
	Short: "List graph nodes, optionally filtered by type",
	RunE: func(cmd *cobra.Command, args []string) error {
		if engine == nil {
			loaded, err := graph.LoadFromFile(".cortex/graph.json")
			if err != nil {
				return fmt.Errorf("no graph available: %w", err)
			}
			engine = loaded
		}

		nodeType := ""
		if len(args) > 0 {
			nodeType = args[0]
		}

		var nodes []*graph.Node
		if nodeType != "" {
			nodes = engine.FindNodesByType(nodeType)
		} else {
			stats := engine.Stats()
			for ntype := range stats.NodeTypes {
				nodes = append(nodes, engine.FindNodesByType(ntype)...)
			}
		}

		if len(nodes) == 0 {
			fmt.Println("No nodes found.")
			return nil
		}

		fmt.Printf("%-36s %-15s %s\n", "UUID", "TYPE", "NAME / PATH")
		fmt.Println(strings.Repeat("-", 80))
		for _, n := range nodes {
			name := ""
			if v, ok := n.Properties["name"]; ok {
				name = fmt.Sprintf("%v", v)
			} else if v, ok := n.Properties["relative_path"]; ok {
				name = fmt.Sprintf("%v", v)
			} else if v, ok := n.Properties["title"]; ok {
				name = fmt.Sprintf("%v", v)
			}
			fmt.Printf("%-36s %-15s %s\n", n.UUID, n.Type, name)
		}
		return nil
	},
}

// ============================================================
// cortex doctor
// ============================================================

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system dependencies and configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("🧠 Cortex System Check")
		fmt.Println()

		checks := 0
		passed := 0

		// Check Go
		checks++
		if _, err := exec.LookPath("go"); err == nil {
			fmt.Println("  ✅ Go installed")
			passed++
		} else {
			fmt.Println("  ❌ Go not found (needed to build Go projects)")
		}

		// Check Git
		checks++
		if _, err := exec.LookPath("git"); err == nil {
			fmt.Println("  ✅ Git installed")
			passed++
		} else {
			fmt.Println("  ❌ Git not found")
		}

		// Check Docker
		checks++
		if _, err := exec.LookPath("docker"); err == nil {
			fmt.Println("  ✅ Docker installed")
			passed++
		} else {
			fmt.Println("  ⚠️  Docker not found (optional, for agent sandboxing)")
		}

		// Check Python
		checks++
		if _, err := exec.LookPath("python3"); err == nil {
			fmt.Println("  ✅ Python 3 installed")
			passed++
		} else {
			fmt.Println("  ⚠️  Python 3 not found (optional)")
		}

		// Check config
		checks++
		configPath, err := config.FindProjectConfig(projectDir)
		if err == nil {
			fmt.Printf("  ✅ Config found: %s\n", configPath)
			passed++
		} else {
			fmt.Println("  ⚠️  No cortex.yaml found (run 'cortex init')")
		}

		// Check graph data
		checks++
		if _, err := os.Stat(filepath.Join(projectDir, ".cortex", "graph.json")); err == nil {
			fmt.Println("  ✅ Graph data found")
			passed++
		} else {
			fmt.Println("  ⚠️  No graph data (run 'cortex import .')")
		}

		// Check LLM connectivity
		checks++
		if cfg != nil {
			if llm, ok := cfg.LLM["default"]; ok && llm.Provider == "ollama" {
				if _, err := exec.LookPath("ollama"); err == nil {
					fmt.Println("  ✅ Ollama installed")
					passed++
				} else {
					fmt.Println("  ⚠️  Ollama not found (needed for local LLM)")
				}
			} else {
				fmt.Println("  ✅ LLM configured (API-based)")
				passed++
			}
		} else {
			fmt.Println("  ⚠️  No LLM configured")
		}

		fmt.Println()
		fmt.Printf("  %d/%d checks passed\n", passed, checks)
		if passed < checks {
			fmt.Println("  Some checks failed — see above for details.")
		}
		return nil
	},
}

// ============================================================
// cortex version
// ============================================================

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Cortex v0.1.0")
	},
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
