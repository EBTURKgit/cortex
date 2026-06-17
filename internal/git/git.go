// Package git provides Git integration for Cortex agents.
// Agents can create branches, commit changes, and create PRs
// through this interface.
package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/EBTURKgit/cortex/internal/logging"
)

// Client wraps Git operations for a repository.
type Client struct {
	repoPath string
}

// NewClient creates a new Git client for the given repository.
func NewClient(repoPath string) *Client {
	logging.Debug("Creating git client", map[string]interface{}{"repo": repoPath})
	return &Client{repoPath: repoPath}
}

// Init initializes a new Git repository.
func (c *Client) Init() error {
	return c.run("init")
}

// Clone clones a remote repository.
func (c *Client) Clone(url string) error {
	return c.run("clone", url, c.repoPath)
}

// Status returns the current working tree status.
func (c *Client) Status() (string, error) {
	out, err := c.output("status", "--porcelain")
	return out, err
}

// Branch creates a new branch and checks it out.
func (c *Client) Branch(name string) error {
	return c.run("checkout", "-b", name)
}

// Checkout switches to an existing branch.
func (c *Client) Checkout(name string) error {
	return c.run("checkout", name)
}

// CurrentBranch returns the name of the current branch.
func (c *Client) CurrentBranch() (string, error) {
	out, err := c.output("rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out), err
}

// Add stages files for commit.
func (c *Client) Add(paths ...string) error {
	args := append([]string{"add"}, paths...)
	return c.run(args...)
}

// Commit creates a commit with the given message.
func (c *Client) Commit(message string) error {
	return c.run("commit", "-m", message)
}

// CommitAll stages all changes and commits.
func (c *Client) CommitAll(message string) error {
	if err := c.Add("."); err != nil {
		return err
	}
	return c.Commit(message)
}

// Diff returns the working tree diff.
func (c *Client) Diff() (string, error) {
	return c.output("diff")
}

// Log returns the recent commit log.
func (c *Client) Log(count int) (string, error) {
	return c.output("log", fmt.Sprintf("--oneline=%d", count))
}

// Push pushes the current branch to origin.
func (c *Client) Push() error {
	branch, err := c.CurrentBranch()
	if err != nil {
		return err
	}
	return c.run("push", "origin", branch)
}

// Pull pulls the latest changes.
func (c *Client) Pull() error {
	return c.run("pull", "--rebase")
}

// CreatePR creates a pull request using gh CLI (GitHub CLI).
func (c *Client) CreatePR(title, description string) (string, error) {
	// Push first
	if err := c.Push(); err != nil {
		return "", fmt.Errorf("push before PR: %w", err)
	}

	out, err := c.output("gh", "pr", "create",
		"--title", title,
		"--body", description,
		"--fill")
	if err != nil {
		return "", fmt.Errorf("create PR: %w", err)
	}

	return strings.TrimSpace(out), nil
}

// HasChanges returns true if there are uncommitted changes.
func (c *Client) HasChanges() (bool, error) {
	out, err := c.Status()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// EnsureClean ensures the working tree is clean (no uncommitted changes).
func (c *Client) EnsureClean() error {
	hasChanges, err := c.HasChanges()
	if err != nil {
		return err
	}
	if hasChanges {
		return fmt.Errorf("working tree has uncommitted changes")
	}
	return nil
}

// Tag creates an annotated tag.
func (c *Client) Tag(name, message string) error {
	return c.run("tag", "-a", name, "-m", message)
}

// run executes a git command.
func (c *Client) run(args ...string) error {
	out, err := c.output(args...)
	if err != nil {
		return fmt.Errorf("git %s: %s\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

// output executes a git command and returns stdout.
func (c *Client) output(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = c.repoPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := stdout.String()

	if err != nil {
		return out, fmt.Errorf("git error: %s\n%s", err, stderr.String())
	}

	return out, nil
}

// FindRepoRoot walks up from dir to find the git repository root.
func FindRepoRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = abs

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}
