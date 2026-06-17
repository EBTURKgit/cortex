package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/EBTURKgit/cortex/internal/graph"
	"github.com/EBTURKgit/cortex/internal/logging"
)

// ManagerAgent extends the base Agent with planning and orchestration capabilities.
// It receives high-level goals and breaks them down into task trees.
type ManagerAgent struct {
	*Agent
}

// NewManager creates a manager agent with the appropriate system prompt.
func NewManager(cfg AgentConfig) *ManagerAgent {
	cfg.SystemPrompt = `You are a software project manager and architect. Your role is to:

1. INTERPRET high-level user requests into concrete software plans
2. DESIGN system architecture including modules, database schema, and API endpoints
3. DECOMPOSE the plan into small, independent tasks with clear acceptance criteria
4. ORDER tasks using dependencies (DEPENDS_ON edges)
5. ASSIGN tasks to the appropriate specialist agents (backend, frontend, database, qa, etc.)
6. SUPERVISE execution — handle failures, retries, and deadlocks

Your output must be structured JSON that the system can parse into graph nodes.
Always produce a complete, detailed plan before starting execution.`

	base := New(cfg)
	return &ManagerAgent{Agent: base}
}

// GoalPlan represents a complete plan generated from a user goal.
type GoalPlan struct {
	Goal      string        `json:"goal"`
	Summary   string        `json:"summary"`
	Modules   []ModuleDef   `json:"modules"`
	Schema    []TableDef    `json:"schema"`
	Endpoints []EndpointDef `json:"endpoints"`
	Tasks     []TaskDef     `json:"tasks"`
}

// ModuleDef defines a software module/package.
type ModuleDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Language    string `json:"language"`
}

// TableDef defines a database table.
type TableDef struct {
	Name    string      `json:"name"`
	Columns []ColumnDef `json:"columns"`
}

// ColumnDef defines a database column.
type ColumnDef struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Primary  bool   `json:"primary"`
}

// EndpointDef defines an API endpoint.
type EndpointDef struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
	Module      string `json:"module"`
}

// TaskDef defines a unit of work.
type TaskDef struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	AgentType   string   `json:"agent_type"` // backend, frontend, database, qa
	DependsOn   []string `json:"depends_on"` // task titles this depends on
	Priority    int      `json:"priority"`
}

// ============================================================
// Planning
// ============================================================

// PlanFromGoal generates a complete project plan from a natural language goal.
// It uses the LLM to design the architecture and task tree.
func (m *ManagerAgent) PlanFromGoal(ctx context.Context, goal string) (*GoalPlan, error) {
	defer logging.Trace("ManagerAgent.PlanFromGoal",
		map[string]interface{}{"goal": goal[:min(len(goal), 100)]})()

	logging.Info("Manager planning from goal", map[string]interface{}{"goal": goal})

	// Build planning prompt
	prompt := fmt.Sprintf(`You are a software architect. Given the following goal, produce a complete build plan in JSON format.

Goal: %s

Output ONLY valid JSON with this exact structure:
{
  "summary": "Brief summary of what will be built",
  "modules": [
    {"name": "module-name", "description": "what it does", "language": "php"}
  ],
  "schema": [
    {"name": "table_name", "columns": [
      {"name": "id", "type": "INT", "nullable": false, "primary": true},
      {"name": "name", "type": "VARCHAR(255)", "nullable": false, "primary": false}
    ]}
  ],
  "endpoints": [
    {"method": "GET", "path": "/api/resource", "description": "list resources", "module": "module-name"}
  ],
  "tasks": [
    {"title": "Create database schema", "description": "Create the SQL migration", "agent_type": "database", "depends_on": [], "priority": 1},
    {"title": "Implement API endpoint", "description": "Implement the endpoint logic", "agent_type": "backend", "depends_on": ["Create database schema"], "priority": 2}
  ]
}

Requirements:
- Each task must have a unique title
- DEPENDS_ON references other task titles to enforce ordering
- Agent types: architect, backend, frontend, database, qa, devops, documentation, security
- Priority 1 = highest, 5 = lowest
- Include ALL necessary modules, tables, endpoints, and tasks
- Keep tasks small and focused (one responsibility each)
- Include testing tasks where appropriate`, goal)

	// Call LLM
	resp, err := m.llm.Chat(ctx, []Message{
		{Role: "system", Content: m.systemPrompt},
		{Role: "user", Content: prompt},
	}, &ChatOptions{
		Model:       m.model,
		Temperature: 0.3, // Lower temperature for structured output
		MaxTokens:   m.maxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("planning LLM call failed: %w", err)
	}

	// Parse JSON from response
	plan, err := parseGoalPlan(resp.Content)
	if err != nil {
		logging.Error("Failed to parse LLM plan",
			map[string]interface{}{
				"error":    err.Error(),
				"response": resp.Content[:min(500, len(resp.Content))],
			})
		return nil, fmt.Errorf("parse plan: %w", err)
	}

	plan.Goal = goal

	logging.Info("Plan generated",
		map[string]interface{}{
			"modules":   len(plan.Modules),
			"tables":    len(plan.Schema),
			"endpoints": len(plan.Endpoints),
			"tasks":     len(plan.Tasks),
		})

	return plan, nil
}

// ExecutePlan writes the plan into the graph as nodes and edges.
func (m *ManagerAgent) ExecutePlan(engine *graph.GraphEngine, plan *GoalPlan) error {
	defer logging.Trace("ManagerAgent.ExecutePlan",
		map[string]interface{}{"goal": plan.Goal[:min(len(plan.Goal), 100)]})()

	logging.Info("Executing plan", map[string]interface{}{
		"goal": plan.Goal,
	})

	// 1. Create Decision node with the plan summary
	decisionNode, _ := engine.CreateNode(graph.NodeTypeDecision, map[string]interface{}{
		"statement": plan.Summary,
		"rationale": fmt.Sprintf("Generated from goal: %s", plan.Goal),
		"plan_json": toJSON(plan),
	})
	_ = decisionNode

	// 2. Create Module nodes
	moduleNodes := make(map[string]string) // name -> uuid
	for _, mod := range plan.Modules {
		node, err := engine.CreateNode(graph.NodeTypeModule, map[string]interface{}{
			"name":        mod.Name,
			"description": mod.Description,
			"language":    mod.Language,
			"type":        "module",
		})
		if err != nil {
			logging.Warn("Failed to create module node", map[string]interface{}{"name": mod.Name})
			continue
		}
		moduleNodes[mod.Name] = node.UUID
	}

	// 3. Create DatabaseSchema nodes
	tableNodes := make(map[string]string) // name -> uuid
	for _, table := range plan.Schema {
		columnsJSON, _ := json.Marshal(table.Columns)
		node, err := engine.CreateNode(graph.NodeTypeDatabaseSchema, map[string]interface{}{
			"table_name": table.Name,
			"columns":    string(columnsJSON),
		})
		if err != nil {
			logging.Warn("Failed to create schema node", map[string]interface{}{"table": table.Name})
			continue
		}
		tableNodes[table.Name] = node.UUID
	}

	// 4. Create Endpoint nodes
	for _, ep := range plan.Endpoints {
		node, err := engine.CreateNode(graph.NodeTypeEndpoint, map[string]interface{}{
			"http_method":  ep.Method,
			"path_pattern": ep.Path,
			"description":  ep.Description,
		})
		if err != nil {
			logging.Warn("Failed to create endpoint node", map[string]interface{}{"path": ep.Path})
			continue
		}

		// Link to module if it exists
		if moduleUUID, ok := moduleNodes[ep.Module]; ok {
			engine.CreateEdge(node.UUID, moduleUUID, graph.EdgeContains, nil)
		}
	}

	// 5. Create Task nodes with dependencies
	taskNodes := make(map[string]string) // title -> uuid
	for _, task := range plan.Tasks {
		node, err := engine.CreateNode(graph.NodeTypeTask, map[string]interface{}{
			"title":               task.Title,
			"description":         task.Description,
			"status":              "pending",
			"priority":            task.Priority,
			"assigned_agent_type": task.AgentType,
		})
		if err != nil {
			logging.Warn("Failed to create task node", map[string]interface{}{"title": task.Title})
			continue
		}
		taskNodes[task.Title] = node.UUID
	}

	// 6. Create DEPENDS_ON edges between tasks
	for _, task := range plan.Tasks {
		taskUUID, ok := taskNodes[task.Title]
		if !ok {
			continue
		}
		for _, depTitle := range task.DependsOn {
			depUUID, ok := taskNodes[depTitle]
			if !ok {
				logging.Warn("Dependency task not found", map[string]interface{}{
					"task": task.Title, "dep": depTitle,
				})
				continue
			}
			engine.CreateEdge(taskUUID, depUUID, graph.EdgeDependsOn, nil)
		}
	}

	logging.Info("Plan executed",
		map[string]interface{}{
			"modules":   len(moduleNodes),
			"tables":    len(tableNodes),
			"endpoints": len(plan.Endpoints),
			"tasks":     len(taskNodes),
		})

	return nil
}

// ============================================================
// Execution Supervision
// ============================================================

// SupervisionConfig controls how the manager handles task execution.
type SupervisionConfig struct {
	MaxRetries    int
	StaleLockTTL  time.Duration
	CheckInterval time.Duration
}

// DefaultSupervisionConfig returns sensible defaults.
func DefaultSupervisionConfig() SupervisionConfig {
	return SupervisionConfig{
		MaxRetries:    3,
		StaleLockTTL:  5 * time.Minute,
		CheckInterval: 30 * time.Second,
	}
}

// SuperviseTasks monitors task execution and handles failures.
// This would be called in a goroutine after ExecutePlan.
func (m *ManagerAgent) SuperviseTasks(ctx context.Context, engine *graph.GraphEngine, config SupervisionConfig) {
	defer logging.Trace("ManagerAgent.SuperviseTasks")()

	logging.Info("Task supervision started")

	ticker := time.NewTicker(config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logging.Info("Supervision stopped")
			return

		case <-ticker.C:
			m.checkTaskProgress(engine, config)
		}
	}
}

// checkTaskProgress scans for failed or stalled tasks and takes action.
func (m *ManagerAgent) checkTaskProgress(engine *graph.GraphEngine, config SupervisionConfig) {
	tasks := engine.FindNodesByType(graph.NodeTypeTask)

	for _, task := range tasks {
		status, _ := task.Properties["status"].(string)

		switch status {
		case "failed":
			logging.Warn("Task failed, handling",
				map[string]interface{}{
					"task": task.UUID,
					"name": task.Properties["title"],
				})
			m.handleFailedTask(engine, task, config)

		case "in_progress":
			// Check for stale locks
			if m.isTaskStale(task, config.StaleLockTTL) {
				logging.Warn("Task has stale lock, reassigning",
					map[string]interface{}{
						"task": task.UUID,
						"name": task.Properties["title"],
					})
				m.reassignTask(engine, task)
			}
		}
	}
}

// handleFailedTask retries or creates a fix task.
func (m *ManagerAgent) handleFailedTask(engine *graph.GraphEngine, task *graph.Node, config SupervisionConfig) {
	retries, _ := task.Properties["retry_count"].(float64)
	if int(retries) >= config.MaxRetries {
		logging.Error("Task exceeded max retries",
			map[string]interface{}{
				"task":    task.UUID,
				"retries": retries,
			})
		// TODO: Notify human operator
		return
	}

	// Increment retry count
	engine.UpdateNode(task.UUID, map[string]interface{}{
		"status":             "pending",
		"retry_count":        int(retries) + 1,
		"locked_by_agent_id": nil,
	})

	logging.Info("Task reset for retry",
		map[string]interface{}{
			"task":  task.UUID,
			"retry": int(retries) + 1,
		})
}

// isTaskStale checks if a task's lock has expired.
func (m *ManagerAgent) isTaskStale(task *graph.Node, ttl time.Duration) bool {
	lockedAt, ok := task.Properties["locked_at"].(string)
	if !ok {
		return false
	}

	t, err := time.Parse(time.RFC3339, lockedAt)
	if err != nil {
		return false
	}

	return time.Since(t) > ttl
}

// reassignTask clears the lock on a task so another agent can pick it up.
func (m *ManagerAgent) reassignTask(engine *graph.GraphEngine, task *graph.Node) {
	engine.UpdateNode(task.UUID, map[string]interface{}{
		"locked_by_agent_id": nil,
		"status":             "pending",
	})
}

// ============================================================
// Helpers
// ============================================================

// parseGoalPlan extracts a GoalPlan from LLM JSON output.
// The LLM may wrap JSON in markdown code blocks.
func parseGoalPlan(response string) (*GoalPlan, error) {
	// Strip markdown code blocks if present
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "```") {
		// Find the JSON content
		start := strings.Index(response, "\n")
		if start > 0 {
			response = response[start:]
		}
		end := strings.LastIndex(response, "```")
		if end > 0 {
			response = response[:end]
		}
		response = strings.TrimSpace(response)
	}

	// Also try to find JSON object if wrapped in other text
	if !strings.HasPrefix(response, "{") {
		braceStart := strings.Index(response, "{")
		if braceStart >= 0 {
			response = response[braceStart:]
		}
	}

	var plan GoalPlan
	if err := json.Unmarshal([]byte(response), &plan); err != nil {
		return nil, fmt.Errorf("json parse error: %w\nResponse: %s", err, response[:min(200, len(response))])
	}

	return &plan, nil
}

// toJSON marshals a value to JSON string safely.
func toJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data)
}
