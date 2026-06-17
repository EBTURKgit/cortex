// Package agent provides the runtime framework for AI agents that connect
// to the Cortex graph server, receive tasks, and execute them via LLMs.
//
// Agent Lifecycle:
//  1. Connect to the graph server via WebSocket
//  2. Subscribe to task assignments for their role
//  3. On receiving a task, fetch context from the graph
//  4. Build a prompt and call the LLM
//  5. Parse the LLM response and write results back to the graph
//  6. Update task status
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/EBTURKgit/cortex/internal/graph"
	"github.com/EBTURKgit/cortex/internal/logging"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// AgentType defines the role of an agent.
type AgentType string

const (
	AgentManager   AgentType = "manager"
	AgentArchitect AgentType = "architect"
	AgentBackend   AgentType = "backend"
	AgentFrontend  AgentType = "frontend"
	AgentDatabase  AgentType = "database"
	AgentQA        AgentType = "qa"
	AgentDevOps    AgentType = "devops"
	AgentDocWriter AgentType = "documentation"
	AgentSecurity  AgentType = "security"
)

// Agent represents a single AI agent that connects to the graph server,
// receives tasks, and executes them using an LLM.
type Agent struct {
	ID        string    `json:"id"`
	Type      AgentType `json:"type"`
	Status    string    `json:"status"` // idle, working, offline
	ServerURL string    `json:"server_url"`

	// Dependencies
	llm    LLMClient
	engine *graph.GraphEngine // local graph copy (cached from server)

	// Connection
	conn *websocket.Conn
	done chan struct{}
	mu   sync.Mutex

	// LLM configuration
	systemPrompt string
	model        string
	temperature  float64
	maxTokens    int
}

// AgentConfig holds configuration for creating an agent.
type AgentConfig struct {
	Type         AgentType
	ServerURL    string
	LLM          LLMClient
	SystemPrompt string
	Model        string
	Temperature  float64
	MaxTokens    int
}

// New creates a new agent with the given configuration.
func New(cfg AgentConfig) *Agent {
	id := uuid.New().String()[:8]
	logging.Debug("Creating agent",
		map[string]interface{}{
			"id": id, "type": cfg.Type, "server": cfg.ServerURL,
		})

	if cfg.ServerURL == "" {
		cfg.ServerURL = "http://127.0.0.1:8741"
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = defaultSystemPrompt(cfg.Type)
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.7
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}

	return &Agent{
		ID:           id,
		Type:         cfg.Type,
		Status:       "idle",
		ServerURL:    cfg.ServerURL,
		llm:          cfg.LLM,
		systemPrompt: cfg.SystemPrompt,
		model:        cfg.Model,
		temperature:  cfg.Temperature,
		maxTokens:    cfg.MaxTokens,
		done:         make(chan struct{}),
	}
}

// defaultSystemPrompt returns a default system prompt based on agent type.
func defaultSystemPrompt(agentType AgentType) string {
	switch agentType {
	case AgentManager:
		return `You are a software project manager. Your role is to:
- Interpret high-level user requests
- Create detailed task plans with dependencies
- Assign tasks to specialist agents
- Monitor progress and handle failures
- Never write code yourself — delegate to specialists`

	case AgentArchitect:
		return `You are a software architect. Your role is to:
- Analyze requirements and existing code
- Design system architecture and module structure
- Define database schemas and API contracts
- Write clear Decision nodes with rationale`

	case AgentBackend:
		return `You are a backend developer. Your role is to:
- Implement server-side logic and APIs
- Write clean, tested, idiomatic code
- Follow existing project conventions
- Use the PROPOSES_CHANGE edge for code modifications`

	case AgentFrontend:
		return `You are a frontend developer. Your role is to:
- Build user interfaces and visual components
- Write HTML, CSS, JavaScript/TypeScript
- Ensure responsive and accessible design`

	default:
		return `You are an AI software engineering agent. Complete the assigned task
by analyzing the context from the knowledge graph and producing high-quality code.`
	}
}

// ============================================================
// Connection & Event Loop
// ============================================================

// Connect establishes a WebSocket connection to the graph server and
// registers this agent in the graph.
func (a *Agent) Connect(ctx context.Context) error {
	defer logging.Trace("Agent.Connect", map[string]interface{}{"id": a.ID})()

	wsURL := strings.Replace(a.ServerURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = wsURL + "/ws"

	logging.Info("Agent connecting to server",
		map[string]interface{}{"url": wsURL, "id": a.ID, "type": a.Type})

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}

	a.mu.Lock()
	a.conn = conn
	a.Status = "idle"
	a.mu.Unlock()

	// Register agent in the graph via REST API
	a.registerAgent()

	// Send subscription message for our task type
	subMsg := map[string]interface{}{
		"type":   "subscribe",
		"topics": []string{"all", fmt.Sprintf("project/*/task/%s", a.Type)},
	}
	subData, _ := json.Marshal(subMsg)
	if err := conn.WriteMessage(websocket.TextMessage, subData); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	logging.Info("Agent connected and subscribed",
		map[string]interface{}{"id": a.ID, "type": a.Type})

	return nil
}

// registerAgent creates an Agent node in the graph via the REST API.
func (a *Agent) registerAgent() {
	logging.Debug("Registering agent in graph", map[string]interface{}{"id": a.ID})

	// For now, register via direct engine if available, or skip
	if a.engine != nil {
		a.engine.CreateNode(graph.NodeTypeAgent, map[string]interface{}{
			"agent_id":     a.ID,
			"agent_type":   string(a.Type),
			"status":       "idle",
			"connected_at": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// Run starts the agent's event loop. It listens for task assignments
// and processes them until the context is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	defer logging.Trace("Agent.Run", map[string]interface{}{"id": a.ID})()

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Heartbeat ticker
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	logging.Info("Agent event loop started",
		map[string]interface{}{"id": a.ID, "type": a.Type})

	for {
		select {
		case <-ctx.Done():
			logging.Info("Agent shutting down", map[string]interface{}{"id": a.ID})
			a.mu.Lock()
			a.Status = "offline"
			a.mu.Unlock()
			return nil

		case <-sigCh:
			logging.Info("Agent received signal", map[string]interface{}{"id": a.ID})
			a.mu.Lock()
			a.Status = "offline"
			a.mu.Unlock()
			return nil

		case <-heartbeat.C:
			a.sendHeartbeat()

		default:
			// Read message from WebSocket
			_, message, err := a.conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					logging.Error("WebSocket error", map[string]interface{}{
						"id": a.ID, "error": err.Error(),
					})
					return fmt.Errorf("websocket read: %w", err)
				}
				logging.Info("WebSocket closed", map[string]interface{}{"id": a.ID})
				return nil
			}

			// Parse the event
			var event struct {
				Type     string `json:"type"`
				NodeType string `json:"node_type"`
				EdgeType string `json:"edge_type"`
				EntityID string `json:"entity_id"`
			}
			if err := json.Unmarshal(message, &event); err != nil {
				logging.Debug("Ignoring non-event message",
					map[string]interface{}{"message": string(message)})
				continue
			}

			// Check if this is a task assignment for us
			if event.NodeType == graph.NodeTypeTask {
				logging.Info("Task event received",
					map[string]interface{}{
						"id":   a.ID,
						"task": event.EntityID,
						"type": event.Type,
					})
				a.handleTaskEvent(event.EntityID)
			}
		}
	}
}

// sendHeartbeat sends a heartbeat to keep the connection alive.
func (a *Agent) sendHeartbeat() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.conn == nil {
		return
	}

	err := a.conn.WriteMessage(websocket.PingMessage, nil)
	if err != nil {
		logging.Warn("Heartbeat failed",
			map[string]interface{}{"id": a.ID, "error": err.Error()})
	}
}

// ============================================================
// Task Processing
// ============================================================

// handleTaskEvent processes a task-related event from the graph.
func (a *Agent) handleTaskEvent(taskUUID string) {
	defer logging.Trace("Agent.handleTaskEvent",
		map[string]interface{}{"task": taskUUID})()

	// Mark as working
	a.mu.Lock()
	a.Status = "working"
	a.mu.Unlock()

	// Process the task
	result := a.processTask(taskUUID)

	// Update status back to idle
	a.mu.Lock()
	a.Status = "idle"
	a.mu.Unlock()

	logging.Info("Task processing complete",
		map[string]interface{}{
			"task":   taskUUID,
			"result": result[:min(100, len(result))],
		})
}

// processTask fetches the task from the graph, builds a prompt, calls the LLM,
// and writes the result back.
func (a *Agent) processTask(taskUUID string) string {
	defer logging.Trace("Agent.processTask", map[string]interface{}{"task": taskUUID})()

	// 1. Fetch task details from the graph
	taskInfo := a.fetchTask(taskUUID)
	if taskInfo == nil {
		errMsg := fmt.Sprintf("Task %s not found in graph", taskUUID)
		logging.Error(errMsg)
		return errMsg
	}

	// 2. Fetch context: related code, decisions, schema
	ctxData := a.fetchContext(taskInfo)

	// 3. Build the prompt
	prompt := a.buildPrompt(taskInfo, ctxData)

	// 4. Call the LLM
	llmResp, err := a.llm.Chat(context.Background(), []Message{
		{Role: "system", Content: a.systemPrompt},
		{Role: "user", Content: prompt},
	}, &ChatOptions{
		Model:       a.model,
		Temperature: a.temperature,
		MaxTokens:   a.maxTokens,
	})
	if err != nil {
		errMsg := fmt.Sprintf("LLM error: %s", err.Error())
		logging.Error(errMsg)
		return errMsg
	}

	result := llmResp.Content

	// 5. Write result back to graph
	a.writeResult(taskUUID, result)

	return result
}

// fetchTask retrieves task details from the graph (or local cache).
func (a *Agent) fetchTask(taskUUID string) map[string]interface{} {
	if a.engine == nil {
		// Would fetch via REST API in production
		return map[string]interface{}{
			"uuid":        taskUUID,
			"title":       "Unknown task",
			"description": "No description available",
		}
	}

	node, err := a.engine.GetNode(taskUUID)
	if err != nil {
		return nil
	}

	return map[string]interface{}{
		"uuid":        node.UUID,
		"title":       node.Properties["title"],
		"description": node.Properties["description"],
		"priority":    node.Properties["priority"],
		"status":      node.Properties["status"],
	}
}

// fetchContext gathers relevant code, decisions, and schema from the graph.
func (a *Agent) fetchContext(taskInfo map[string]interface{}) string {
	if a.engine == nil {
		return "No context available (graph not connected)."
	}

	var parts []string

	// Add related decisions
	decisions := a.engine.FindNodesByType(graph.NodeTypeDecision)
	if len(decisions) > 0 {
		parts = append(parts, "=== Previous Decisions ===")
		for _, d := range decisions[:min(5, len(decisions))] {
			if stmt, ok := d.Properties["statement"]; ok {
				parts = append(parts, fmt.Sprintf("- %s", stmt))
			}
		}
	}

	// Add project structure
	files := a.engine.FindNodesByType(graph.NodeTypeFile)
	if len(files) > 0 {
		parts = append(parts, fmt.Sprintf("\n=== Project Files (%d total) ===", len(files)))
		for _, f := range files[:min(10, len(files))] {
			if path, ok := f.Properties["relative_path"]; ok {
				parts = append(parts, fmt.Sprintf("- %s", path))
			}
		}
		if len(files) > 10 {
			parts = append(parts, fmt.Sprintf("... and %d more files", len(files)-10))
		}
	}

	return strings.Join(parts, "\n")
}

// buildPrompt constructs the LLM prompt from task info and context.
func (a *Agent) buildPrompt(taskInfo map[string]interface{}, ctxData string) string {
	title, _ := taskInfo["title"].(string)
	description, _ := taskInfo["description"].(string)

	prompt := fmt.Sprintf(`## Task: %s

### Description
%s

### Context from Knowledge Graph
%s

### Instructions
1. Analyze the task and context carefully
2. Plan your approach before writing code
3. Output your response with clear sections:
   - PLAN: Your approach
   - CODE: Any code changes needed
   - TESTS: How to verify the changes
   - SUMMARY: Brief summary of what was done
`, title, description, ctxData)

	return prompt
}

// writeResult writes the LLM's response back to the graph.
func (a *Agent) writeResult(taskUUID, result string) {
	if a.engine == nil {
		logging.Debug("No graph engine available, result not saved")
		return
	}

	// Update task status
	a.engine.UpdateNode(taskUUID, map[string]interface{}{
		"status":         "completed",
		"result_summary": result[:min(500, len(result))],
	})

	// Create a decision node if the result contains architectural decisions
	if _, err := a.engine.CreateNode(graph.NodeTypeDecision, map[string]interface{}{
		"statement": result[:min(200, len(result))],
		"rationale": fmt.Sprintf("Generated by agent %s (%s)", a.ID, a.Type),
	}); err != nil {
		logging.Warn("Failed to create decision node", map[string]interface{}{"error": err.Error()})
	}

	logging.Debug("Task result written to graph",
		map[string]interface{}{
			"task":   taskUUID,
			"length": len(result),
		})
}

// Disconnect gracefully disconnects from the server.
func (a *Agent) Disconnect() error {
	logging.Debug("Disconnecting agent", map[string]interface{}{"id": a.ID})

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.conn != nil {
		err := a.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"))
		a.conn.Close()
		a.conn = nil
		return err
	}
	return nil
}

// SetGraphEngine attaches a local graph engine for testing/caching.
func (a *Agent) SetGraphEngine(engine *graph.GraphEngine) {
	a.engine = engine
}
