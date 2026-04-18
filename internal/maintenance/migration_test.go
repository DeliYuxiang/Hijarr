package maintenance

import (
	"context"
	"testing"
)

type mockTask struct {
	id  string
	run func() error
}

func (m *mockTask) ID() string                       { return m.id }
func (m *mockTask) Category() TaskCategory           { return CategoryProtocol }
func (m *mockTask) Run(ctx context.Context) error { return m.run() }

func TestRegistry(t *testing.T) {
	r := &Registry{}
	task := &mockTask{id: "t1"}
	r.Register(task)

	if len(r.All()) != 1 {
		t.Errorf("expected 1 task, got %d", len(r.All()))
	}
}
