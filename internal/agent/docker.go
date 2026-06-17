// Package agent provides Docker-based sandbox for secure agent execution.
// Each agent runs in an isolated container with only the project directory mounted.
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

// DockerSandbox provides container-level isolation for agent command execution.
type DockerSandbox struct {
	workDir     string
	image       string
	timeout     time.Duration
	containerID string
}

// NewDockerSandbox creates a new Docker sandbox.
func NewDockerSandbox(workDir string) *DockerSandbox {
	logging.Debug("Creating Docker sandbox", map[string]interface{}{"workdir": workDir})
	return &DockerSandbox{
		workDir: workDir,
		image:   "cortex-agent:latest",
		timeout: 5 * time.Minute,
	}
}

// EnsureImage ensures the Docker image exists, building it if needed.
func (s *DockerSandbox) EnsureImage(ctx context.Context) error {
	defer logging.Trace("DockerSandbox.EnsureImage")()

	// Check if image exists
	if err := exec.CommandContext(ctx, "docker", "image", "inspect", s.image).Run(); err == nil {
		return nil
	}

	// Build a minimal agent image
	logging.Info("Building agent Docker image", map[string]interface{}{"image": s.image})

	dockerfile := `FROM alpine:3.19
RUN apk add --no-cache git go python3 nodejs npm php composer rust cargo gcc musl-dev
WORKDIR /project
CMD ["sleep", "infinity"]`

	tmpDir, err := os.MkdirTemp("", "cortex-docker")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}

	cmd := exec.CommandContext(ctx, "docker", "build", "-t", s.image, tmpDir)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build Docker image: %w", err)
	}

	logging.Info("Agent Docker image built", map[string]interface{}{"image": s.image})
	return nil
}

// Run executes a command inside a Docker container.
func (s *DockerSandbox) Run(command string, args ...string) (*SandboxResult, error) {
	defer logging.Trace("DockerSandbox.Run", map[string]interface{}{"cmd": command})()

	if err := s.EnsureImage(context.Background()); err != nil {
		return nil, fmt.Errorf("ensure image: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	// Create container with project mounted
	dockerArgs := []string{
		"run", "--rm",
		"--network", "none", // no network access
		"--read-only", // read-only filesystem
		"--security-opt", "no-new-privileges:true",
		"--cap-drop", "ALL",
		"-v", s.workDir + ":/project:ro", // project mounted read-only
		"-w", "/project",
		s.image,
		command,
	}
	dockerArgs = append(dockerArgs, args...)

	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			exitCode = -1
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

	logging.Debug("Docker command executed",
		map[string]interface{}{
			"cmd":       command,
			"exit_code": exitCode,
			"duration":  result.Duration,
		})

	return result, err
}

// RunScript writes a script and runs it inside Docker.
func (s *DockerSandbox) RunScript(script string, interpreter string) (*SandboxResult, error) {
	tmpFile := filepath.Join(s.workDir, ".cortex", "tmp", fmt.Sprintf("script_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(filepath.Dir(tmpFile), 0755); err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	if err := os.WriteFile(tmpFile, []byte(script), 0600); err != nil {
		return nil, fmt.Errorf("write script: %w", err)
	}
	defer os.Remove(tmpFile)

	// Re-run without read-only for the script
	return s.Run(interpreter, tmpFile)
}

// ReadFile reads a file from the project directory.
func (s *DockerSandbox) ReadFile(path string) (string, error) {
	fullPath := filepath.Join(s.workDir, path)
	rel, err := filepath.Rel(s.workDir, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path outside sandbox: %s", path)
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

// WriteFile writes a file to the project directory.
func (s *DockerSandbox) WriteFile(path, content string) error {
	fullPath := filepath.Join(s.workDir, path)
	rel, err := filepath.Rel(s.workDir, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("path outside sandbox: %s", path)
	}
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	return os.WriteFile(fullPath, []byte(content), 0644)
}
