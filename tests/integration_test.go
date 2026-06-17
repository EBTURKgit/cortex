// Package tests contains integration tests for the Cortex system.
// These tests verify end-to-end behavior: graph CRUD, persistence,
// indexing, MCP server, and the CLI interface.
package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EBTURKgit/cortex/internal/graph"
	"github.com/EBTURKgit/cortex/internal/mcp"
	"github.com/EBTURKgit/cortex/internal/server"

	"github.com/gorilla/websocket"
)

// ============================================================
// Graph Engine Integration Tests
// ============================================================

func TestGraphEngineFullCycle(t *testing.T) {
	g := graph.New()

	// Create a project
	_, err := g.CreateNode(graph.NodeTypeProject, map[string]interface{}{
		"name": "test-project",
	})
	if err != nil {
		t.Fatalf("CreateNode(project): %s", err)
	}

	// Create a file
	file, err := g.CreateNode(graph.NodeTypeFile, map[string]interface{}{
		"relative_path": "main.go",
		"language":      "go",
	})
	if err != nil {
		t.Fatalf("CreateNode(file): %s", err)
	}

	// Create a function
	fn, err := g.CreateNode(graph.NodeTypeFunction, map[string]interface{}{
		"name":      "main",
		"file_path": "main.go",
	})
	if err != nil {
		t.Fatalf("CreateNode(func): %s", err)
	}

	// Link them
	_, err = g.CreateEdge(file.UUID, fn.UUID, graph.EdgeContains, nil)
	if err != nil {
		t.Fatalf("CreateEdge: %s", err)
	}

	// Create a task
	task, err := g.CreateNode(graph.NodeTypeTask, map[string]interface{}{
		"title":       "Implement feature",
		"description": "Do the thing",
		"status":      "pending",
	})
	if err != nil {
		t.Fatalf("CreateNode(task): %s", err)
	}

	// Verify counts
	stats := g.Stats()
	if stats.NodeCount != 4 {
		t.Errorf("expected 4 nodes, got %d", stats.NodeCount)
	}
	if stats.EdgeCount != 1 {
		t.Errorf("expected 1 edge, got %d", stats.EdgeCount)
	}

	// Search by name
	nodes := g.FindNodeByName("main")
	if len(nodes) != 1 {
		t.Errorf("expected 1 match for 'main', got %d", len(nodes))
	}

	// Get edges
	edges := g.GetNodeEdges(file.UUID, "outgoing")
	if len(edges) != 1 {
		t.Errorf("expected 1 outgoing edge, got %d", len(edges))
	}

	// Update task
	g.UpdateNode(task.UUID, map[string]interface{}{
		"status": "completed",
	})
	updated, _ := g.GetNode(task.UUID)
	if updated.Properties["status"] != "completed" {
		t.Errorf("expected status 'completed', got '%s'", updated.Properties["status"])
	}

	// Lock the task
	lock, err := g.AcquireLock(task.UUID, "agent-1", 0)
	if err != nil {
		t.Fatalf("AcquireLock: %s", err)
	}
	if lock.AgentID != "agent-1" {
		t.Errorf("expected agent-1, got %s", lock.AgentID)
	}
	g.ReleaseLock(task.UUID, "agent-1")
}

func TestGraphPersistence(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "graph.json")

	g := graph.New()
	g.CreateNode(graph.NodeTypeFunction, map[string]interface{}{"name": "persistTest"})
	g.CreateNode(graph.NodeTypeClass, map[string]interface{}{"name": "PersistClass"})

	if err := g.SaveToFile(tmpFile); err != nil {
		t.Fatalf("SaveToFile: %s", err)
	}

	loaded, err := graph.LoadFromFile(tmpFile)
	if err != nil {
		t.Fatalf("LoadFromFile: %s", err)
	}

	if loaded.Stats().NodeCount != 2 {
		t.Errorf("expected 2 nodes after load, got %d", loaded.Stats().NodeCount)
	}

	nodes := loaded.FindNodeByName("persistTest")
	if len(nodes) != 1 {
		t.Errorf("expected 1 match for 'persistTest', got %d", len(nodes))
	}
}

func TestGraphSubscribeAndEvents(t *testing.T) {
	g := graph.New()

	ch := g.Subscribe("all")
	defer g.Unsubscribe("all", ch)

	node, err := g.CreateNode(graph.NodeTypeDecision, map[string]interface{}{
		"statement": "Use Go",
	})
	if err != nil {
		t.Fatalf("CreateNode: %s", err)
	}

	select {
	case event := <-ch:
		if event.Type != "create" {
			t.Errorf("expected 'create' event, got '%s'", event.Type)
		}
		if event.EntityID != node.UUID {
			t.Errorf("expected entity %s, got %s", node.UUID, event.EntityID)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestOptimisticLocking(t *testing.T) {
	g := graph.New()

	node, err := g.CreateNode(graph.NodeTypeTask, map[string]interface{}{
		"title":  "test",
		"status": "pending",
	})
	if err != nil {
		t.Fatalf("CreateNode: %s", err)
	}

	// First update succeeds
	_, err = g.UpdateNode(node.UUID, map[string]interface{}{
		"status":           "in_progress",
		"expected_version": 1,
	})
	if err != nil {
		t.Fatalf("first update: %s", err)
	}

	// Second update with stale version fails
	_, err = g.UpdateNode(node.UUID, map[string]interface{}{
		"status":           "completed",
		"expected_version": 1,
	})
	if err == nil {
		t.Fatal("expected version conflict error")
	}

	// Correct version succeeds
	_, err = g.UpdateNode(node.UUID, map[string]interface{}{
		"status":           "completed",
		"expected_version": 2,
	})
	if err != nil {
		t.Fatalf("update with correct version: %s", err)
	}
}

// ============================================================
// HTTP Server Integration Tests
// ============================================================

func setupTestServer(t *testing.T) (*server.Server, *graph.GraphEngine) {
	t.Helper()
	engine := graph.New()
	srv := server.New(engine, "127.0.0.1", 0) // port 0 = random available port

	// Add project and some nodes
	engine.CreateNode(graph.NodeTypeProject, map[string]interface{}{"name": "test"})
	engine.CreateNode(graph.NodeTypeFunction, map[string]interface{}{"name": "hello"})
	engine.CreateNode(graph.NodeTypeFunction, map[string]interface{}{"name": "world"})

	return srv, engine
}

func TestServerHealthEndpoint(t *testing.T) {
	srv, _ := setupTestServer(t)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%s'", body["status"])
	}
}

func TestServerStatsEndpoint(t *testing.T) {
	srv, _ := setupTestServer(t)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/stats")
	if err != nil {
		t.Fatalf("GET /stats: %s", err)
	}
	defer resp.Body.Close()
	var stats graph.EngineStats
	json.NewDecoder(resp.Body).Decode(&stats)

	if stats.NodeCount != 3 {
		t.Errorf("expected 3 nodes, got %d", stats.NodeCount)
	}
}

func TestServerCreateAndGetNode(t *testing.T) {
	srv, _ := setupTestServer(t)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Create node
	body := `{"type":"Class","properties":{"name":"UserService"}}`
	resp, err := http.Post(ts.URL+"/nodes", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /nodes: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}

	var node graph.Node
	json.NewDecoder(resp.Body).Decode(&node)
	if node.Properties["name"] != "UserService" {
		t.Errorf("expected name 'UserService', got '%s'", node.Properties["name"])
	}
}

func TestServerQueryEndpoint(t *testing.T) {
	srv, _ := setupTestServer(t)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/query?q=hello")
	if err != nil {
		t.Fatalf("GET /query: %s", err)
	}
	defer resp.Body.Close()

	var nodes []*graph.Node
	json.NewDecoder(resp.Body).Decode(&nodes)
	if len(nodes) == 0 {
		t.Error("expected at least 1 result for 'hello'")
	}
}

// ============================================================
// MCP Server Integration Tests
// ============================================================

func TestMCPServerInitialize(t *testing.T) {
	engine := graph.New()
	srv := mcp.New(engine)

	var buf bytes.Buffer
	srv.SetIO(
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+"\n"),
		&buf,
	)

	if err := srv.Run(); err != nil {
		t.Fatalf("MCP Run: %s", err)
	}

	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %s", err)
	}

	if resp.Result.ServerInfo.Name != "cortex" {
		t.Errorf("expected server name 'cortex', got '%s'", resp.Result.ServerInfo.Name)
	}
}

func TestMCPToolsList(t *testing.T) {
	engine := graph.New()
	srv := mcp.New(engine)

	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n"

	var buf bytes.Buffer
	srv.SetIO(strings.NewReader(input), &buf)
	srv.Run()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 2 {
		t.Fatal("expected at least 2 response lines")
	}

	var toolsResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &toolsResp); err != nil {
		t.Fatalf("parse tools response: %s", err)
	}

	toolNames := make(map[string]bool)
	for _, t := range toolsResp.Result.Tools {
		toolNames[t.Name] = true
	}

	expectedTools := []string{"graph_query", "context_for_file", "context_for_symbol",
		"search_code", "get_project_structure", "get_recent_errors"}
	for _, et := range expectedTools {
		if !toolNames[et] {
			t.Errorf("missing tool: %s", et)
		}
	}
}

func TestMCPGraphQueryStats(t *testing.T) {
	engine := graph.New()
	engine.CreateNode(graph.NodeTypeProject, map[string]interface{}{"name": "test"})
	engine.CreateNode(graph.NodeTypeFunction, map[string]interface{}{"name": "fn1"})
	engine.CreateNode(graph.NodeTypeFunction, map[string]interface{}{"name": "fn2"})

	srv := mcp.New(engine)

	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"graph_query","arguments":{"query":"stats"}}}` + "\n"

	var buf bytes.Buffer
	srv.SetIO(strings.NewReader(input), &buf)
	srv.Run()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 2 {
		t.Fatal("expected at least 2 response lines")
	}

	if !strings.Contains(lines[1], "node_count") {
		t.Errorf("expected stats in response, got: %s", lines[1])
	}
}

// ============================================================
// WebSocket Integration Tests
// ============================================================

func TestServerWebSocket(t *testing.T) {
	srv, engine := setupTestServer(t)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Start event bridge in background
	go srv.Start()
	time.Sleep(50 * time.Millisecond)

	// Connect WebSocket
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WebSocket dial: %s", err)
	}
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)

	// Create a node
	node, err := engine.CreateNode(graph.NodeTypeDecision, map[string]interface{}{
		"statement": "test decision",
	})
	if err != nil {
		t.Fatalf("CreateNode: %s", err)
	}

	// Read the event from WebSocket
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("WebSocket read: %s (node UUID: %s)", err, node.UUID)
	}

	var event struct {
		Type     string `json:"type"`
		EntityID string `json:"entity_id"`
	}
	if err := json.Unmarshal(msg, &event); err != nil {
		t.Fatalf("parse event: %s", err)
	}

	if event.EntityID != node.UUID {
		t.Errorf("expected entity %s, got %s", node.UUID, event.EntityID)
	}
}

// ============================================================
// Edge Cases
// ============================================================

func TestGraphMaxNodesLimit(t *testing.T) {
	_ = graph.MaxNodes
	// Verify the constant exists (compile-time check)
}

func TestEmptyGraphOperations(t *testing.T) {
	g := graph.New()

	_, err := g.GetNode("00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Error("expected error for non-existent node")
	}

	_, err = g.UpdateNode("00000000-0000-0000-0000-000000000000", map[string]interface{}{"x": 1})
	if err == nil {
		t.Error("expected error for non-existent node")
	}

	stats := g.Stats()
	if stats.NodeCount != 0 {
		t.Errorf("expected 0 nodes, got %d", stats.NodeCount)
	}
}

// ============================================================
// Benchmark
// ============================================================

func BenchmarkGraphCreateNodes(b *testing.B) {
	g := graph.New()
	for i := 0; i < b.N; i++ {
		g.CreateNode(graph.NodeTypeFunction, map[string]interface{}{
			"name": "func",
		})
	}
}

func BenchmarkGraphFindByName(b *testing.B) {
	g := graph.New()
	for i := 0; i < 10000; i++ {
		g.CreateNode(graph.NodeTypeFunction, map[string]interface{}{
			"name": "func",
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.FindNodeByName("func")
	}
}
