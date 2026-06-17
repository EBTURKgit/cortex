// Package progress provides a build checklist tracker ("memory layer").
// It records every step completed during the build process and can verify
// that prerequisites are met before moving to the next step.
//
// This is the "memory layer" that ensures we always know what has been done
// and what needs verification before proceeding.
package progress

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/EBTURKgit/cortex/internal/logging"
)

// StepStatus represents the current state of a build step.
type StepStatus string

const (
	StatusPending    StepStatus = "pending"
	StatusInProgress StepStatus = "in_progress"
	StatusCompleted  StepStatus = "completed"
	StatusFailed     StepStatus = "failed"
	StatusVerified   StepStatus = "verified" // completed + verified passing
)

// Step represents a single build step in the checklist.
type Step struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	Status       StepStatus `json:"status"`
	Dependencies []string   `json:"dependencies"` // step IDs that must be completed first
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	Error        string     `json:"error,omitempty"`
	Output       string     `json:"output,omitempty"`
}

// Checklist manages a sequence of build steps with dependency tracking.
type Checklist struct {
	mu       sync.Mutex
	steps    map[string]*Step
	ordered  []string // IDs in insertion order
	filePath string   // path to save progress
}

// NewChecklist creates a new progress checklist.
func NewChecklist(filePath string) *Checklist {
	logging.Debug("Creating progress checklist", map[string]interface{}{
		"path": filePath,
	})

	return &Checklist{
		steps:    make(map[string]*Step),
		ordered:  make([]string, 0),
		filePath: filePath,
	}
}

// LoadChecklist loads a saved checklist from disk.
func LoadChecklist(filePath string) (*Checklist, error) {
	logging.Debug("Loading checklist from disk", map[string]interface{}{"path": filePath})

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return NewChecklist(filePath), nil
		}
		return nil, fmt.Errorf("read checklist: %w", err)
	}

	var c Checklist
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse checklist: %w", err)
	}

	c.mu = sync.Mutex{}
	c.filePath = filePath

	logging.Info("Checklist loaded", map[string]interface{}{
		"steps":     len(c.steps),
		"completed": c.CountCompleted(),
	})

	return &c, nil
}

// AddStep registers a new step. If the step already exists, it's a no-op.
func (c *Checklist) AddStep(id, name, description string, dependencies ...string) *Step {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.steps[id]; exists {
		return c.steps[id]
	}

	step := &Step{
		ID:           id,
		Name:         name,
		Description:  description,
		Status:       StatusPending,
		Dependencies: dependencies,
	}

	c.steps[id] = step
	c.ordered = append(c.ordered, id)

	logging.Debug("Step added", map[string]interface{}{
		"id": id, "name": name, "deps": dependencies,
	})

	c.save()
	return step
}

// StartStep marks a step as in-progress after verifying dependencies.
func (c *Checklist) StartStep(id string) error {
	defer logging.Trace("Checklist.StartStep", map[string]interface{}{"step": id})()

	c.mu.Lock()
	defer c.mu.Unlock()

	step, ok := c.steps[id]
	if !ok {
		return fmt.Errorf("step not found: %s", id)
	}

	// Check dependencies
	for _, depID := range step.Dependencies {
		dep, ok := c.steps[depID]
		if !ok {
			return fmt.Errorf("dependency %s not found for step %s", depID, id)
		}
		if dep.Status != StatusCompleted && dep.Status != StatusVerified {
			return fmt.Errorf("dependency %s (%s) is not completed (status: %s)",
				depID, dep.Name, dep.Status)
		}
	}

	now := time.Now()
	step.Status = StatusInProgress
	step.StartedAt = &now

	logging.Info("Step started", map[string]interface{}{
		"step": id, "name": step.Name,
	})

	c.save()
	return nil
}

// CompleteStep marks a step as completed.
func (c *Checklist) CompleteStep(id string, output string) {
	defer logging.Trace("Checklist.CompleteStep", map[string]interface{}{"step": id})()

	c.mu.Lock()
	defer c.mu.Unlock()

	step, ok := c.steps[id]
	if !ok {
		logging.Warn("Step not found for completion", map[string]interface{}{"id": id})
		return
	}

	now := time.Now()
	step.Status = StatusCompleted
	step.CompletedAt = &now
	step.Output = output

	logging.Info("Step completed", map[string]interface{}{
		"step": id, "name": step.Name,
	})

	c.save()
}

// FailStep marks a step as failed with an error message.
func (c *Checklist) FailStep(id string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	step, ok := c.steps[id]
	if !ok {
		return
	}

	step.Status = StatusFailed
	step.Error = err.Error()

	logging.Error("Step failed", map[string]interface{}{
		"step": id, "name": step.Name, "error": err.Error(),
	})

	c.save()
}

// VerifyStep marks a step as verified (completed + tests pass).
func (c *Checklist) VerifyStep(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	step, ok := c.steps[id]
	if !ok {
		return
	}

	step.Status = StatusVerified

	logging.Info("Step verified", map[string]interface{}{
		"step": id, "name": step.Name,
	})

	c.save()
}

// GetStep returns a step by ID.
func (c *Checklist) GetStep(id string) *Step {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.steps[id]
}

// Status returns the status of a step.
func (c *Checklist) Status(id string) StepStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	step, ok := c.steps[id]
	if !ok {
		return StatusPending
	}
	return step.Status
}

// NextPending returns the first pending step whose dependencies are met.
func (c *Checklist) NextPending() *Step {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, id := range c.ordered {
		step := c.steps[id]
		if step.Status == StatusPending || step.Status == StatusFailed {
			// Check deps
			depsMet := true
			for _, depID := range step.Dependencies {
				dep, ok := c.steps[depID]
				if !ok || (dep.Status != StatusCompleted && dep.Status != StatusVerified) {
					depsMet = false
					break
				}
			}
			if depsMet {
				return step
			}
		}
	}
	return nil
}

// Summary returns a summary of the checklist progress.
func (c *Checklist) Summary() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()

	summary := map[string]int{
		"total":       len(c.steps),
		"pending":     0,
		"in_progress": 0,
		"completed":   0,
		"failed":      0,
		"verified":    0,
	}

	for _, step := range c.steps {
		switch step.Status {
		case StatusPending:
			summary["pending"]++
		case StatusInProgress:
			summary["in_progress"]++
		case StatusCompleted:
			summary["completed"]++
		case StatusFailed:
			summary["failed"]++
		case StatusVerified:
			summary["verified"]++
		}
	}

	return summary
}

// CountCompleted returns the number of completed+verified steps.
func (c *Checklist) CountCompleted() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for _, step := range c.steps {
		if step.Status == StatusCompleted || step.Status == StatusVerified {
			count++
		}
	}
	return count
}

// AllComplete returns true if all steps are completed or verified.
func (c *Checklist) AllComplete() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, step := range c.steps {
		if step.Status != StatusCompleted && step.Status != StatusVerified {
			return false
		}
	}
	return true
}

// List returns all steps in order.
func (c *Checklist) List() []*Step {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := make([]*Step, 0, len(c.ordered))
	for _, id := range c.ordered {
		result = append(result, c.steps[id])
	}
	return result
}

// Save persists the checklist to disk (public method).
func (c *Checklist) Save() {
	c.save()
}

// save persists the checklist to disk.
func (c *Checklist) save() {
	if c.filePath == "" {
		return
	}

	dir := filepath.Dir(c.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		logging.Warn("Failed to create progress dir", map[string]interface{}{"error": err.Error()})
		return
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		logging.Warn("Failed to marshal checklist", map[string]interface{}{"error": err.Error()})
		return
	}

	if err := os.WriteFile(c.filePath, data, 0644); err != nil {
		logging.Warn("Failed to save checklist", map[string]interface{}{"error": err.Error()})
	}
}

// MarshalJSON implements json.Marshaler to avoid mutex serialization.
func (c *Checklist) MarshalJSON() ([]byte, error) {
	type Alias struct {
		Steps   map[string]*Step `json:"steps"`
		Ordered []string         `json:"ordered"`
	}

	return json.Marshal(&Alias{
		Steps:   c.steps,
		Ordered: c.ordered,
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (c *Checklist) UnmarshalJSON(data []byte) error {
	type Alias struct {
		Steps   map[string]*Step `json:"steps"`
		Ordered []string         `json:"ordered"`
	}

	var alias Alias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}

	c.steps = alias.Steps
	c.ordered = alias.Ordered

	if c.steps == nil {
		c.steps = make(map[string]*Step)
	}

	return nil
}

// PrintSummary prints a progress summary to the console.
func PrintSummary(steps []*Step) {
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("  Cortex Build Progress")
	fmt.Println("========================================")

	completed := 0
	total := len(steps)

	for _, step := range steps {
		statusIcon := "⬜"
		switch step.Status {
		case StatusPending:
			statusIcon = "⬜"
		case StatusInProgress:
			statusIcon = "🔄"
		case StatusCompleted:
			statusIcon = "✅"
			completed++
		case StatusFailed:
			statusIcon = "❌"
		case StatusVerified:
			statusIcon = "✅"
			completed++
		}

		fmt.Printf("  %s [%s] %s\n", statusIcon, step.Status, step.Name)
		if step.Error != "" {
			fmt.Printf("       Error: %s\n", step.Error)
		}
	}

	fmt.Println()
	fmt.Printf("  Progress: %d/%d steps complete\n", completed, total)
	fmt.Println("========================================")
	fmt.Println()
}

// BuildSteps defines the full project build sequence.
// This is the master plan that the progress tracker uses.
func BuildSteps() []StepDef {
	return []StepDef{
		{ID: "01-project-structure", Name: "Project structure & build system", Description: "Create directories, go.mod, Makefile"},
		{ID: "02-logging", Name: "Logging system", Description: "Structured logging with debug tracing on every layer"},
		{ID: "03-config", Name: "Configuration system", Description: "cortex.yaml, user config, env overrides", Deps: []string{"02-logging"}},
		{ID: "04-graph-engine", Name: "In-memory graph engine", Description: "Nodes, edges, indexes, events, locking", Deps: []string{"03-config"}},
		{ID: "05-http-server", Name: "HTTP + WebSocket server", Description: "REST API, WS events, event bridge", Deps: []string{"04-graph-engine"}},
		{ID: "06-mcp-server", Name: "MCP server (AI tool integration)", Description: "MCP tools for opencode, Cursor, Claude Code", Deps: []string{"04-graph-engine"}},
		{ID: "07-indexer", Name: "Code indexer", Description: "Source code scanner with multi-language support", Deps: []string{"04-graph-engine"}},
		{ID: "08-cli-commands", Name: "CLI commands", Description: "init, serve, dev, config, import, query, mcp", Deps: []string{"02-logging", "03-config", "05-http-server", "06-mcp-server", "07-indexer"}},
		{ID: "09-main-entry", Name: "Main entrypoint & wiring", Description: "Wire everything together, parse CLI args", Deps: []string{"08-cli-commands"}},
		{ID: "10-build-verify", Name: "Build verification", Description: "Compile, run smoke test, verify all layers", Deps: []string{"09-main-entry"}},
	}
}

// StepDef defines a build step for the master plan.
type StepDef struct {
	ID          string
	Name        string
	Description string
	Deps        []string
}

// RegisterBuildSteps creates and returns a checklist with all build steps.
func RegisterBuildSteps(checklist *Checklist) {
	for _, def := range BuildSteps() {
		checklist.AddStep(def.ID, def.Name, def.Description, def.Deps...)
	}
}
