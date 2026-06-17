// Package indexer scans source code directories and populates the Cortex graph
// with File, Function, Class, Method, and Module nodes and their relationships.
//
// It uses tree-sitter for accurate parsing when available, with a regex-based
// fallback for quick indexing.
package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/EBTURKgit/cortex/internal/graph"
	"github.com/EBTURKgit/cortex/internal/logging"

	"github.com/fsnotify/fsnotify"
)

// SupportedLanguages maps file extensions to language names.
var SupportedLanguages = map[string]string{
	".go":    "go",
	".php":   "php",
	".py":    "python",
	".js":    "javascript",
	".ts":    "typescript",
	".tsx":   "typescriptreact",
	".jsx":   "javascriptreact",
	".rs":    "rust",
	".java":  "java",
	".rb":    "ruby",
	".c":     "c",
	".cpp":   "cpp",
	".h":     "c",
	".hpp":   "cpp",
	".swift": "swift",
	".kt":    "kotlin",
	".scala": "scala",
}

// LanguageParsers holds regex patterns for extracting code structures.
// In production, this would use tree-sitter. For now, we use regex with
// reasonable accuracy for common patterns.
type LanguageParser struct {
	Name             string
	Extensions       []string
	CommentStyles    []string
	FunctionPattern  *regexp.Regexp
	ClassPattern     *regexp.Regexp
	MethodPattern    *regexp.Regexp
	InterfacePattern *regexp.Regexp
	ImportPattern    *regexp.Regexp
	ModulePattern    *regexp.Regexp
}

// Indexer watches a directory and maintains the graph in sync with the filesystem.
type Indexer struct {
	graph    *graph.GraphEngine
	rootPath string
	ignore   []string
	parsers  map[string]*LanguageParser
	watcher  *fsnotify.Watcher
	mu       sync.Mutex
	done     chan struct{}

	// Stats (protected by mu)
	filesIndexed  int
	entitiesFound int
	errors        []string

	// Per-file timeout to prevent hangs
	fileTimeout time.Duration
}

// New creates a new Indexer for the given project root.
func New(engine *graph.GraphEngine, rootPath string, ignore []string) (*Indexer, error) {
	defer logging.Trace("Indexer.New", map[string]interface{}{"root": rootPath})()

	idx := &Indexer{
		graph:       engine,
		rootPath:    rootPath,
		ignore:      ignore,
		parsers:     make(map[string]*LanguageParser),
		done:        make(chan struct{}),
		fileTimeout: 30 * time.Second,
	}

	// Load .gitignore patterns if present
	if gi, err := os.ReadFile(filepath.Join(rootPath, ".gitignore")); err == nil {
		for _, line := range strings.Split(string(gi), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				idx.ignore = append(idx.ignore, line)
			}
		}
		logging.Debug("Loaded .gitignore", map[string]interface{}{"patterns": len(idx.ignore)})
	}

	// Load .cortexignore patterns if present
	if ci, err := os.ReadFile(filepath.Join(rootPath, ".cortexignore")); err == nil {
		for _, line := range strings.Split(string(ci), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				idx.ignore = append(idx.ignore, line)
			}
		}
		logging.Debug("Loaded .cortexignore", map[string]interface{}{"patterns": len(idx.ignore)})
	}

	// Register parsers
	idx.registerParsers()

	// Create file watcher
	var err error
	idx.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create file watcher: %w", err)
	}

	logging.Info("Indexer created", map[string]interface{}{
		"root":   rootPath,
		"ignore": ignore,
	})

	return idx, nil
}

// registerParsers sets up language parsers for supported languages.
func (idx *Indexer) registerParsers() {
	// Go parser
	idx.parsers["go"] = &LanguageParser{
		Name:             "go",
		Extensions:       []string{".go"},
		FunctionPattern:  regexp.MustCompile(`func\s+(?:\([^)]*\)\s+)?([A-Za-z_]\w*)\s*\(`),
		ClassPattern:     nil,
		MethodPattern:    regexp.MustCompile(`func\s+\([^)]*\)\s+([A-Za-z_]\w*)\s*\(`),
		InterfacePattern: regexp.MustCompile(`type\s+([A-Za-z_]\w*)\s+interface\s*\{`),
		ImportPattern:    regexp.MustCompile(`(?:"([^"]+)"|'([^']+)')`),
		ModulePattern:    regexp.MustCompile(`(?:^|\n)\s*package\s+(\w+)`),
	}

	// PHP parser
	idx.parsers["php"] = &LanguageParser{
		Name:             "php",
		Extensions:       []string{".php"},
		FunctionPattern:  regexp.MustCompile(`function\s+([A-Za-z_]\w*)\s*\(`),
		ClassPattern:     regexp.MustCompile(`(?:abstract\s+)?class\s+([A-Za-z_]\w*)`),
		MethodPattern:    regexp.MustCompile(`(?:public|protected|private|static|abstract)?\s*function\s+([A-Za-z_]\w*)\s*\(`),
		InterfacePattern: regexp.MustCompile(`interface\s+([A-Za-z_]\w*)`),
		ImportPattern:    regexp.MustCompile(`(?:use|import|require(?:_once)?)\s+([^;]+)`),
		ModulePattern:    regexp.MustCompile(`(?:^|\n)\s*namespace\s+([^;]+)`),
	}

	// Python parser
	idx.parsers["python"] = &LanguageParser{
		Name:             "python",
		Extensions:       []string{".py"},
		FunctionPattern:  regexp.MustCompile(`(?:^|\n)\s*def\s+([A-Za-z_]\w*)\s*\(`),
		ClassPattern:     regexp.MustCompile(`(?:^|\n)\s*class\s+([A-Za-z_]\w*)`),
		MethodPattern:    regexp.MustCompile(`(?:^|\n)\s+def\s+([A-Za-z_]\w*)\s*\(`),
		InterfacePattern: nil,
		ImportPattern:    regexp.MustCompile(`(?:from\s+(\S+)\s+)?import\s+(\S+)`),
		ModulePattern:    nil,
	}

	// JavaScript/TypeScript parser
	idx.parsers["javascript"] = &LanguageParser{
		Name:             "javascript",
		Extensions:       []string{".js", ".jsx", ".ts", ".tsx"},
		FunctionPattern:  regexp.MustCompile(`(?:function\s+|const\s+\w+\s*=\s*(?:async\s+)?(?:function\s*)?\()([A-Za-z_$]\w*)?`),
		ClassPattern:     regexp.MustCompile(`class\s+([A-Za-z_$]\w*)`),
		MethodPattern:    regexp.MustCompile(`(\w+)\s*\([^)]*\)\s*\{`),
		InterfacePattern: regexp.MustCompile(`interface\s+([A-Za-z_$]\w*)`),
		ImportPattern:    regexp.MustCompile(`(?:import\s+(?:\{[^}]*\}|[^;]+)|require\s*\(['"]([^'"]+)['"]\))`),
		ModulePattern:    nil,
	}

	// Also register "typescript" pointing to the same parser
	idx.parsers["typescript"] = idx.parsers["javascript"]
	idx.parsers["typescriptreact"] = idx.parsers["javascript"]
	idx.parsers["javascriptreact"] = idx.parsers["javascript"]
}

// ============================================================
// Full Scan (Import)
// ============================================================

// Scan performs a full scan of the root path and indexes all supported files.
// This is the main entry point for `cortex import`.
func (idx *Indexer) Scan() (*ScanResult, error) {
	defer logging.Trace("Indexer.Scan")()

	logging.Info("Starting full scan", map[string]interface{}{
		"root": idx.rootPath,
	})

	result := &ScanResult{
		StartTime: time.Now(),
		Files:     make([]string, 0),
		FileCount: 0,
	}

	err := filepath.Walk(idx.rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logging.Warn("Walk error", map[string]interface{}{"path": path, "error": err.Error()})
			return nil // skip errors
		}

		// Check if should be ignored
		relPath, _ := filepath.Rel(idx.rootPath, path)
		if idx.isIgnored(relPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return nil
		}

		// Check if file is a supported language
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := SupportedLanguages[ext]; !ok {
			return nil
		}

		result.Files = append(result.Files, relPath)
		result.FileCount++

		// Index this file
		fileResult := idx.indexFile(path, relPath)
		result.EntitiesFound += fileResult.EntitiesFound
		result.Errors = append(result.Errors, fileResult.Errors...)

		return nil
	})

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	// Second pass: extract CALLS edges between known functions
	callCount := idx.extractCallsEdges()
	result.EntitiesFound += callCount

	logging.Info("Scan complete", map[string]interface{}{
		"files":    result.FileCount,
		"entities": result.EntitiesFound,
		"errors":   len(result.Errors),
		"duration": result.Duration.String(),
		"calls":    callCount,
	})

	return result, err
}

// ScanResult contains the results of a full scan.
type ScanResult struct {
	StartTime     time.Time     `json:"start_time"`
	EndTime       time.Time     `json:"end_time"`
	Duration      time.Duration `json:"duration"`
	Files         []string      `json:"files"`
	FileCount     int           `json:"file_count"`
	EntitiesFound int           `json:"entities_found"`
	Errors        []string      `json:"errors"`
}

// FileResult contains the result of indexing a single file.
type FileResult struct {
	EntitiesFound int
	Errors        []string
}

// indexFile parses a single file and creates graph nodes for its contents.
func (idx *Indexer) indexFile(absPath, relPath string) *FileResult {
	defer logging.Trace("Indexer.indexFile", map[string]interface{}{"file": relPath})()

	result := &FileResult{}

	// Per-file timeout to prevent hangs
	var fileData []byte
	type fileResult struct {
		data []byte
		err  error
	}
	ch := make(chan fileResult, 1)
	go func() {
		d, e := os.ReadFile(absPath)
		ch <- fileResult{d, e}
	}()
	select {
	case fr := <-ch:
		if fr.err != nil {
			errMsg := fmt.Sprintf("read %s: %s", relPath, fr.err)
			result.Errors = append(result.Errors, errMsg)
			idx.createErrorLog(errMsg)
			return result
		}
		fileData = fr.data
	case <-time.After(idx.fileTimeout):
		errMsg := fmt.Sprintf("timeout reading %s after %s", relPath, idx.fileTimeout)
		result.Errors = append(result.Errors, errMsg)
		idx.createErrorLog(errMsg)
		return result
	}

	content := string(fileData)
	ext := strings.ToLower(filepath.Ext(absPath))
	lang := SupportedLanguages[ext]

	parser, ok := idx.parsers[lang]
	if !ok {
		// No parser for this language, still create a file node
		result.EntitiesFound++
		idx.createFileNode(relPath, lang, content)
		return result
	}

	// Create file node and track its UUID for CONTAINS edges
	fileUUID := idx.createFileNode(relPath, lang, content)

	// Helper to create CONTAINS edge from file to entity
	linkToFile := func(entityUUID string) {
		if fileUUID != "" && entityUUID != "" {
			if _, err := idx.graph.CreateEdge(fileUUID, entityUUID, graph.EdgeContains, nil); err != nil {
				logging.Debug("Failed to create CONTAINS edge",
					map[string]interface{}{"file": relPath, "error": err.Error()})
			}
		}
	}

	// Extract module/namespace
	moduleName := ""
	if parser.ModulePattern != nil {
		matches := parser.ModulePattern.FindStringSubmatch(content)
		if len(matches) > 1 {
			moduleName = strings.TrimSpace(matches[1])
		}
	}

	// Create module node if found
	if moduleName != "" {
		modUUID := idx.createModuleNode(moduleName, relPath, lang)
		linkToFile(modUUID)
	}

	// Extract functions (standalone)
	if parser.FunctionPattern != nil {
		matches := parser.FunctionPattern.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 && m[1] != "" {
				funcName := strings.TrimSpace(m[1])
				result.EntitiesFound++
				funcUUID := idx.createFunctionNode(funcName, relPath, lang, "")
				linkToFile(funcUUID)
			}
		}
	}

	// Extract classes
	if parser.ClassPattern != nil {
		matches := parser.ClassPattern.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 {
				className := strings.TrimSpace(m[1])
				result.EntitiesFound++
				classUUID := idx.createClassNode(className, relPath, lang)
				linkToFile(classUUID)
			}
		}
	}

	// Extract interfaces
	if parser.InterfacePattern != nil {
		matches := parser.InterfacePattern.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 {
				ifaceName := strings.TrimSpace(m[1])
				result.EntitiesFound++
				ifaceUUID := idx.createInterfaceNode(ifaceName, relPath, lang)
				linkToFile(ifaceUUID)
			}
		}
	}

	// Extract methods (inside classes)
	if parser.MethodPattern != nil {
		matches := parser.MethodPattern.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 && m[1] != "" {
				methodName := strings.TrimSpace(m[1])
				result.EntitiesFound++
				methodUUID := idx.createMethodNode(methodName, relPath, lang)
				linkToFile(methodUUID)
			}
		}
	}

	// Extract imports and create IMPORT edges
	if parser.ImportPattern != nil {
		matches := parser.ImportPattern.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if len(m) > 1 && m[1] != "" {
				imp := strings.TrimSpace(m[1])
				if imp != "" && !strings.HasPrefix(imp, "/") {
					// Create an import edge from current file to the imported module
					fileNodes := idx.graph.FindNodeByName(relPath)
					impNodes := idx.graph.FindNodeByName(imp)
					if len(fileNodes) > 0 && len(impNodes) > 0 {
						idx.graph.CreateEdge(fileNodes[0].UUID, impNodes[0].UUID, graph.EdgeImports, nil)
					}
				}
			}
		}
	}

	return result
}

// extractCallsEdges does a second pass over all files to create CALLS edges
// between functions. This runs after all nodes have been created.
func (idx *Indexer) extractCallsEdges() int {
	defer logging.Trace("Indexer.extractCallsEdges")()

	count := 0
	callPattern := regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)

	// Walk all files
	filepath.Walk(idx.rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(idx.rootPath, path)
		if idx.isIgnored(relPath) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := SupportedLanguages[ext]; !ok {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		content := string(data)
		callMatches := callPattern.FindAllStringSubmatch(content, -1)
		seen := make(map[string]bool)

		for _, m := range callMatches {
			if len(m) > 1 {
				callName := m[1]
				if isKeyword(callName) || seen[callName] {
					continue
				}
				seen[callName] = true

				callees := idx.graph.FindNodeByName(callName)
				for _, callee := range callees {
					if callee.Type == graph.NodeTypeFunction || callee.Type == graph.NodeTypeMethod {
						fileNodes := idx.graph.FindNodeByName(relPath)
						if len(fileNodes) > 0 {
							if _, err := idx.graph.CreateEdge(
								fileNodes[0].UUID, callee.UUID, graph.EdgeCalls,
								map[string]interface{}{"call_type": "static", "file": relPath},
							); err == nil {
								count++
							}
						}
					}
				}
			}
		}
		return nil
	})

	if count > 0 {
		logging.Debug("CALLS edges created", map[string]interface{}{"count": count})
	}
	return count
}

// createErrorLog creates a LogEntry node for indexer errors.
func (idx *Indexer) createErrorLog(message string) {
	idx.graph.CreateNode(graph.NodeTypeLogEntry, map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"level":     "ERROR",
		"message":   message,
		"source":    "indexer",
	})
}

// Status returns current indexing progress.
func (idx *Indexer) Stats() (filesIndexed, entitiesFound, errors int) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.filesIndexed, idx.entitiesFound, len(idx.errors)
}

// isKeyword returns true if the name is a Go/reserved keyword to skip.
func isKeyword(name string) bool {
	switch name {
	case "if", "for", "switch", "select", "case", "return", "go", "defer",
		"func", "type", "struct", "interface", "map", "chan", "range",
		"append", "len", "cap", "make", "new", "copy", "delete", "close",
		"panic", "recover", "print", "println", "error", "string", "int",
		"bool", "float64", "true", "false", "nil", "iota", "import",
		"package", "var", "const", "else", "fallthrough", "continue",
		"break", "default":
		return true
	}
	return false
}

// ============================================================
// Graph Node Creators
// ============================================================

func (idx *Indexer) createFileNode(relPath, language, content string) string {
	n, err := idx.graph.CreateNode(graph.NodeTypeFile, map[string]interface{}{
		"name":            relPath,
		"relative_path":   relPath,
		"language":        language,
		"checksum":        fmt.Sprintf("%d", len(content)),
		"last_indexed_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		logging.Warn("Failed to create file node", map[string]interface{}{
			"path": relPath, "error": err.Error(),
		})
		return ""
	}
	return n.UUID
}

func (idx *Indexer) createModuleNode(name, filePath, language string) string {
	n, err := idx.graph.CreateNode(graph.NodeTypeModule, map[string]interface{}{
		"name":      name,
		"file_path": filePath,
		"language":  language,
		"type":      "module",
	})
	if err != nil {
		logging.Warn("Failed to create module node", map[string]interface{}{
			"name": name, "error": err.Error(),
		})
		return ""
	}
	return n.UUID
}

func (idx *Indexer) createFunctionNode(name, filePath, language, signature string) string {
	n, err := idx.graph.CreateNode(graph.NodeTypeFunction, map[string]interface{}{
		"name":      name,
		"file_path": filePath,
		"language":  language,
		"signature": signature,
	})
	if err != nil {
		logging.Warn("Failed to create function node", map[string]interface{}{
			"name": name, "error": err.Error(),
		})
		return ""
	}
	return n.UUID
}

func (idx *Indexer) createClassNode(name, filePath, language string) string {
	n, err := idx.graph.CreateNode(graph.NodeTypeClass, map[string]interface{}{
		"name":      name,
		"file_path": filePath,
		"language":  language,
	})
	if err != nil {
		logging.Warn("Failed to create class node", map[string]interface{}{
			"name": name, "error": err.Error(),
		})
		return ""
	}
	return n.UUID
}

func (idx *Indexer) createMethodNode(name, filePath, language string) string {
	n, err := idx.graph.CreateNode(graph.NodeTypeMethod, map[string]interface{}{
		"name":      name,
		"file_path": filePath,
		"language":  language,
	})
	if err != nil {
		logging.Warn("Failed to create method node", map[string]interface{}{
			"name": name, "error": err.Error(),
		})
		return ""
	}
	return n.UUID
}

func (idx *Indexer) createInterfaceNode(name, filePath, language string) string {
	n, err := idx.graph.CreateNode(graph.NodeTypeInterface, map[string]interface{}{
		"name":      name,
		"file_path": filePath,
		"language":  language,
	})
	if err != nil {
		logging.Warn("Failed to create interface node", map[string]interface{}{
			"name": name, "error": err.Error(),
		})
		return ""
	}
	return n.UUID
}

// ============================================================
// File Watching
// ============================================================

// Watch starts watching the filesystem for changes.
func (idx *Indexer) Watch() error {
	defer logging.Trace("Indexer.Watch")()

	// Add all directories to the watcher
	err := filepath.Walk(idx.rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			relPath, _ := filepath.Rel(idx.rootPath, path)
			if idx.isIgnored(relPath) {
				return filepath.SkipDir
			}
			return idx.watcher.Add(path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("setup watcher: %w", err)
	}

	logging.Info("File watcher started", map[string]interface{}{
		"root": idx.rootPath,
	})

	go idx.watchLoop()

	return nil
}

// watchLoop processes filesystem events.
func (idx *Indexer) watchLoop() {
	for {
		select {
		case event, ok := <-idx.watcher.Events:
			if !ok {
				return
			}
			idx.handleFSEvent(event)

		case err, ok := <-idx.watcher.Errors:
			if !ok {
				return
			}
			logging.Error("File watcher error", map[string]interface{}{"error": err.Error()})

		case <-idx.done:
			return
		}
	}
}

// handleFSEvent processes a single filesystem event.
func (idx *Indexer) handleFSEvent(event fsnotify.Event) {
	relPath, err := filepath.Rel(idx.rootPath, event.Name)
	if err != nil {
		return
	}

	if idx.isIgnored(relPath) {
		return
	}

	ext := strings.ToLower(filepath.Ext(event.Name))
	if _, ok := SupportedLanguages[ext]; !ok {
		return
	}

	logging.Debug("FS event", map[string]interface{}{
		"path": relPath,
		"op":   event.Op.String(),
	})

	switch {
	case event.Op&fsnotify.Create != 0:
		// New file or directory
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			idx.watcher.Add(event.Name)
		} else {
			idx.indexFile(event.Name, relPath)
		}

	case event.Op&fsnotify.Write != 0:
		// File modified — re-index
		// In a real implementation, we'd diff the old and new entities
		idx.indexFile(event.Name, relPath)

	case event.Op&fsnotify.Remove != 0:
		// File deleted — remove from graph
		nodes := idx.graph.FindNodeByName(relPath)
		for _, n := range nodes {
			if n.Type == graph.NodeTypeFile {
				idx.graph.DeleteNode(n.UUID)
			}
		}
	}
}

// Stop shuts down the file watcher.
func (idx *Indexer) Stop() error {
	logging.Debug("Stopping indexer")
	close(idx.done)
	return idx.watcher.Close()
}

// ============================================================
// Helpers
// ============================================================

// isIgnored checks if a path matches any ignore patterns.
func (idx *Indexer) isIgnored(relPath string) bool {
	for _, pattern := range idx.ignore {
		if strings.HasPrefix(relPath, pattern) || strings.Contains(relPath, "/"+pattern+"/") {
			return true
		}
		// Simple glob: if pattern ends with *, check prefix
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(relPath, prefix) {
				return true
			}
		}
	}
	return false
}

// GetParser returns the parser for a given language, or nil.
func (idx *Indexer) GetParser(language string) *LanguageParser {
	return idx.parsers[language]
}
