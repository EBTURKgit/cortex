// Package git provides automated Git workflow for Cortex agents.
// Agents can auto-branch, commit, and create PRs when tasks complete.
package git

import (
	"fmt"
	"strings"

	"github.com/EBTURKgit/cortex/internal/logging"
)

// Workflow manages automated Git operations for task-driven development.
type Workflow struct {
	client *Client
}

// NewWorkflow creates a new Git workflow for the given repository.
func NewWorkflow(repoPath string) *Workflow {
	return &Workflow{
		client: NewClient(repoPath),
	}
}

// TaskBranch generates a branch name from a task title.
func TaskBranch(taskTitle string) string {
	safe := strings.ToLower(taskTitle)
	safe = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, safe)
	safe = strings.Trim(safe, "-")
	if len(safe) > 50 {
		safe = safe[:50]
	}
	return "github.com/EBTURKgit/cortex/" + safe
}

// CommitMessage formats a commit message with task reference.
func CommitMessage(taskTitle, taskUUID string) string {
	shortUUID := taskUUID
	if len(shortUUID) > 8 {
		shortUUID = shortUUID[:8]
	}
	return fmt.Sprintf("cortex: %s\n\nTask: %s", taskTitle, shortUUID)
}

// AutoBranch creates a branch for a task and commits changes.
// Returns the branch name.
func (w *Workflow) AutoBranch(taskTitle, taskUUID string) (string, error) {
	defer logging.Trace("Workflow.AutoBranch",
		map[string]interface{}{"task": taskTitle})()

	branch := TaskBranch(taskTitle)

	// Check if already on this branch
	current, err := w.client.CurrentBranch()
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}

	if current != branch {
		// Stash any uncommitted changes first
		hasChanges, _ := w.client.HasChanges()
		if hasChanges {
			if err := w.client.run("stash"); err != nil {
				return "", fmt.Errorf("stash: %w", err)
			}
		}

		// Create and switch to task branch
		if err := w.client.Branch(branch); err != nil {
			// Branch may already exist, try checkout
			if err2 := w.client.Checkout(branch); err2 != nil {
				return "", fmt.Errorf("branch/checkout: %s / %s", err, err2)
			}
		}

		// Apply stashed changes
		if hasChanges {
			w.client.run("stash", "pop")
		}
	}

	logging.Info("Switched to task branch", map[string]interface{}{
		"branch": branch, "task": taskTitle,
	})

	return branch, nil
}

// AutoCommit stages all changes and commits with a task-referencing message.
func (w *Workflow) AutoCommit(taskTitle, taskUUID string) error {
	defer logging.Trace("Workflow.AutoCommit",
		map[string]interface{}{"task": taskTitle})()

	msg := CommitMessage(taskTitle, taskUUID)
	if err := w.client.CommitAll(msg); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	logging.Info("Committed changes", map[string]interface{}{
		"task": taskTitle, "message": msg,
	})

	return nil
}

// AutoPush pushes the current branch and creates a PR.
// Returns the PR URL if successful.
func (w *Workflow) AutoPush(taskTitle, taskUUID string) (string, error) {
	defer logging.Trace("Workflow.AutoPush",
		map[string]interface{}{"task": taskTitle})()

	branch, err := w.AutoBranch(taskTitle, taskUUID)
	if err != nil {
		return "", err
	}

	// Commit any pending changes
	hasChanges, _ := w.client.HasChanges()
	if hasChanges {
		if err := w.AutoCommit(taskTitle, taskUUID); err != nil {
			return "", err
		}
	}

	// Push
	if err := w.client.Push(); err != nil {
		return "", fmt.Errorf("push: %w", err)
	}

	// Create PR
	prURL, err := w.client.CreatePR(
		fmt.Sprintf("[Cortex] %s", taskTitle),
		fmt.Sprintf("Automated by Cortex agent.\n\nTask: %s\nBranch: %s", taskUUID, branch),
	)
	if err != nil {
		// PR creation is optional — push succeeded
		logging.Warn("PR creation failed", map[string]interface{}{"error": err.Error()})
		return fmt.Sprintf("Pushed to %s (PR failed: %s)", branch, err), nil
	}

	logging.Info("PR created", map[string]interface{}{"url": prURL})
	return prURL, nil
}

// EnsureClean ensures we're on a clean main branch.
func (w *Workflow) EnsureClean() error {
	if err := w.client.EnsureClean(); err != nil {
		return err
	}
	current, err := w.client.CurrentBranch()
	if err != nil {
		return err
	}
	if current != "main" && current != "master" {
		return fmt.Errorf("not on main/master branch (current: %s)", current)
	}
	return nil
}

// SyncFromMain rebases the current branch onto main/master.
func (w *Workflow) SyncFromMain() error {
	current, err := w.client.CurrentBranch()
	if err != nil {
		return err
	}
	if current == "main" || current == "master" {
		return nil
	}

	mainBranch := "main"
	if err := w.client.run("fetch", "origin", mainBranch); err != nil {
		mainBranch = "master"
		if err2 := w.client.run("fetch", "origin", mainBranch); err2 != nil {
			return fmt.Errorf("fetch: %s / %s", err, err2)
		}
	}

	return w.client.run("rebase", fmt.Sprintf("origin/%s", mainBranch))
}

// TagRelease creates an annotated release tag.
func (w *Workflow) TagRelease(version, message string) error {
	if err := w.client.EnsureClean(); err != nil {
		return err
	}
	current, _ := w.client.CurrentBranch()
	if current != "main" && current != "master" {
		return fmt.Errorf("releases must be tagged from main/master")
	}
	return w.client.Tag(version, message)
}

// Changelog generates a changelog from recent commits.
func (w *Workflow) Changelog(since string) (string, error) {
	out, err := w.client.output("log", fmt.Sprintf("%s..HEAD", since), "--oneline", "--no-merges")
	return out, err
}

// LastRelease returns the most recent tag.
func (w *Workflow) LastRelease() (string, error) {
	out, err := w.client.output("describe", "--tags", "--abbrev=0")
	if err != nil {
		return "", fmt.Errorf("no tags found")
	}
	return strings.TrimSpace(out), nil
}
