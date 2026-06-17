package graph

import (
	"path/filepath"
	"testing"
)

func TestCreateAndGetNode(t *testing.T) {
	g := New()

	node, err := g.CreateNode(NodeTypeFunction, map[string]interface{}{
		"name": "testFunc",
		"language": "go",
	})
	if err != nil {
		t.Fatalf("CreateNode failed: %s", err)
	}

	if node.Type != NodeTypeFunction {
		t.Errorf("expected type %s, got %s", NodeTypeFunction, node.Type)
	}

	// Get the node
	got, err := g.GetNode(node.UUID)
	if err != nil {
		t.Fatalf("GetNode failed: %s", err)
	}

	if got.UUID != node.UUID {
		t.Errorf("expected UUID %s, got %s", node.UUID, got.UUID)
	}

	name, _ := got.Properties["name"].(string)
	if name != "testFunc" {
		t.Errorf("expected name 'testFunc', got '%s'", name)
	}
}

func TestCreateNodeInvalidType(t *testing.T) {
	g := New()
	_, err := g.CreateNode("InvalidType", nil)
	if err == nil {
		t.Fatal("expected error for invalid node type")
	}
}

func TestUpdateNode(t *testing.T) {
	g := New()

	node, _ := g.CreateNode(NodeTypeClass, map[string]interface{}{
		"name": "UserService",
	})

	updated, err := g.UpdateNode(node.UUID, map[string]interface{}{
		"name":      "AdminService",
		"is_abstract": true,
	})
	if err != nil {
		t.Fatalf("UpdateNode failed: %s", err)
	}

	name, _ := updated.Properties["name"].(string)
	if name != "AdminService" {
		t.Errorf("expected name 'AdminService', got '%s'", name)
	}

	if updated.Version != 2 {
		t.Errorf("expected version 2, got %d", updated.Version)
	}
}

func TestDeleteNode(t *testing.T) {
	g := New()

	node, _ := g.CreateNode(NodeTypeFile, map[string]interface{}{
		"relative_path": "main.go",
	})

	if err := g.DeleteNode(node.UUID); err != nil {
		t.Fatalf("DeleteNode failed: %s", err)
	}

	_, err := g.GetNode(node.UUID)
	if err == nil {
		t.Fatal("expected error after deletion")
	}

	if g.stats.NodeCount != 0 {
		t.Errorf("expected 0 nodes, got %d", g.stats.NodeCount)
	}
}

func TestCreateEdge(t *testing.T) {
	g := New()

	func1, _ := g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "caller"})
	func2, _ := g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "callee"})

	edge, err := g.CreateEdge(func1.UUID, func2.UUID, EdgeCalls, nil)
	if err != nil {
		t.Fatalf("CreateEdge failed: %s", err)
	}

	if edge.Type != EdgeCalls {
		t.Errorf("expected edge type %s, got %s", EdgeCalls, edge.Type)
	}

	if edge.SourceUUID != func1.UUID || edge.TargetUUID != func2.UUID {
		t.Error("edge source/target mismatch")
	}
}

func TestCreateEdgeInvalidType(t *testing.T) {
	g := New()
	n1, _ := g.CreateNode(NodeTypeFunction, nil)
	n2, _ := g.CreateNode(NodeTypeFunction, nil)

	_, err := g.CreateEdge(n1.UUID, n2.UUID, "INVALID_EDGE", nil)
	if err == nil {
		t.Fatal("expected error for invalid edge type")
	}
}

func TestFindNodesByType(t *testing.T) {
	g := New()

	g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "f1"})
	g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "f2"})
	g.CreateNode(NodeTypeClass, map[string]interface{}{"name": "c1"})

	fns := g.FindNodesByType(NodeTypeFunction)
	if len(fns) != 2 {
		t.Errorf("expected 2 functions, got %d", len(fns))
	}

	cls := g.FindNodesByType(NodeTypeClass)
	if len(cls) != 1 {
		t.Errorf("expected 1 class, got %d", len(cls))
	}
}

func TestFindNodeByName(t *testing.T) {
	g := New()

	g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "CreateUser"})
	g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "DeleteUser"})

	nodes := g.FindNodeByName("create")
	if len(nodes) != 1 {
		t.Errorf("expected 1 match for 'create', got %d", len(nodes))
	}

	// Case insensitive
	nodes = g.FindNodeByName("USER")
	if len(nodes) != 2 {
		t.Errorf("expected 2 matches for 'USER', got %d", len(nodes))
	}
}

func TestGetNodeEdges(t *testing.T) {
	g := New()

	a, _ := g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "A"})
	b, _ := g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "B"})
	c, _ := g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "C"})

	g.CreateEdge(a.UUID, b.UUID, EdgeCalls, nil)
	g.CreateEdge(c.UUID, a.UUID, EdgeCalls, nil)

	// Outgoing from A
	out := g.GetNodeEdges(a.UUID, "outgoing")
	if len(out) != 1 {
		t.Errorf("expected 1 outgoing edge, got %d", len(out))
	}

	// Incoming to A
	in := g.GetNodeEdges(a.UUID, "incoming")
	if len(in) != 1 {
		t.Errorf("expected 1 incoming edge, got %d", len(in))
	}

	// Both directions
	all := g.GetNodeEdges(a.UUID, "")
	if len(all) != 2 {
		t.Errorf("expected 2 total edges, got %d", len(all))
	}
}

func TestGetCallersAndCallees(t *testing.T) {
	g := New()

	a, _ := g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "A"})
	b, _ := g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "B"})

	g.CreateEdge(a.UUID, b.UUID, EdgeCalls, nil)

	callers := g.GetCallers(b.UUID)
	if len(callers) != 1 {
		t.Errorf("expected 1 caller, got %d", len(callers))
	}

	callees := g.GetCallees(a.UUID)
	if len(callees) != 1 {
		t.Errorf("expected 1 callee, got %d", len(callees))
	}
}

func TestAcquireAndReleaseLock(t *testing.T) {
	g := New()

	node, _ := g.CreateNode(NodeTypeTask, map[string]interface{}{
		"title": "test-task",
	})

	lock, err := g.AcquireLock(node.UUID, "agent-1", 0)
	if err != nil {
		t.Fatalf("AcquireLock failed: %s", err)
	}

	if lock.AgentID != "agent-1" {
		t.Errorf("expected agent 'agent-1', got '%s'", lock.AgentID)
	}

	// Second acquire by different agent should fail
	_, err = g.AcquireLock(node.UUID, "agent-2", 0)
	if err == nil {
		t.Fatal("expected lock conflict error")
	}

	// Release
	if err := g.ReleaseLock(node.UUID, "agent-1"); err != nil {
		t.Fatalf("ReleaseLock failed: %s", err)
	}

	// Now agent-2 can acquire
	_, err = g.AcquireLock(node.UUID, "agent-2", 0)
	if err != nil {
		t.Fatalf("Re-acquire after release failed: %s", err)
	}
}

func TestSubscribeAndEmit(t *testing.T) {
	g := New()

	ch := g.Subscribe("all")

	node, _ := g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "test"})

	event := <-ch
	if event.Type != "create" {
		t.Errorf("expected 'create' event, got '%s'", event.Type)
	}
	if event.NodeType != NodeTypeFunction {
		t.Errorf("expected node type %s, got %s", NodeTypeFunction, event.NodeType)
	}
	if event.EntityID != node.UUID {
		t.Errorf("expected entity ID %s, got %s", node.UUID, event.EntityID)
	}

	g.Unsubscribe("all", ch)
}

func TestSaveAndLoadFromFile(t *testing.T) {
	g := New()

	g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "savedFunc"})
	g.CreateNode(NodeTypeClass, map[string]interface{}{"name": "savedClass"})

	tmpFile := filepath.Join(t.TempDir(), "graph.json")

	if err := g.SaveToFile(tmpFile); err != nil {
		t.Fatalf("SaveToFile failed: %s", err)
	}

	loaded, err := LoadFromFile(tmpFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %s", err)
	}

	if loaded.Stats().NodeCount != 2 {
		t.Errorf("expected 2 nodes after load, got %d", loaded.Stats().NodeCount)
	}

	fns := loaded.FindNodesByType(NodeTypeFunction)
	if len(fns) != 1 {
		t.Errorf("expected 1 function after load, got %d", len(fns))
	}

	nodes := loaded.FindNodeByName("savedFunc")
	if len(nodes) != 1 {
		t.Errorf("expected 1 match for 'savedFunc', got %d", len(nodes))
	}
}

func TestStats(t *testing.T) {
	g := New()

	g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "f1"})
	g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "f2"})
	g.CreateNode(NodeTypeClass, map[string]interface{}{"name": "c1"})

	stats := g.Stats()
	if stats.NodeCount != 3 {
		t.Errorf("expected 3 nodes, got %d", stats.NodeCount)
	}
	if stats.NodeTypes[NodeTypeFunction] != 2 {
		t.Errorf("expected 2 functions, got %d", stats.NodeTypes[NodeTypeFunction])
	}
	if stats.NodeTypes[NodeTypeClass] != 1 {
		t.Errorf("expected 1 class, got %d", stats.NodeTypes[NodeTypeClass])
	}
}

func TestDeleteNodeRemovesEdges(t *testing.T) {
	g := New()

	a, _ := g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "A"})
	b, _ := g.CreateNode(NodeTypeFunction, map[string]interface{}{"name": "B"})

	g.CreateEdge(a.UUID, b.UUID, EdgeCalls, nil)

	g.DeleteNode(a.UUID)

	if g.stats.EdgeCount != 0 {
		t.Errorf("expected 0 edges after deleting source node, got %d", g.stats.EdgeCount)
	}
}
