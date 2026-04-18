package maintenance

import (
	"context"
	"fmt"
	"os"
	"syscall"
)

// TaskStatus is the view of a single maintenance task for the debug API.
type TaskStatus struct {
	ID        string       `json:"id"`
	Category  TaskCategory `json:"category"`
	Applied   bool         `json:"applied"`
	RunCount  int          `json:"run_count"`
	AppliedAt int64        `json:"applied_at,omitempty"`
}

// TaskRunner executes registered maintenance tasks.
type TaskRunner struct {
	store    *TaskStore
	registry *Registry
}

// NewTaskRunner creates a TaskRunner.
func NewTaskRunner(store *TaskStore, registry *Registry) *TaskRunner {

	return &TaskRunner{store: store, registry: registry}
}

// RunOneShotMigrations executes pending one-shot protocol migrations (CategoryProtocol).
// It restarts the process after each successful migration to ensure clean state.
func (r *TaskRunner) RunOneShotMigrations(ctx context.Context) error {
	for _, t := range r.registry.tasks {
		if t.Category() != CategoryProtocol {
			continue
		}
		done, err := r.store.IsApplied(t.ID())
		if err != nil {
			return err
		}
		if done {
			continue
		}

		fmt.Printf("🔄 [Maintenance] Running one-shot migration %q …\n", t.ID())
		if err := t.Run(ctx); err != nil {
			return fmt.Errorf("migration %q failed: %w", t.ID(), err)
		}
		if err := r.store.MarkApplied(t.ID()); err != nil {
			return fmt.Errorf("migration %q: marking applied: %w", t.ID(), err)
		}
		fmt.Printf("✅ [Maintenance] %q applied; restarting process …\n", t.ID())
		return restart()
	}
	return nil
}

// RunCommunityTasks executes community maintenance tasks that the client has opted into.
func (r *TaskRunner) RunCommunityTasks(ctx context.Context, categories []TaskCategory) error {
	catMap := make(map[TaskCategory]bool)
	for _, c := range categories {
		catMap[c] = true
	}

	for _, t := range r.registry.tasks {
		if !catMap[t.Category()] {
			continue
		}
		// Community tasks might be periodic or conditional; 
		// for now we just run them during boot if opted-in.
		fmt.Printf("🔧 [Maintenance] Executing community task %q (%s) …\n", t.ID(), t.Category())
		if err := t.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  Task %q failed: %v\n", t.ID(), err)
		}
	}
	return nil
}

func restart() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("restart: cannot find executable: %w", err)
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
