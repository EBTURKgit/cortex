// Package mcp implements the Model Context Protocol (MCP) server.
// MCP allows AI coding tools (opencode, Cursor, Claude Code, etc.)
// to query the Cortex graph for project context.
//
// Protocol: JSON-RPC 2.0 over stdin/stdout (stdio transport)
// or over SSE (Server-Sent Events).
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/EBTURKgit/cortex/internal/graph"
	"github.com/EBTURKgit/cortex/internal/logging"
)

// ============================================================
// JSON-RPC Types
// ============================================================

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type MCPTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]PropertySchema `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

type PropertySchema struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

// ============================================================
// MCP Server
// ============================================================

// Server implements the Model Context Protocol for AI tool integration.
type Server struct {
	engine  *graph.GraphEngine
	reader  *bufio.Scanner
	writer  io.Writer
	logger  *logging.Logger

	// Server info
	serverInfo map[string]interface{}
}

// New creates a new MCP server that reads from the given reader and writes to the given writer.
// For stdio transport, use os.Stdin and os.Stdout.
func New(engine *graph.GraphEngine) *Server {
	logging.Debug("Creating MCP server")

	return &Server{
		engine: engine,
		reader: bufio.NewScanner(os.Stdin),
		writer: os.Stdout,
		logger: logging.DefaultLogger.WithFields(map[string]interface{}{
			"service": "mcp",
		}),
		serverInfo: map[string]interface{}{
			"name":    "cortex",
			"version": "0.1.0",
		},
	}
}

// SetIO changes the input/output readers/writers (useful for testing).
func (s *Server) SetIO(r io.Reader, w io.Writer) {
	s.reader = bufio.NewScanner(r)
	s.writer = w
}

// Run starts the MCP server's main loop, processing JSON-RPC messages.
func (s *Server) Run() error {
	defer logging.Trace("MCP.Run")()

	s.logger.Info("MCP server started (stdio transport)")

	// Use a larger buffer for scanning — must be set before first Scan()
	s.reader.Buffer(make([]byte, 1024*1024), 1024*1024)
	s.reader.Split(bufio.ScanLines)

	for s.reader.Scan() {
		line := s.reader.Text()
		if line == "" {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.logger.Debug("Invalid JSON-RPC request",
				map[string]interface{}{"error": err.Error()})
			continue
		}

		s.logger.Debug("MCP request", map[string]interface{}{
			"method": req.Method,
			"id":     req.ID,
		})

		s.handleRequest(req)
	}

	if err := s.reader.Err(); err != nil {
		return fmt.Errorf("MCP read error: %w", err)
	}

	s.logger.Info("MCP server shutting down")
	return nil
}

// handleRequest routes a JSON-RPC request to the appropriate handler.
func (s *Server) handleRequest(req JSONRPCRequest) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(req)
	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method), nil)
	}
}

// ============================================================
// Handler: initialize
// ============================================================

func (s *Server) handleInitialize(req JSONRPCRequest) {
	s.sendResult(req.ID, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"serverInfo":      s.serverInfo,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
	})
}

// ============================================================
// Handler: tools/list
// ============================================================

func (s *Server) handleToolsList(req JSONRPCRequest) {
	tools := []MCPTool{
		{
			Name:        "graph_query",
			Description: "Run a natural-language or type-based query against the project knowledge graph",
			InputSchema: InputSchema{
				Type:     "object",
				Required: []string{"query"},
				Properties: map[string]PropertySchema{
					"query": {
						Type:        "string",
						Description: "Query string: node type name, symbol name, or 'stats'",
					},
				},
			},
		},
		{
			Name:        "context_for_file",
			Description: "Get all functions, classes, and dependencies for a file",
			InputSchema: InputSchema{
				Type:     "object",
				Required: []string{"file_path"},
				Properties: map[string]PropertySchema{
					"file_path": {
						Type:        "string",
						Description: "Relative path of the file",
					},
				},
			},
		},
		{
			Name:        "context_for_symbol",
			Description: "Get definition, references, callers, and callees for a function or class",
			InputSchema: InputSchema{
				Type:     "object",
				Required: []string{"symbol_name"},
				Properties: map[string]PropertySchema{
					"symbol_name": {
						Type:        "string",
						Description: "Name of the function, class, or method",
					},
				},
			},
		},
		{
			Name:        "search_code",
			Description: "Full-text search across all indexed code entities (functions, classes, files)",
			InputSchema: InputSchema{
				Type:     "object",
				Required: []string{"query"},
				Properties: map[string]PropertySchema{
					"query": {
						Type:        "string",
						Description: "Search term",
					},
				},
			},
		},
		{
			Name:        "get_project_structure",
			Description: "Get the high-level module and file tree of the project",
			InputSchema: InputSchema{
				Type:       "object",
				Required:   []string{},
				Properties: map[string]PropertySchema{},
			},
		},
		{
			Name:        "get_recent_errors",
			Description: "Get recent ERROR-level log entries linked to functions",
			InputSchema: InputSchema{
				Type:     "object",
				Required: []string{},
				Properties: map[string]PropertySchema{
					"limit": {
						Type:        "number",
						Description: "Max results to return (default 10)",
					},
				},
			},
		},
	}

	s.sendResult(req.ID, map[string]interface{}{
		"tools": tools,
	})
}

// ============================================================
// Handler: tools/call
// ============================================================

func (s *Server) handleToolsCall(req JSONRPCRequest) {
	var params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	s.logger.Debug("Tool call", map[string]interface{}{
		"tool":    params.Name,
		"args":    params.Arguments,
	})

	var result interface{}
	var err error

	switch params.Name {
	case "graph_query":
		result, err = s.handleGraphQuery(params.Arguments)
	case "context_for_file":
		result, err = s.handleContextForFile(params.Arguments)
	case "context_for_symbol":
		result, err = s.handleContextForSymbol(params.Arguments)
	case "search_code":
		result, err = s.handleSearchCode(params.Arguments)
	case "get_project_structure":
		result, err = s.handleProjectStructure(params.Arguments)
	case "get_recent_errors":
		result, err = s.handleRecentErrors(params.Arguments)
	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("Tool not found: %s", params.Name), nil)
		return
	}

	if err != nil {
		s.sendError(req.ID, -32000, err.Error(), nil)
		return
	}

	// Wrap the result in MCP-compliant content format
	contentJSON, _ := json.Marshal(result)
	s.sendResult(req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": string(contentJSON),
			},
		},
	})
}

// ============================================================
// Tool Implementations
// ============================================================

func (s *Server) handleGraphQuery(args map[string]interface{}) (interface{}, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query parameter required")
	}

	query = strings.ToLower(strings.TrimSpace(query))

	// Check for stats
	if query == "stats" {
		return s.engine.Stats(), nil
	}

	// Try as node type
	nodes := s.engine.FindNodesByType(query)
	if len(nodes) > 0 {
		return map[string]interface{}{
			"type":  "nodes",
			"count": len(nodes),
			"nodes": summarizeNodes(nodes, 20),
		}, nil
	}

	// Try as name search
	nodes = s.engine.FindNodeByName(query)
	if len(nodes) > 0 {
		return map[string]interface{}{
			"type":  "nodes",
			"count": len(nodes),
			"nodes": summarizeNodes(nodes, 20),
		}, nil
	}

	return map[string]interface{}{
		"type":    "empty",
		"message": "No results found",
		"nodes":   []interface{}{},
	}, nil
}

func (s *Server) handleContextForFile(args map[string]interface{}) (interface{}, error) {
	filePath, _ := args["file_path"].(string)
	if filePath == "" {
		return nil, fmt.Errorf("file_path parameter required")
	}

	// Find the file node
	nodes := s.engine.FindNodeByName(filePath)
	var fileNode *graph.Node
	for _, n := range nodes {
		if n.Type == graph.NodeTypeFile {
			fileNode = n
			break
		}
	}

	if fileNode == nil {
		// Try searching with just the filename
		parts := strings.Split(filePath, "/")
		filename := parts[len(parts)-1]
		nodes = s.engine.FindNodeByName(filename)
		for _, n := range nodes {
			if n.Type == graph.NodeTypeFile {
				fileNode = n
				break
			}
		}
	}

	if fileNode == nil {
		return map[string]interface{}{
			"error":   "not_found",
			"message": fmt.Sprintf("File not indexed: %s", filePath),
		}, nil
	}

	// Get contained nodes (functions, classes)
	contained := s.engine.GetNodeEdges(fileNode.UUID, "outgoing", graph.EdgeContains)

	var functions []map[string]interface{}
	var classes []map[string]interface{}

	for _, edge := range contained {
		target, err := s.engine.GetNode(edge.TargetUUID)
		if err != nil {
			continue
		}
		summary := nodeSummary(target)
		switch target.Type {
		case graph.NodeTypeFunction, graph.NodeTypeMethod:
			functions = append(functions, summary)
		case graph.NodeTypeClass:
			classes = append(classes, summary)
		}
	}

	return map[string]interface{}{
		"file":      nodeSummary(fileNode),
		"functions": functions,
		"classes":   classes,
	}, nil
}

func (s *Server) handleContextForSymbol(args map[string]interface{}) (interface{}, error) {
	symbolName, _ := args["symbol_name"].(string)
	if symbolName == "" {
		return nil, fmt.Errorf("symbol_name parameter required")
	}

	nodes := s.engine.FindNodeByName(symbolName)
	if len(nodes) == 0 {
		return map[string]interface{}{
			"error":   "not_found",
			"message": fmt.Sprintf("Symbol not found: %s", symbolName),
		}, nil
	}

	symbol := nodes[0]

	// Get callers (incoming CALLS)
	callers := s.engine.GetCallers(symbol.UUID)
	callerNodes := make([]map[string]interface{}, 0, len(callers))
	for _, edge := range callers {
		if node, err := s.engine.GetNode(edge.SourceUUID); err == nil {
			callerNodes = append(callerNodes, nodeSummary(node))
		}
	}

	// Get callees (outgoing CALLS)
	callees := s.engine.GetCallees(symbol.UUID)
	calleeNodes := make([]map[string]interface{}, 0, len(callees))
	for _, edge := range callees {
		if node, err := s.engine.GetNode(edge.TargetUUID); err == nil {
			calleeNodes = append(calleeNodes, nodeSummary(node))
		}
	}

	return map[string]interface{}{
		"symbol":  nodeSummary(symbol),
		"callers": callerNodes,
		"callees": calleeNodes,
	}, nil
}

func (s *Server) handleSearchCode(args map[string]interface{}) (interface{}, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query parameter required")
	}

	nodes := s.engine.FindNodeByName(query)
	if nodes == nil {
		nodes = []*graph.Node{}
	}

	return map[string]interface{}{
		"query":   query,
		"count":   len(nodes),
		"results": summarizeNodes(nodes, 30),
	}, nil
}

func (s *Server) handleProjectStructure(args map[string]interface{}) (interface{}, error) {
	// Get all files
	files := s.engine.FindNodesByType(graph.NodeTypeFile)
	modules := s.engine.FindNodesByType(graph.NodeTypeModule)

	fileSummaries := make([]map[string]interface{}, 0, len(files))
	for _, f := range files {
		fileSummaries = append(fileSummaries, nodeSummary(f))
	}

	moduleSummaries := make([]map[string]interface{}, 0, len(modules))
	for _, m := range modules {
		moduleSummaries = append(moduleSummaries, nodeSummary(m))
	}

	return map[string]interface{}{
		"total_files":  len(files),
		"total_modules": len(modules),
		"files":        fileSummaries,
		"modules":      moduleSummaries,
	}, nil
}

func (s *Server) handleRecentErrors(args map[string]interface{}) (interface{}, error) {
	limit := 10
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	entries := s.engine.FindNodesByType(graph.NodeTypeLogEntry)
	var errors []map[string]interface{}
	for _, e := range entries {
		level, _ := e.Properties["level"].(string)
		if level != "ERROR" {
			continue
		}
		errors = append(errors, map[string]interface{}{
			"uuid":      e.UUID,
			"timestamp": e.Properties["timestamp"],
			"message":   e.Properties["message"],
		})
		if len(errors) >= limit {
			break
		}
	}

	if errors == nil {
		errors = []map[string]interface{}{}
	}

	return map[string]interface{}{
		"count":  len(errors),
		"errors": errors,
	}, nil
}

// ============================================================
// JSON-RPC Helpers
// ============================================================

func (s *Server) sendResult(id interface{}, result interface{}) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	s.writeResponse(resp)
}

func (s *Server) sendError(id interface{}, code int, message string, data interface{}) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	s.writeResponse(resp)
}

func (s *Server) writeResponse(resp JSONRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		s.logger.Error("Failed to marshal response", map[string]interface{}{"error": err.Error()})
		return
	}

	_, err = fmt.Fprintln(s.writer, string(data))
	if err != nil {
		s.logger.Error("Failed to write response", map[string]interface{}{"error": err.Error()})
	}
}

// ============================================================
// Node Summarization Helpers
// ============================================================

func nodeSummary(n *graph.Node) map[string]interface{} {
	summary := map[string]interface{}{
		"uuid": n.UUID,
		"type": n.Type,
	}

	if name, ok := n.Properties["name"]; ok {
		summary["name"] = name
	}
	if sig, ok := n.Properties["signature"]; ok {
		summary["signature"] = sig
	}
	if path, ok := n.Properties["relative_path"]; ok {
		summary["path"] = path
	}
	if lang, ok := n.Properties["language"]; ok {
		summary["language"] = lang
	}

	return summary
}

func summarizeNodes(nodes []*graph.Node, max int) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, min(len(nodes), max))
	for i, n := range nodes {
		if i >= max {
			break
		}
		result = append(result, nodeSummary(n))
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
