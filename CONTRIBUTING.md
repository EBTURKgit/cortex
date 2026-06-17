# Contributing to Cortex

Thanks for your interest! Here's how to contribute.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/EBTURKgit/cortex.git`
3. Create a branch: `git checkout -b my-feature`
4. Make your changes
5. Run tests: `make test`
6. Run vet: `go vet ./...`
7. Commit and push
8. Open a pull request

## Development Setup

```bash
# Build
make build

# Test
make test

# Cross-compile all platforms
make build-all
```

## Code Standards

- **Go 1.22+** — use modern Go idioms
- **Error handling** — always check errors, return them with context using `fmt.Errorf("context: %w", err)`
- **Logging** — use `defer logging.Trace(...)` on every public function
- **Comments** — doc comments on all exported symbols
- **No external runtime dependencies** — single binary, zero deps at runtime
- **Tests** — add tests for new functionality

## Adding a New Language to the Indexer

1. Add file extension to `SupportedLanguages` in `internal/indexer/indexer.go`
2. Add a parser entry in `registerParsers()` with regex patterns
3. Add test fixtures in `internal/indexer/testdata/`

## Adding a New MCP Tool

1. Add tool definition in `handleToolsList()` in `internal/mcp/mcp.go`
2. Add handler function
3. Add case in `handleToolsCall()` switch
4. Wrap result in MCP content format

## Adding a New CLI Command

1. Add cobra command in `cmd/root.go`
2. Register in `init()`
3. Add help text with examples

## Reporting Issues

- Use GitHub Issues
- Include: Go version, OS, cortex version, steps to reproduce
- For bugs: include the full error output

## Code of Conduct

Please read and follow our [Code of Conduct](CODE_OF_CONDUCT.md).
