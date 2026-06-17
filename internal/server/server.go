// Package server provides the HTTP REST API and WebSocket real-time event layer
// for the Cortex graph engine. It also embeds the MCP server for AI tool integration.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	_ "embed"
	"sync"
	"time"

	"github.com/EBTURKgit/cortex/internal/graph"
	"github.com/EBTURKgit/cortex/internal/ingestion"
	"github.com/EBTURKgit/cortex/internal/logging"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
)

// DefaultPort is the default port for the Cortex graph server.
const DefaultPort = 8741

// Server wraps the graph engine with HTTP + WebSocket interfaces.
type Server struct {
	engine  *graph.GraphEngine
	router  *chi.Mux
	httpSrv *http.Server
	wsUpgr  *websocket.Upgrader
	port    int

	// WebSocket connections for broadcasting
	wsConns   map[*websocket.Conn]bool
	wsMu      sync.RWMutex
	wsSubs    map[*websocket.Conn][]string // conn -> subscribed topics

	// Event bridge: graph events -> WebSocket
	eventCh chan graph.ChangeEvent
	stopCh  chan struct{}

	// Ingestion service for runtime log/trace ingestion
	ingestionSvc *ingestion.Service
}

// New creates a new server wrapping the given graph engine.
func New(engine *graph.GraphEngine, port int) *Server {
	logging.Debug("Creating server", map[string]interface{}{"port": port})

	s := &Server{
		engine:        engine,
		port:          port,
		wsConns:       make(map[*websocket.Conn]bool),
		wsSubs:        make(map[*websocket.Conn][]string),
		eventCh:       make(chan graph.ChangeEvent, 1000),
		stopCh:        make(chan struct{}),
		ingestionSvc: ingestion.NewService(engine),
	}

	s.wsUpgr = &websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	// Set WebSocket read limit (10MB max message)
	s.wsUpgr.EnableCompression = false

	s.router = chi.NewRouter()
	s.setupRoutes()

	return s
}

// setupRoutes configures all HTTP routes.
func (s *Server) setupRoutes() {
	// Middleware
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.Timeout(60 * time.Second))
	s.router.Use(bodySizeLimitMiddleware)
	s.router.Use(corsMiddleware)

	// Health & Info
	s.router.Get("/health", s.handleHealth)
	s.router.Get("/stats", s.handleStats)

	// Node CRUD
	s.router.Post("/nodes", s.handleCreateNode)
	s.router.Get("/nodes/{uuid}", s.handleGetNode)
	s.router.Put("/nodes/{uuid}", s.handleUpdateNode)
	s.router.Delete("/nodes/{uuid}", s.handleDeleteNode)
	s.router.Get("/nodes", s.handleFindNodes)

	// Edge CRUD
	s.router.Post("/edges", s.handleCreateEdge)
	s.router.Get("/edges", s.handleGetEdges)
	s.router.Get("/nodes/{uuid}/edges", s.handleGetNodeEdges)

	// Query
	s.router.Get("/query", s.handleQuery)

	// WebSocket
	s.router.Get("/ws", s.handleWebSocket)

	// Runtime ingestion
	s.router.Post("/ingest/log", s.ingestionSvc.HandleLogIngestion)
	s.router.Post("/ingest/trace", s.ingestionSvc.HandleTraceIngestion)

	// Dashboard & Graph visualization
	s.router.Get("/", s.handleDashboard)
	s.router.Get("/graph", s.handleGraphVisualization)
	s.router.Get("/graph-data", s.handleGraphData)
	s.router.Get("/tasks-data", s.handleTasksData)
	s.router.Get("/events-data", s.handleEventsData)
}

// Start begins listening for HTTP connections and bridges graph events to WebSocket.
func (s *Server) Start() error {
	logging.Trace("Server.Start")()

	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	s.httpSrv = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	// Start event bridge
	go s.eventBridge()

	logging.Info("Server starting", map[string]interface{}{
		"address": addr,
	})

	err := s.httpSrv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	logging.Trace("Server.Shutdown")()

	close(s.stopCh)

	// Close all WebSocket connections
	s.wsMu.Lock()
	for conn := range s.wsConns {
		conn.Close()
	}
	s.wsMu.Unlock()

	return s.httpSrv.Shutdown(ctx)
}

// Port returns the port the server is listening on.
func (s *Server) Port() int {
	return s.port
}

// Router returns the HTTP router for testing purposes.
func (s *Server) Router() *chi.Mux {
	return s.router
}

// ============================================================
// Event Bridge: graph changes -> WebSocket clients
// ============================================================

func (s *Server) eventBridge() {
	logging.Debug("Event bridge started")

	// Subscribe to all graph events
	sub := s.engine.Subscribe("all")
	defer s.engine.Unsubscribe("all", sub)

	for {
		select {
		case event := <-sub:
			s.broadcastToSubscribers(event)
		case <-s.stopCh:
			logging.Debug("Event bridge stopped")
			return
		}
	}
}

func (s *Server) broadcastToSubscribers(event graph.ChangeEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		logging.Error("Failed to marshal event", map[string]interface{}{"error": err.Error()})
		return
	}

	s.wsMu.RLock()
	var removeQueue []*websocket.Conn
	for conn, topics := range s.wsSubs {
		if matchesTopic(topics, event) {
			err := conn.WriteMessage(websocket.TextMessage, data)
			if err != nil {
				logging.Warn("WebSocket write error",
					map[string]interface{}{"error": err.Error()})
				removeQueue = append(removeQueue, conn)
			}
		}
	}
	s.wsMu.RUnlock()

	// Remove failed connections outside the lock to prevent deadlock
	for _, conn := range removeQueue {
		s.removeConn(conn)
	}
}

func matchesTopic(topics []string, event graph.ChangeEvent) bool {
	for _, t := range topics {
		if t == "all" {
			return true
		}
		if event.NodeType != "" && t == "node" {
			return true
		}
		if event.EdgeType != "" && t == "edge" {
			return true
		}
	}
	return false
}

// ============================================================
// REST Handlers
// ============================================================

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"version": "0.1.0",
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.Stats())
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	defer logging.Trace("handleCreateNode")()
	logging.Debug("POST /nodes")

	var req struct {
		Type       string                 `json:"type"`
		Properties map[string]interface{} `json:"properties"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	node, err := s.engine.CreateNode(req.Type, req.Properties)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, node)
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	defer logging.Trace("handleGetNode")()
	uuid := chi.URLParam(r, "uuid")
	logging.Debug("GET /nodes/"+uuid)

	node, err := s.engine.GetNode(uuid)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, node)
}

func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	defer logging.Trace("handleUpdateNode")()
	uuid := chi.URLParam(r, "uuid")
	logging.Debug("PUT /nodes/"+uuid)

	var props map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&props); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	node, err := s.engine.UpdateNode(uuid, props)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, node)
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	defer logging.Trace("handleDeleteNode")()
	uuid := chi.URLParam(r, "uuid")
	logging.Debug("DELETE /nodes/"+uuid)

	if err := s.engine.DeleteNode(uuid); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFindNodes(w http.ResponseWriter, r *http.Request) {
	defer logging.Trace("handleFindNodes")()
	nodeType := r.URL.Query().Get("type")
	name := r.URL.Query().Get("name")

	logging.Debug("GET /nodes", map[string]interface{}{
		"type": nodeType, "name": name,
	})

	var nodes []*graph.Node

	if name != "" {
		nodes = s.engine.FindNodeByName(name)
	} else if nodeType != "" {
		nodes = s.engine.FindNodesByType(nodeType)
	} else {
		writeError(w, http.StatusBadRequest, "provide 'type' or 'name' query parameter")
		return
	}

	if nodes == nil {
		nodes = []*graph.Node{}
	}

	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) handleCreateEdge(w http.ResponseWriter, r *http.Request) {
	defer logging.Trace("handleCreateEdge")()
	logging.Debug("POST /edges")

	var req struct {
		SourceUUID string                 `json:"source_uuid"`
		TargetUUID string                 `json:"target_uuid"`
		Type       string                 `json:"type"`
		Properties map[string]interface{} `json:"properties"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	edge, err := s.engine.CreateEdge(req.SourceUUID, req.TargetUUID, req.Type, req.Properties)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, edge)
}

func (s *Server) handleGetEdges(w http.ResponseWriter, r *http.Request) {
	defer logging.Trace("handleGetEdges")()
	edgeType := r.URL.Query().Get("type")

	if edgeType != "" {
		edges := s.engine.GetEdgesByType(edgeType)
		writeJSON(w, http.StatusOK, edges)
		return
	}

	writeError(w, http.StatusBadRequest, "provide 'type' query parameter")
}

func (s *Server) handleGetNodeEdges(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	direction := r.URL.Query().Get("direction")
	edgeType := r.URL.Query().Get("type")

	logging.Debug("GET /nodes/"+uuid+"/edges",
		map[string]interface{}{"direction": direction, "type": edgeType})

	var edgeTypes []string
	if edgeType != "" {
		edgeTypes = []string{edgeType}
	}

	edges := s.engine.GetNodeEdges(uuid, direction, edgeTypes...)
	if edges == nil {
		edges = []*graph.Edge{}
	}

	writeJSON(w, http.StatusOK, edges)
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	logging.Debug("GET /query", map[string]interface{}{"q": q})

	if q == "" {
		writeError(w, http.StatusBadRequest, "provide 'q' query parameter")
		return
	}

	// Simple query routing
	switch {
	case q == "stats":
		writeJSON(w, http.StatusOK, s.engine.Stats())
	default:
		// Try name search
		nodes := s.engine.FindNodeByName(q)
		if len(nodes) > 0 {
			writeJSON(w, http.StatusOK, nodes)
			return
		}
		// Try type search
		nodes = s.engine.FindNodesByType(q)
		writeJSON(w, http.StatusOK, nodes)
	}
}

// ============================================================
// Graph Visualization
// ============================================================

type visNode struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Type   string `json:"type"`
	Group  int    `json:"group"`
}

type visEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Label  string `json:"label"`
}

// handleGraphData returns nodes and edges as JSON for the visualizer.
func (s *Server) handleGraphData(w http.ResponseWriter, r *http.Request) {
	stats := s.engine.Stats()

	// Assign a group number per node type for coloring
	typeColors := make(map[string]int)
	i := 0
	for t := range stats.NodeTypes {
		typeColors[t] = i
		i++
	}

	// Collect all nodes by iterating each type
	allNodes := make([]*graph.Node, 0)
	nodeTypes := make([]string, 0)
	for t := range stats.NodeTypes {
		nodeTypes = append(nodeTypes, t)
	}
	for _, nt := range nodeTypes {
		allNodes = append(allNodes, s.engine.FindNodesByType(nt)...)
	}

	nodes := make([]visNode, 0, len(allNodes))
	for _, n := range allNodes {
		label := ""
		if v, ok := n.Properties["name"]; ok {
			label = fmt.Sprintf("%v", v)
		} else if v, ok := n.Properties["relative_path"]; ok {
			label = fmt.Sprintf("%v", v)
		} else if v, ok := n.Properties["title"]; ok {
			label = fmt.Sprintf("%v", v)
		} else {
			label = n.Type
		}

		nodes = append(nodes, visNode{
			ID:    n.UUID,
			Label: label,
			Type:  n.Type,
			Group: typeColors[n.Type],
		})
	}

	edges := make([]visEdge, 0)
	for _, nt := range nodeTypes {
		for _, n := range s.engine.FindNodesByType(nt) {
			connEdges := s.engine.GetNodeEdges(n.UUID, "outgoing")
			for _, e := range connEdges {
				if len(edges) < 500 { // limit edges for visual clarity
					edges = append(edges, visEdge{
						From:  e.SourceUUID,
						To:    e.TargetUUID,
						Label: e.Type,
					})
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"nodes": nodes,
		"edges": edges,
	})
}

// handleGraphVisualization serves an interactive HTML graph explorer.
func (s *Server) handleGraphVisualization(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>Cortex Graph Explorer</title>
<style>
  body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, sans-serif; background: #1a1a2e; color: #eee; }
  #header { padding: 12px 20px; background: #16213e; border-bottom: 1px solid #0f3460; display: flex; align-items: center; gap: 16px; }
  #header h1 { margin: 0; font-size: 18px; color: #e94560; }
  #header span { color: #888; font-size: 13px; }
  #legend { padding: 8px 20px; background: #16213e; display: flex; gap: 16px; flex-wrap: wrap; font-size: 12px; border-bottom: 1px solid #0f3460; }
  .legend-item { display: flex; align-items: center; gap: 4px; }
  .legend-dot { width: 10px; height: 10px; border-radius: 50%; display: inline-block; }
  #graph { width: 100vw; height: calc(100vh - 80px); }
  #tooltip { position: absolute; background: #16213e; border: 1px solid #0f3460; border-radius: 6px; padding: 8px 12px; font-size: 12px; pointer-events: none; display: none; z-index: 100; }
</style>
</head>
<body>
<div id="header">
  <h1>🧠 Cortex</h1>
  <span>Graph Explorer</span>
</div>
<div id="legend"></div>
<div id="graph"></div>
<div id="tooltip"></div>

<script src="https://d3js.org/d3.v7.min.js" integrity="sha512-qV6kE5WU4uwZLEtoYhFv7rD+zRo0kMGNBp3cB2FN8K5z3lz7YyLQMBXf2IeCq2DB1I4Q6MFg77+l74BbtIDqLg==" crossorigin="anonymous"></script>
<script>
const colors = d3.scaleOrdinal(d3.schemeSet2);
const typeColors = {};

fetch('/graph-data')
  .then(r => r.json())
  .then(data => {
    const nodes = data.nodes.map(n => ({ ...n }));
    const edges = data.edges.map(e => ({ ...e }));
    
    // Build legend
    const types = [...new Set(nodes.map(n => n.type))];
    types.forEach((t, i) => { typeColors[t] = i; });
    const legendDiv = document.getElementById('legend');
    types.forEach(t => {
      const item = document.createElement('div');
      item.className = 'legend-item';
      item.innerHTML = '<span class="legend-dot" style="background:' + colors(typeColors[t]) + '"></span> ' + t;
      legendDiv.appendChild(item);
    });
    legendDiv.innerHTML += '<span style="color:#666"> | ' + nodes.length + ' nodes, ' + edges.length + ' edges</span>';

    const width = window.innerWidth;
    const height = window.innerHeight - 80;

    const svg = d3.select('#graph').append('svg')
      .attr('width', width).attr('height', height);

    const g = svg.append('g');

    const zoom = d3.zoom().scaleExtent([0.1, 8]).on('zoom', (e) => {
      g.attr('transform', e.transform);
    });
    svg.call(zoom);

    const simulation = d3.forceSimulation(nodes)
      .force('link', d3.forceLink(edges).id(d => d.id).distance(80))
      .force('charge', d3.forceManyBody().strength(-200))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide().radius(20));

    const link = g.append('g').selectAll('line')
      .data(edges).join('line')
      .attr('stroke', '#444').attr('stroke-width', 1).attr('stroke-opacity', 0.4);

    const node = g.append('g').selectAll('circle')
      .data(nodes).join('circle')
      .attr('r', 6)
      .attr('fill', d => colors(typeColors[d.type] || 0))
      .attr('stroke', '#fff').attr('stroke-width', 0.5)
      .call(d3.drag()
        .on('start', (e, d) => { if (!e.active) simulation.alphaTarget(0.3).restart(); d.fx = d.x; d.fy = d.y; })
        .on('drag', (e, d) => { d.fx = e.x; d.fy = e.y; })
        .on('end', (e, d) => { if (!e.active) simulation.alphaTarget(0); d.fx = null; d.fy = null; })
      );

    const label = g.append('g').selectAll('text')
      .data(nodes).join('text')
      .text(d => d.label.length > 20 ? d.label.slice(0, 17) + '...' : d.label)
      .attr('font-size', '9px').attr('dx', 8).attr('dy', 3)
      .attr('fill', '#ccc')
      .style('pointer-events', 'none');

    const tooltip = document.getElementById('tooltip');
    node.on('mouseenter', (e, d) => {
      tooltip.style.display = 'block';
      tooltip.innerHTML = '<b>' + d.label + '</b><br>Type: ' + d.type + '<br>ID: ' + d.id.slice(0, 12) + '...';
    }).on('mousemove', (e) => {
      tooltip.style.left = (e.pageX + 12) + 'px';
      tooltip.style.top = (e.pageY - 10) + 'px';
    }).on('mouseleave', () => { tooltip.style.display = 'none'; });

    simulation.on('tick', () => {
      link.attr('x1', d => d.source.x).attr('y1', d => d.source.y)
          .attr('x2', d => d.target.x).attr('y2', d => d.target.y);
      node.attr('cx', d => d.x).attr('cy', d => d.y);
      label.attr('x', d => d.x).attr('y', d => d.y);
    });
  });
</script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, html); err != nil {
		logging.Warn("Failed to write graph page", map[string]interface{}{"error": err.Error()})
	}
}

// ============================================================
// Dashboard
// ============================================================

// handleTasksData returns all tasks as JSON for the dashboard.
func (s *Server) handleTasksData(w http.ResponseWriter, r *http.Request) {
	tasks := s.engine.FindNodesByType(graph.NodeTypeTask)
	type taskView struct {
		UUID        string `json:"uuid"`
		Title       string `json:"title"`
		Status      string `json:"status"`
		AgentType   string `json:"agent_type"`
		Priority    int    `json:"priority"`
		Description string `json:"description,omitempty"`
	}
	views := make([]taskView, 0, len(tasks))
	for _, t := range tasks {
		title, _ := t.Properties["title"].(string)
		status, _ := t.Properties["status"].(string)
		agentType, _ := t.Properties["assigned_agent_type"].(string)
		priority, _ := t.Properties["priority"].(float64)
		desc, _ := t.Properties["description"].(string)
		views = append(views, taskView{
			UUID: t.UUID, Title: title, Status: status,
			AgentType: agentType, Priority: int(priority),
			Description: truncate(desc, 100),
		})
	}
	writeJSON(w, http.StatusOK, views)
}

// handleEventsData returns recent events from the graph.
func (s *Server) handleEventsData(w http.ResponseWriter, r *http.Request) {
	// Return recent decisions and completed tasks as an activity feed
	decisions := s.engine.FindNodesByType(graph.NodeTypeDecision)
	type eventView struct {
		Time    string `json:"time"`
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	events := make([]eventView, 0, len(decisions)+10)
	for _, d := range decisions {
		stmt, _ := d.Properties["statement"].(string)
		events = append(events, eventView{
			Time: d.CreatedAt.Format(time.RFC3339),
			Type: "decision",
			Message: truncate(stmt, 150),
		})
	}
	tasks := s.engine.FindNodesByType(graph.NodeTypeTask)
	for _, t := range tasks {
		status, _ := t.Properties["status"].(string)
		if status == "completed" || status == "failed" {
			title, _ := t.Properties["title"].(string)
			events = append(events, eventView{
				Time:    t.UpdatedAt.Format(time.RFC3339),
				Type:    "task_" + status,
				Message: fmt.Sprintf("Task '%s' %s", title, status),
			})
		}
	}
	// Reverse chronological
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	if len(events) > 50 {
		events = events[:50]
	}
	writeJSON(w, http.StatusOK, events)
}

//go:embed static/dashboard.html
var dashboardHTML string

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, dashboardHTML)
}

// ============================================================
// WebSocket Handler
// ============================================================

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	logging.Debug("WebSocket connection request")

	conn, err := s.wsUpgr.Upgrade(w, r, nil)
	if err != nil {
		logging.Error("WebSocket upgrade failed", map[string]interface{}{"error": err.Error()})
		return
	}
	conn.SetReadLimit(10 * 1024 * 1024) // 10MB max message

	s.wsMu.Lock()
	s.wsConns[conn] = true
	s.wsSubs[conn] = []string{"all"} // subscribe to all by default
	count := len(s.wsConns)
	s.wsMu.Unlock()

	logging.Info("WebSocket client connected", map[string]interface{}{
		"remote":        conn.RemoteAddr().String(),
		"total_clients": count,
	})

	// Read messages (for subscription management)
	go s.handleWSMessages(conn)
}

func (s *Server) handleWSMessages(conn *websocket.Conn) {
	defer func() {
		s.removeConn(conn)
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				logging.Debug("WebSocket closed", map[string]interface{}{
					"error": err.Error(),
				})
			}
			return
		}

		// Parse subscription messages
		var msg struct {
			Type   string   `json:"type"`
			Topics []string `json:"topics"`
		}
		if err := json.Unmarshal(message, &msg); err != nil {
			logging.Debug("Invalid WS message", map[string]interface{}{"message": string(message)})
			continue
		}

		if msg.Type == "subscribe" {
			s.wsMu.Lock()
			s.wsSubs[conn] = msg.Topics
			s.wsMu.Unlock()
			logging.Debug("WS subscription updated", map[string]interface{}{
				"topics": msg.Topics,
			})
		}
	}
}

func (s *Server) removeConn(conn *websocket.Conn) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()

	delete(s.wsConns, conn)
	delete(s.wsSubs, conn)
	conn.Close()
}

// ============================================================
// Helpers
// ============================================================

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logging.Error("JSON encode error", map[string]interface{}{"error": err.Error()})
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func bodySizeLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024) // 10MB limit
		}
		next.ServeHTTP(w, r)
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://127.0.0.1:8741")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
