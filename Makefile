.PHONY: build run test clean tidy serve build-linux build-macos build-windows

# Build for current platform
build:
	@echo "Building cortex..."
	@mkdir -p bin
	go build -o bin/cortex .
	@echo "Done: bin/cortex"

# Cross-compile for Linux amd64
build-linux:
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 go build -o bin/cortex-linux-amd64 .
	@echo "Done: bin/cortex-linux-amd64"

# Cross-compile for macOS amd64
build-macos:
	@mkdir -p bin
	GOOS=darwin GOARCH=amd64 go build -o bin/cortex-darwin-amd64 .
	@echo "Done: bin/cortex-darwin-amd64"

# Cross-compile for macOS arm64 (Apple Silicon)
build-macos-arm:
	@mkdir -p bin
	GOOS=darwin GOARCH=arm64 go build -o bin/cortex-darwin-arm64 .
	@echo "Done: bin/cortex-darwin-arm64"

# Cross-compile for Windows amd64
build-windows:
	@mkdir -p bin
	GOOS=windows GOARCH=amd64 go build -o bin/cortex-windows-amd64.exe .
	@echo "Done: bin/cortex-windows-amd64.exe"

# Build all platforms
build-all: build-linux build-macos build-macos-arm build-windows

# Run in dev mode (server foreground)
run: build
	./bin/cortex serve

# Run all tests
test:
	go test ./... -v 2>&1 | tail -50

# Clean build artifacts
clean:
	rm -rf bin/
	go clean

# Tidy dependencies
tidy:
	go mod tidy

# Start server in background
serve: build
	./bin/cortex serve
