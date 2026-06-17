// Package graph provides an in-memory labeled property graph engine
// with indexing, change-data-capture events, and advisory locking.
// This is the core memory layer of Cortex.
package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/EBTURKgit/cortex/internal/logging"

	"github.com/google/uuid"
)

// uuidPattern validates standard UUID format.
var uuidPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

// ============================================================
// Node & Edge Type Constants
// ============================================================

const (
	NodeTypeProject        = "Project"
	NodeTypeFile           = "File"
	NodeTypeModule         = "Module"
	NodeTypeFunction       = "Function"
	NodeTypeClass          = "Class"
	NodeTypeMethod         = "Method"
	NodeTypeVariable       = "Variable"
	NodeTypeInterface      = "Interface"
	NodeTypeEndpoint       = "Endpoint"
	NodeTypeDatabaseSchema = "DatabaseSchema"
	NodeTypeLogEntry       = "LogEntry"
	NodeTypeTraceSpan      = "TraceSpan"
	NodeTypeTask           = "Task"
	NodeTypeDecision       = "Decision"
	NodeTypeCommit         = "Commit"
	NodeTypeAgent          = "Agent"
)

const (
	EdgeContains        = "CONTAINS"
	EdgeImports         = "IMPORTS"
	EdgeCalls           = "CALLS"
	EdgeInherits        = "INHERITS"
	EdgeImplements      = "IMPLEMENTS"
	EdgeReferences      = "REFERENCES"
	EdgeDefinesEndpoint = "DEFINES_ENDPOINT"
	EdgeMapsToTable     = "MAPS_TO_TABLE"
	EdgeTriggeredBy     = "TRIGGERED_BY"
	EdgeBelongsToTrace  = "BELONGS_TO_TRACE"
	EdgeAssignedTo      = "ASSIGNED_TO"
	EdgeDependsOn       = "DEPENDS_ON"
	EdgeMotivates       = "MOTIVATES"
	EdgeChangedIn       = "CHANGED_IN"
	EdgeProposesChange  = "PROPOSES_CHANGE"
	EdgeHasError        = "HAS_ERROR"
	EdgeResolvedBy      = "RESOLVED_BY"
)

// ============================================================
// Core Types
// ============================================================

// Node represents a single entity in the knowledge graph.
type Node struct {
	UUID       string                 `json:"uuid"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
	Version    int                    `json:"version"`
}

// Edge represents a directed relationship between two nodes.
type Edge struct {
	SourceUUID string                 `json:"source_uuid"`
	TargetUUID string                 `json:"target_uuid"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	CreatedAt  time.Time              `json:"created_at"`
}

// ChangeEvent is emitted by the graph engine on every mutation.
type ChangeEvent struct {
	Type       string      `json:"type"`       // "create", "update", "delete"
	NodeType   string      `json:"node_type"`   // node type (empty for edge events)
	EdgeType   string      `json:"edge_type"`   // edge type (empty for node events)
	EntityID   string      `json:"entity_id"`  // node UUID or "source:type:target" for edges
	Properties interface{} `json:"properties,omitempty"`
}

// Lock represents an advisory lock on a node (used for task assignment).
type Lock struct {
	NodeUUID   string    `json:"node_uuid"`
	AgentID    string    `json:"agent_id"`
	LockedAt   time.Time `json:"locked_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// ============================================================
// GraphEngine — The Core Memory Store
// ============================================================

// MaxNodes limits the total number of nodes in the in-memory graph to prevent OOM.
const MaxNodes = 1_000_000

// GraphEngine is a thread-safe in-memory property graph with indexing and events.
type GraphEngine struct {
	mu      sync.RWMutex
	nodes   map[string]*Node
	edges   []*Edge

	// Indexes
	typeIndex   map[string]map[string]*Node  // nodeType -> {uuid: node}
	nameIndex   map[string][]*Node           // property "name" -> nodes
	edgeIndex   map[string][]*Edge           // edgeType -> []edges
	sourceIndex map[string][]*Edge           // sourceUUID -> []edges
	targetIndex map[string][]*Edge           // targetUUID -> []edges

	// Event subscribers
	subscribers map[string][]chan ChangeEvent
	subMu       sync.RWMutex

	// Locks
	locks map[string]*Lock

	// Node type map for validation
	validNodeTypes map[string]bool
	validEdgeTypes map[string]bool

	// Stats
	stats EngineStats
}

// EngineStats holds basic metrics about the graph.
type EngineStats struct {
	NodeCount   int            `json:"node_count"`
	EdgeCount   int            `json:"edge_count"`
	NodeTypes   map[string]int `json:"node_types"`
	EdgeTypes   map[string]int `json:"edge_types"`
	Subscribers int            `json:"subscribers"`
	LockCount   int            `json:"lock_count"`
}

// New creates a new empty GraphEngine with all indexes initialized.
func New() *GraphEngine {
	logging.Debug("Creating new graph engine")

	return &GraphEngine{
		nodes:          make(map[string]*Node),
		edges:          make([]*Edge, 0),
		typeIndex:      make(map[string]map[string]*Node),
		nameIndex:      make(map[string][]*Node),
		edgeIndex:      make(map[string][]*Edge),
		sourceIndex:    make(map[string][]*Edge),
		targetIndex:    make(map[string][]*Edge),
		subscribers:    make(map[string][]chan ChangeEvent),
		locks:          make(map[string]*Lock),
		validNodeTypes: validNodeTypes(),
		validEdgeTypes: validEdgeTypes(),
		stats: EngineStats{
			NodeTypes: make(map[string]int),
			EdgeTypes: make(map[string]int),
		},
	}
}

func validNodeTypes() map[string]bool {
	return map[string]bool{
		NodeTypeProject: true, NodeTypeFile: true, NodeTypeModule: true,
		NodeTypeFunction: true, NodeTypeClass: true, NodeTypeMethod: true,
		NodeTypeVariable: true, NodeTypeInterface: true, NodeTypeEndpoint: true,
		NodeTypeDatabaseSchema: true, NodeTypeLogEntry: true, NodeTypeTraceSpan: true,
		NodeTypeTask: true, NodeTypeDecision: true, NodeTypeCommit: true, NodeTypeAgent: true,
	}
}

func validEdgeTypes() map[string]bool {
	return map[string]bool{
		EdgeContains: true, EdgeImports: true, EdgeCalls: true,
		EdgeInherits: true, EdgeImplements: true, EdgeReferences: true,
		EdgeDefinesEndpoint: true, EdgeMapsToTable: true, EdgeTriggeredBy: true,
		EdgeBelongsToTrace: true, EdgeAssignedTo: true, EdgeDependsOn: true,
		EdgeMotivates: true, EdgeChangedIn: true, EdgeProposesChange: true,
		EdgeHasError: true, EdgeResolvedBy: true,
	}
}

// ============================================================
// Node CRUD
// ============================================================

// CreateNode adds a new node of the given type with the given properties.
// It auto-generates a UUID and sets timestamps.
func (g *GraphEngine) CreateNode(nodeType string, props map[string]interface{}) (*Node, error) {
	defer logging.Trace("GraphEngine.CreateNode", map[string]interface{}{"type": nodeType})()

	if !g.validNodeTypes[nodeType] {
		return nil, fmt.Errorf("invalid node type: %s", nodeType)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.nodes) >= MaxNodes {
		return nil, fmt.Errorf("node limit reached (%d): delete nodes before creating more", MaxNodes)
	}

	now := time.Now().UTC()
	node := &Node{
		UUID:       uuid.New().String(),
		Type:       nodeType,
		Properties: copyProps(props),
		CreatedAt:  now,
		UpdatedAt:  now,
		Version:    1,
	}

	g.nodes[node.UUID] = node

	// Update indexes
	if g.typeIndex[nodeType] == nil {
		g.typeIndex[nodeType] = make(map[string]*Node)
	}
	g.typeIndex[nodeType][node.UUID] = node
	if name, ok := getStringProp(props, "name"); ok {
		g.nameIndex[name] = append(g.nameIndex[name], node)
	}

	// Update stats
	g.stats.NodeCount++
	g.stats.NodeTypes[nodeType]++

	logging.Debug("Node created", map[string]interface{}{
		"uuid": node.UUID, "type": nodeType,
	})

	// Emit event
	g.emitEvent(ChangeEvent{
		Type:       "create",
		NodeType:   nodeType,
		EntityID:   node.UUID,
		Properties: props,
	})

	return node, nil
}

// GetNode retrieves a node by UUID.
func (g *GraphEngine) GetNode(uuid string) (*Node, error) {
	if err := validateUUID(uuid); err != nil {
		return nil, err
	}
	g.mu.RLock()
	defer g.mu.RUnlock()

	node, ok := g.nodes[uuid]
	if !ok {
		return nil, fmt.Errorf("node not found: %s", uuid)
	}
	return node, nil
}

// UpdateNode updates properties on an existing node.
// Returns the updated node. Empty props are ignored.
// Optimistic locking: if props contains "expected_version", the update
// only succeeds if the node's current version matches.
func (g *GraphEngine) UpdateNode(uuid string, props map[string]interface{}) (*Node, error) {
	defer logging.Trace("GraphEngine.UpdateNode", map[string]interface{}{"uuid": uuid})()
	if err := validateUUID(uuid); err != nil {
		return nil, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	node, ok := g.nodes[uuid]
	if !ok {
		return nil, fmt.Errorf("node not found: %s", uuid)
	}

	if len(props) == 0 {
		return node, nil
	}

	// Optimistic locking: check expected_version if provided
	if expVer, ok := props["expected_version"]; ok {
		expected := -1
		switch v := expVer.(type) {
		case float64:
			expected = int(v)
		case int:
			expected = v
		case int64:
			expected = int(v)
		}
		if expected > 0 && expected != node.Version {
			return nil, fmt.Errorf("version conflict: expected %d, current %d",
				expected, node.Version)
		}
		delete(props, "expected_version")
	}

	// Remove old name from index
	if oldName, ok := getStringProp(node.Properties, "name"); ok {
		g.removeFromNameIndex(oldName, uuid)
	}

	// Apply updates
	for k, v := range props {
		node.Properties[k] = v
	}
	node.UpdatedAt = time.Now().UTC()
	node.Version++

	// Add new name to index
	if newName, ok := getStringProp(node.Properties, "name"); ok {
		g.nameIndex[newName] = append(g.nameIndex[newName], node)
	}

	logging.Debug("Node updated", map[string]interface{}{
		"uuid": uuid, "version": node.Version,
	})

	g.emitEvent(ChangeEvent{
		Type:       "update",
		NodeType:   node.Type,
		EntityID:   node.UUID,
		Properties: props,
	})

	return node, nil
}

// DeleteNode removes a node and all its edges from the graph.
func (g *GraphEngine) DeleteNode(uuid string) error {
	defer logging.Trace("GraphEngine.DeleteNode", map[string]interface{}{"uuid": uuid})()
	if err := validateUUID(uuid); err != nil {
		return err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	node, ok := g.nodes[uuid]
	if !ok {
		return fmt.Errorf("node not found: %s", uuid)
	}

	nodeType := node.Type

	// Remove from type index
	delete(g.typeIndex[nodeType], uuid)

	// Remove from name index
	if name, ok := getStringProp(node.Properties, "name"); ok {
		g.removeFromNameIndex(name, uuid)
	}

	// Remove all edges connected to this node
	g.removeEdgesForNode(uuid)

	// Remove the node
	delete(g.nodes, uuid)

	// Update stats
	g.stats.NodeCount--
	g.stats.NodeTypes[nodeType]--
	if g.stats.NodeTypes[nodeType] <= 0 {
		delete(g.stats.NodeTypes, nodeType)
	}

	logging.Debug("Node deleted", map[string]interface{}{"uuid": uuid})

	g.emitEvent(ChangeEvent{
		Type:     "delete",
		NodeType: nodeType,
		EntityID: uuid,
	})

	return nil
}

// ============================================================
// Edge CRUD
// ============================================================

// CreateEdge creates a directed edge from source to target with the given type.
func (g *GraphEngine) CreateEdge(sourceUUID, targetUUID, edgeType string, props map[string]interface{}) (*Edge, error) {
	defer logging.Trace("GraphEngine.CreateEdge",
		map[string]interface{}{"source": sourceUUID, "target": targetUUID, "type": edgeType})()

	if !g.validEdgeTypes[edgeType] {
		return nil, fmt.Errorf("invalid edge type: %s", edgeType)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Verify both nodes exist
	if _, ok := g.nodes[sourceUUID]; !ok {
		return nil, fmt.Errorf("source node not found: %s", sourceUUID)
	}
	if _, ok := g.nodes[targetUUID]; !ok {
		return nil, fmt.Errorf("target node not found: %s", targetUUID)
	}

	edge := &Edge{
		SourceUUID: sourceUUID,
		TargetUUID: targetUUID,
		Type:       edgeType,
		Properties: copyProps(props),
		CreatedAt:  time.Now().UTC(),
	}

	g.edges = append(g.edges, edge)

	// Update indexes
	g.edgeIndex[edgeType] = append(g.edgeIndex[edgeType], edge)
	g.sourceIndex[sourceUUID] = append(g.sourceIndex[sourceUUID], edge)
	g.targetIndex[targetUUID] = append(g.targetIndex[targetUUID], edge)

	// Update stats
	g.stats.EdgeCount++
	g.stats.EdgeTypes[edgeType]++

	logging.Debug("Edge created", map[string]interface{}{
		"source": sourceUUID, "target": targetUUID, "type": edgeType,
	})

	g.emitEvent(ChangeEvent{
		Type:     "create",
		EdgeType: edgeType,
		EntityID: fmt.Sprintf("%s:%s:%s", sourceUUID, edgeType, targetUUID),
	})

	return edge, nil
}

// GetNodeEdges returns all edges connected to a node, optionally filtered by direction and type.
func (g *GraphEngine) GetNodeEdges(nodeUUID string, direction string, edgeTypes ...string) []*Edge {
	defer logging.Trace("GraphEngine.GetNodeEdges",
		map[string]interface{}{"node": nodeUUID, "direction": direction})()

	g.mu.RLock()
	defer g.mu.RUnlock()

	typeFilter := make(map[string]bool)
	for _, et := range edgeTypes {
		typeFilter[et] = true
	}

	var result []*Edge

	switch direction {
	case "outgoing", "out", "":
		for _, e := range g.sourceIndex[nodeUUID] {
			if len(typeFilter) == 0 || typeFilter[e.Type] {
				result = append(result, e)
			}
		}
		if direction == "outgoing" || direction == "out" {
			break
		}
		fallthrough
	case "incoming", "in":
		for _, e := range g.targetIndex[nodeUUID] {
			if len(typeFilter) == 0 || typeFilter[e.Type] {
				result = append(result, e)
			}
		}
	}

	return result
}

// GetEdgesByType returns all edges of the given type.
func (g *GraphEngine) GetEdgesByType(edgeType string) []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.edgeIndex[edgeType]
}

// ============================================================
// Query Helpers
// ============================================================

// FindNodesByType returns all nodes of a given type.
func (g *GraphEngine) FindNodesByType(nodeType string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := g.typeIndex[nodeType]
	result := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		result = append(result, n)
	}
	return result
}

// FindNodeByName returns nodes whose "name" property matches (case-insensitive, prefix match).
func (g *GraphEngine) FindNodeByName(name string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nameLower := strings.ToLower(name)
	var result []*Node
	for key, nodes := range g.nameIndex {
		if strings.Contains(strings.ToLower(key), nameLower) {
			result = append(result, nodes...)
		}
	}
	return result
}

// GetCallers returns edges where the given function UUID is the target (i.e., incoming CALLS edges).
func (g *GraphEngine) GetCallers(funcUUID string) []*Edge {
	return g.GetNodeEdges(funcUUID, "incoming", EdgeCalls)
}

// GetCallees returns edges where the given function UUID is the source (i.e., outgoing CALLS edges).
func (g *GraphEngine) GetCallees(funcUUID string) []*Edge {
	return g.GetNodeEdges(funcUUID, "outgoing", EdgeCalls)
}

// ============================================================
// Events / Change-Data-Capture
// ============================================================

// Subscribe returns a channel that receives change events for the given topic.
// Topic format: "project/{id}/node/{type}", "project/{id}/edge/{type}", or "all".
func (g *GraphEngine) Subscribe(topic string) chan ChangeEvent {
	g.subMu.Lock()
	defer g.subMu.Unlock()

	ch := make(chan ChangeEvent, 100)
	g.subscribers[topic] = append(g.subscribers[topic], ch)

	g.stats.Subscribers = len(g.subscribers)

	logging.Debug("New subscriber", map[string]interface{}{"topic": topic})
	return ch
}

// Unsubscribe removes a subscriber channel.
func (g *GraphEngine) Unsubscribe(topic string, ch chan ChangeEvent) {
	g.subMu.Lock()
	defer g.subMu.Unlock()

	subs := g.subscribers[topic]
	for i, s := range subs {
		if s == ch {
			g.subscribers[topic] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}

	g.stats.Subscribers = len(g.subscribers)
	logging.Debug("Subscriber removed", map[string]interface{}{"topic": topic})
}

// emitEvent sends a change event to all matching subscribers.
func (g *GraphEngine) emitEvent(event ChangeEvent) {
	g.subMu.RLock()
	defer g.subMu.RUnlock()

	// Topics to match
	topics := []string{"all"}
	if event.NodeType != "" {
		topics = append(topics, "project/*/node/"+event.NodeType)
		topics = append(topics, "project/*/node/*")
	}
	if event.EdgeType != "" {
		topics = append(topics, "project/*/edge/"+event.EdgeType)
		topics = append(topics, "project/*/edge/*")
	}

	for _, topic := range topics {
		for _, ch := range g.subscribers[topic] {
			select {
			case ch <- event:
			default:
				logging.Warn("Subscriber channel full, dropping event",
					map[string]interface{}{"topic": topic})
			}
		}
	}
}

// ============================================================
// Advisory Locking
// ============================================================

// AcquireLock attempts to acquire a lock on a node for an agent.
func (g *GraphEngine) AcquireLock(nodeUUID, agentID string, ttl time.Duration) (*Lock, error) {
	defer logging.Trace("GraphEngine.AcquireLock",
		map[string]interface{}{"node": nodeUUID, "agent": agentID})()

	g.mu.Lock()
	defer g.mu.Unlock()

	if _, ok := g.nodes[nodeUUID]; !ok {
		return nil, fmt.Errorf("node not found: %s", nodeUUID)
	}

	// Check existing lock
	if existing, ok := g.locks[nodeUUID]; ok {
		if time.Now().UTC().Before(existing.ExpiresAt) && existing.AgentID != agentID {
			return nil, fmt.Errorf("node %s is locked by agent %s until %s",
				nodeUUID, existing.AgentID, existing.ExpiresAt.Format(time.RFC3339))
		}
		// Lock expired or same agent — allow re-acquire
		logging.Debug("Lock expired or re-acquired",
			map[string]interface{}{"node": nodeUUID, "old_agent": existing.AgentID})
	}

	now := time.Now().UTC()
	expiresAt := now.Add(ttl)
	if ttl == 0 {
		expiresAt = now.Add(365 * 24 * time.Hour) // No expiry = 1 year
	}
	lock := &Lock{
		NodeUUID:  nodeUUID,
		AgentID:   agentID,
		LockedAt:  now,
		ExpiresAt: expiresAt,
	}
	g.locks[nodeUUID] = lock
	g.stats.LockCount = len(g.locks)

	logging.Debug("Lock acquired", map[string]interface{}{
		"node": nodeUUID, "agent": agentID, "ttl": ttl.String(),
	})

	return lock, nil
}

// ReleaseLock releases a lock held by an agent.
func (g *GraphEngine) ReleaseLock(nodeUUID, agentID string) error {
	defer logging.Trace("GraphEngine.ReleaseLock",
		map[string]interface{}{"node": nodeUUID, "agent": agentID})()

	g.mu.Lock()
	defer g.mu.Unlock()

	lock, ok := g.locks[nodeUUID]
	if !ok {
		return nil // no lock to release
	}
	if lock.AgentID != agentID {
		return fmt.Errorf("lock on %s is held by agent %s, not %s",
			nodeUUID, lock.AgentID, agentID)
	}

	delete(g.locks, nodeUUID)
	g.stats.LockCount = len(g.locks)

	logging.Debug("Lock released", map[string]interface{}{"node": nodeUUID, "agent": agentID})
	return nil
}

// ============================================================
// Persistence (JSON Save/Load)
// ============================================================

// graphData is the serializable structure for saving/loading the graph.
type graphData struct {
	Nodes  map[string]*Node `json:"nodes"`
	Edges  []*Edge          `json:"edges"`
	Locks  map[string]*Lock `json:"locks"`
}

// SaveToFile serializes the graph to a JSON file.
func (g *GraphEngine) SaveToFile(path string) error {
	defer logging.Trace("GraphEngine.SaveToFile", map[string]interface{}{"path": path})()

	g.mu.RLock()
	defer g.mu.RUnlock()

	data := graphData{
		Nodes: g.nodes,
		Edges: g.edges,
		Locks: g.locks,
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal graph: %w", err)
	}

	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		return fmt.Errorf("write graph file: %w", err)
	}

	logging.Info("Graph saved to disk", map[string]interface{}{
		"path":  path,
		"nodes": len(g.nodes),
		"edges": len(g.edges),
	})

	return nil
}

// LoadFromFile loads graph state from a JSON file and returns a new engine.
func LoadFromFile(path string) (*GraphEngine, error) {
	logging.Debug("Loading graph from file", map[string]interface{}{"path": path})

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return nil, fmt.Errorf("read graph file: %w", err)
	}

	var gd graphData
	if err := json.Unmarshal(data, &gd); err != nil {
		return nil, fmt.Errorf("parse graph file: %w", err)
	}

	engine := New()
	engine.nodes = gd.Nodes
	engine.edges = gd.Edges
	engine.locks = gd.Locks

	// Rebuild indexes
	for uuid, node := range engine.nodes {
		nt := node.Type
		if engine.typeIndex[nt] == nil {
			engine.typeIndex[nt] = make(map[string]*Node)
		}
		engine.typeIndex[nt][uuid] = node
		if name, ok := getStringProp(node.Properties, "name"); ok {
			engine.nameIndex[name] = append(engine.nameIndex[name], node)
		}
	}

	for _, edge := range engine.edges {
		engine.edgeIndex[edge.Type] = append(engine.edgeIndex[edge.Type], edge)
		engine.sourceIndex[edge.SourceUUID] = append(engine.sourceIndex[edge.SourceUUID], edge)
		engine.targetIndex[edge.TargetUUID] = append(engine.targetIndex[edge.TargetUUID], edge)
	}

	// Rebuild stats
	engine.stats.NodeCount = len(engine.nodes)
	engine.stats.EdgeCount = len(engine.edges)
	for _, n := range engine.nodes {
		engine.stats.NodeTypes[n.Type]++
	}
	for _, e := range engine.edges {
		engine.stats.EdgeTypes[e.Type]++
	}
	engine.stats.LockCount = len(engine.locks)

	logging.Info("Graph loaded from disk", map[string]interface{}{
		"path":  path,
		"nodes": engine.stats.NodeCount,
		"edges": engine.stats.EdgeCount,
	})

	return engine, nil
}

// ============================================================
// Stats & Introspection
// ============================================================

// Stats returns current graph statistics.
func (g *GraphEngine) Stats() EngineStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Return a copy to avoid race conditions
	stats := g.stats
	stats.NodeTypes = make(map[string]int)
	for k, v := range g.stats.NodeTypes {
		stats.NodeTypes[k] = v
	}
	stats.EdgeTypes = make(map[string]int)
	for k, v := range g.stats.EdgeTypes {
		stats.EdgeTypes[k] = v
	}
	return stats
}

// ============================================================
// Internal Helpers
// ============================================================

func validateUUID(u string) error {
	if !uuidPattern.MatchString(u) {
		return fmt.Errorf("invalid UUID format: %s", u)
	}
	return nil
}

func copyProps(props map[string]interface{}) map[string]interface{} {
	if props == nil {
		return make(map[string]interface{})
	}
	cp := make(map[string]interface{}, len(props))
	for k, v := range props {
		cp[k] = v
	}
	return cp
}

func getStringProp(props map[string]interface{}, key string) (string, bool) {
	if props == nil {
		return "", false
	}
	v, ok := props[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func (g *GraphEngine) removeFromNameIndex(name, uuid string) {
	nodes := g.nameIndex[name]
	for i, n := range nodes {
		if n.UUID == uuid {
			g.nameIndex[name] = append(nodes[:i], nodes[i+1:]...)
			if len(g.nameIndex[name]) == 0 {
				delete(g.nameIndex, name)
			}
			break
		}
	}
}

func (g *GraphEngine) removeEdgesForNode(uuid string) {
	var keep []*Edge
	for _, e := range g.edges {
		if e.SourceUUID == uuid || e.TargetUUID == uuid {
			g.stats.EdgeCount--
			g.stats.EdgeTypes[e.Type]--
			if g.stats.EdgeTypes[e.Type] <= 0 {
				delete(g.stats.EdgeTypes, e.Type)
			}
			// Remove from edge index
			g.removeFromEdgeIndex(e)
			// Remove from source/target indexes
			g.removeFromSourceTargetIndex(e)
		} else {
			keep = append(keep, e)
		}
	}
	g.edges = keep
}

func (g *GraphEngine) removeFromEdgeIndex(e *Edge) {
	edges := g.edgeIndex[e.Type]
	for i, edge := range edges {
		if edge == e {
			g.edgeIndex[e.Type] = append(edges[:i], edges[i+1:]...)
			if len(g.edgeIndex[e.Type]) == 0 {
				delete(g.edgeIndex, e.Type)
			}
			break
		}
	}
}

func (g *GraphEngine) removeFromSourceTargetIndex(e *Edge) {
	// Remove from source index
	srcEdges := g.sourceIndex[e.SourceUUID]
	for i, edge := range srcEdges {
		if edge == e {
			g.sourceIndex[e.SourceUUID] = append(srcEdges[:i], srcEdges[i+1:]...)
			if len(g.sourceIndex[e.SourceUUID]) == 0 {
				delete(g.sourceIndex, e.SourceUUID)
			}
			break
		}
	}

	// Remove from target index
	tgtEdges := g.targetIndex[e.TargetUUID]
	for i, edge := range tgtEdges {
		if edge == e {
			g.targetIndex[e.TargetUUID] = append(tgtEdges[:i], tgtEdges[i+1:]...)
			if len(g.targetIndex[e.TargetUUID]) == 0 {
				delete(g.targetIndex, e.TargetUUID)
			}
			break
		}
	}
}
