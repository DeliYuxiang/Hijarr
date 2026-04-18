package maintenance

import "context"

// TaskCategory defines the type of community maintenance work.
type TaskCategory string

const (
	CategoryProtocol TaskCategory = "protocol" // One-shot protocol upgrades (e.g. re-signing)
	CategoryCleanup  TaskCategory = "cleanup"  // SRN network cleanup (Kind 1002/1003 handling)
	CategoryStats    TaskCategory = "stats"    // Statistical event integration
)


// Task is a maintenance job that can be领取 and processed by the client.
// One-shot tasks (CategoryProtocol) run exactly once per install.
// Periodic community tasks (CategoryCleanup) may be opted-in by the client.
type Task interface {
	ID() string
	Category() TaskCategory
	Run(ctx context.Context) error
}

// Registry holds registered maintenance tasks.
type Registry struct {
	tasks []Task
	seen  map[string]struct{}
}

// Register appends a task. Panics on duplicate ID.
func (r *Registry) Register(t Task) {
	if r.seen == nil {
		r.seen = make(map[string]struct{})
	}
	if _, dup := r.seen[t.ID()]; dup {
		panic("maintenance: duplicate task ID " + t.ID())
	}
	r.seen[t.ID()] = struct{}{}
	r.tasks = append(r.tasks, t)
}

// All returns every registered task.
func (r *Registry) All() []Task {
	return r.tasks
}
