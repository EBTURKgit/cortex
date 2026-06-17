// Package ingestion receives application logs, traces, and errors
// and links them to the code entities in the Cortex graph.
// This enables the Runtime-to-Code feedback loop — agents can query
// "what errors happened in function X in the last hour?"
package ingestion

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/EBTURKgit/cortex/internal/graph"
	"github.com/EBTURKgit/cortex/internal/logging"

	"github.com/google/uuid"
)

// LogLevel represents the severity of a log entry.
type LogLevel string

const (
	Debug LogLevel = "DEBUG"
	Info  LogLevel = "INFO"
	Warn  LogLevel = "WARN"
	Error LogLevel = "ERROR"
)

// LogEntry represents a single log line from the application.
type LogEntry struct {
	Timestamp   time.Time              `json:"timestamp"`
	Level       LogLevel               `json:"level"`
	Message     string                 `json:"message"`
	FunctionID  string                 `json:"cortex_function_id,omitempty"`
	File        string                 `json:"file,omitempty"`
	Line        int                    `json:"line,omitempty"`
	Raw         map[string]interface{} `json:"raw,omitempty"`
}

// TraceSpan represents a single span in a distributed trace.
type TraceSpan struct {
	TraceID       string  `json:"trace_id"`
	SpanID        string  `json:"span_id"`
	ParentSpanID  string  `json:"parent_span_id,omitempty"`
	OperationName string  `json:"operation_name"`
	FunctionID    string  `json:"cortex_function_id,omitempty"`
	StartTime     int64   `json:"start_time"` // unix millis
	DurationMs    float64 `json:"duration_ms"`
	Status        string  `json:"status"`
}

// LogIngestionError is emitted when linking a log to a function fails.
type LogIngestionError struct {
	Log     LogEntry `json:"log"`
	Reason  string   `json:"reason"`
}

// Service ingests logs and traces, linking them to graph nodes.
type Service struct {
	engine     *graph.GraphEngine
	mu         sync.Mutex
	errorCount int

	// Stats
	stats struct {
		LogsReceived   int `json:"logs_received"`
		LogsLinked     int `json:"logs_linked"`
		TracesReceived int `json:"traces_received"`
		TracesLinked   int `json:"traces_linked"`
		Errors         int `json:"errors"`
	}
}

// NewService creates a new ingestion service backed by the given graph engine.
func NewService(engine *graph.GraphEngine) *Service {
	logging.Debug("Creating ingestion service")
	return &Service{
		engine: engine,
	}
}

// ============================================================
// Log Ingestion
// ============================================================

// IngestLog processes a single log entry and creates graph nodes.
func (s *Service) IngestLog(entry LogEntry) error {
	defer logging.Trace("Ingestion.IngestLog",
		map[string]interface{}{"level": entry.Level, "function": entry.FunctionID})()

	s.mu.Lock()
	s.stats.LogsReceived++
	s.mu.Unlock()

	// Create the LogEntry node
	rawJSON := ""
	if entry.Raw != nil {
		if data, err := json.Marshal(entry.Raw); err == nil {
			rawJSON = string(data)
		}
	}

	node, err := s.engine.CreateNode(graph.NodeTypeLogEntry, map[string]interface{}{
		"timestamp": entry.Timestamp.Format(time.RFC3339Nano),
		"level":     string(entry.Level),
		"message":   entry.Message,
		"raw_json":  rawJSON,
	})
	if err != nil {
		s.mu.Lock()
		s.stats.Errors++
		s.mu.Unlock()
		return fmt.Errorf("create log entry node: %w", err)
	}

	// Link to function if function_id is provided
	if entry.FunctionID != "" {
		if _, err := s.engine.GetNode(entry.FunctionID); err == nil {
			s.engine.CreateEdge(node.UUID, entry.FunctionID, graph.EdgeTriggeredBy, map[string]interface{}{
				"file": entry.File,
				"line": entry.Line,
			})
			s.mu.Lock()
			s.stats.LogsLinked++
			s.mu.Unlock()

			// Auto-create HAS_ERROR edge for ERROR-level logs
			if entry.Level == Error {
				s.engine.CreateEdge(node.UUID, entry.FunctionID, graph.EdgeHasError, nil)
				logging.Debug("Error linked to function",
					map[string]interface{}{"function": entry.FunctionID})
			}
		} else {
			logging.Debug("Function not found for log link",
				map[string]interface{}{"function_id": entry.FunctionID})
		}
	}

	return nil
}

// IngestLogBatch processes multiple log entries in a single transaction.
func (s *Service) IngestLogBatch(entries []LogEntry) (int, error) {
	success := 0
	for _, entry := range entries {
		if err := s.IngestLog(entry); err != nil {
			logging.Warn("Failed to ingest log",
				map[string]interface{}{"error": err.Error()})
			continue
		}
		success++
	}
	return success, nil
}

// ============================================================
// Trace Ingestion
// ============================================================

// IngestTrace processes a trace span and creates graph nodes.
func (s *Service) IngestTrace(span TraceSpan) error {
	defer logging.Trace("Ingestion.IngestTrace",
		map[string]interface{}{"trace": span.TraceID, "span": span.SpanID})()

	s.mu.Lock()
	s.stats.TracesReceived++
	s.mu.Unlock()

	node, err := s.engine.CreateNode(graph.NodeTypeTraceSpan, map[string]interface{}{
		"trace_id":        span.TraceID,
		"span_id":         span.SpanID,
		"parent_span_id":  span.ParentSpanID,
		"operation_name":  span.OperationName,
		"start_time":      span.StartTime,
		"duration_ms":     span.DurationMs,
		"status":          span.Status,
	})
	if err != nil {
		return fmt.Errorf("create trace span node: %w", err)
	}

	// Link to function if function_id is provided
	if span.FunctionID != "" {
		if _, err := s.engine.GetNode(span.FunctionID); err == nil {
			s.engine.CreateEdge(node.UUID, span.FunctionID, graph.EdgeTriggeredBy, nil)
			s.mu.Lock()
			s.stats.TracesLinked++
			s.mu.Unlock()
		}
	}

	// Link to parent span
	if span.ParentSpanID != "" {
		parentSpans := s.engine.FindNodeByName(span.ParentSpanID)
		for _, parent := range parentSpans {
			if parent.Type == graph.NodeTypeTraceSpan {
				s.engine.CreateEdge(node.UUID, parent.UUID, graph.EdgeBelongsToTrace, nil)
				break
			}
		}
	}

	return nil
}

// ============================================================
// HTTP Handlers (for application-side ingestion)
// ============================================================

// HandleLogIngestion is an HTTP handler that receives log entries.
func (s *Service) HandleLogIngestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Try single entry first
	var entry LogEntry
	if err := json.Unmarshal(body, &entry); err == nil && entry.Message != "" {
		if err := s.IngestLog(entry); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
		return
	}

	// Try batch
	var batch []LogEntry
	if err := json.Unmarshal(body, &batch); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	success, err := s.IngestLogBatch(batch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "accepted",
		"ingested": success,
		"total":   len(batch),
	})
}

// HandleTraceIngestion is an HTTP handler that receives trace spans.
func (s *Service) HandleTraceIngestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var span TraceSpan
	if err := json.NewDecoder(r.Body).Decode(&span); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.IngestTrace(span); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

// Stats returns current ingestion statistics.
func (s *Service) Stats() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]int{
		"logs_received":   s.stats.LogsReceived,
		"logs_linked":     s.stats.LogsLinked,
		"traces_received": s.stats.TracesReceived,
		"traces_linked":   s.stats.TracesLinked,
		"errors":          s.stats.Errors,
	}
}

// Must be used by uuid package
var _ = uuid.New
