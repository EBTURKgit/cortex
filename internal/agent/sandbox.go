package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/EBTURKgit/cortex/internal/logging"
)

// SandboxResult contains the output of a sandboxed command execution.
type SandboxResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Duration string `json:"duration"`
}

// Sandbox provides a restricted environment for running commands.
// It constrains the working directory to the project root and enforces
// timeouts and resource limits.
type Sandbox struct {
	workDir     string
	timeout     time.Duration
	allowedCmds []string
}

// NewSandbox creates a new sandbox rooted at the given directory.
func NewSandbox(workDir string) *Sandbox {
	logging.Debug("Creating sandbox", map[string]interface{}{"workdir": workDir})

	return &Sandbox{
		workDir: workDir,
		timeout: 60 * time.Second,
		allowedCmds: []string{
			"go", "php", "python", "node", "npm", "npx",
			"git", "make", "gcc", "clang", "rustc", "cargo",
			"docker", "composer", "pip", "pip3",
			"ls", "cat", "head", "tail", "wc", "find", "grep",
			"mkdir", "cp", "mv", "rm", "chmod",
			"echo", "printf", "test", "[",
			"pwd", "which", "uname",
		},
	}
}

// Run executes a command in the sandbox and returns the result.
func (s *Sandbox) Run(command string, args ...string) (*SandboxResult, error) {
	defer logging.Trace("Sandbox.Run",
		map[string]interface{}{"cmd": command, "args": args})()

	// Validate the command is allowed
	if !s.isAllowed(command) {
		return nil, fmt.Errorf("command not allowed: %s", command)
	}

	// Resolve the command path
	cmdPath, err := exec.LookPath(command)
	if err != nil {
		return nil, fmt.Errorf("command not found: %s", command)
	}

	// Create the command with timeout
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdPath, args...)
	cmd.Dir = s.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			exitCode = -1 // timeout
			err = fmt.Errorf("command timed out after %s", s.timeout)
		} else {
			exitCode = -2
		}
	}

	result := &SandboxResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Duration: duration.Round(time.Millisecond).String(),
	}

	logging.Debug("Command executed",
		map[string]interface{}{
			"cmd":       command,
			"exit_code": exitCode,
			"duration":  result.Duration,
		})

	return result, err
}

// RunScript writes a script file and executes it in the sandbox.
func (s *Sandbox) RunScript(script string, interpreter string) (*SandboxResult, error) {
	defer logging.Trace("Sandbox.RunScript",
		map[string]interface{}{"interpreter": interpreter})()

	// Write script to temp file in workdir
	tmpFile := filepath.Join(s.workDir, ".cortex", "tmp", fmt.Sprintf("script_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(filepath.Dir(tmpFile), 0755); err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	if err := os.WriteFile(tmpFile, []byte(script), 0600); err != nil {
		return nil, fmt.Errorf("write script: %w", err)
	}
	defer os.Remove(tmpFile)

	return s.Run(interpreter, tmpFile)
}

// ReadFile reads a file from within the sandbox (path is relative to workDir).
func (s *Sandbox) ReadFile(path string) (string, error) {
	fullPath := s.resolvePath(path)
	if err := s.ValidatePathInSandbox(fullPath); err != nil {
		return "", err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

// WriteFile writes a file within the sandbox.
func (s *Sandbox) WriteFile(path, content string) error {
	fullPath := s.resolvePath(path)
	if err := s.ValidatePathInSandbox(fullPath); err != nil {
		return err
	}
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	return os.WriteFile(fullPath, []byte(content), 0644)
}

// resolvePath ensures the path is within the sandbox working directory.
// If the path is absolute, it is joined with workDir after cleaning to prevent traversal.
func (s *Sandbox) resolvePath(path string) string {
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) {
		return filepath.Join(s.workDir, clean)
	}
	return filepath.Join(s.workDir, clean)
}

// isAllowed checks if a command is in the allowed list.
func (s *Sandbox) isAllowed(command string) bool {
	base := filepath.Base(command)
	for _, allowed := range s.allowedCmds {
		if base == allowed {
			return true
		}
	}
	return false
}

// ValidatePathInSandbox checks that a resolved path stays within the sandbox.
func (s *Sandbox) ValidatePathInSandbox(path string) error {
	rel, err := filepath.Rel(s.workDir, path)
	if err != nil {
		return fmt.Errorf("path %s: %w", path, err)
	}
	if strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path %s is outside sandbox %s", path, s.workDir)
	}
	return nil
}
