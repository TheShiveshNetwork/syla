package tui

import (
	"context"
	"testing"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"github.com/TheShiveshNetwork/syla/pkg/loop"
	"github.com/TheShiveshNetwork/syla/pkg/workspace"
)

func TestNewModel(t *testing.T) {
	ws, _ := workspace.Get("none", t.TempDir())
	eng, _ := loop.New(loop.Config{
		Agent:     &fakeAgent{},
		Workspace: ws,
		RunID:     "test-tui",
	})
	defer eng.Close()
	m := New(eng)
	if m.engine != eng {
		t.Fatalf("engine mismatch")
	}
	if m.viewport.Width == 0 {
		// viewport is initialized with 80, but okay
	}
}

type fakeAgent struct{}

func (f *fakeAgent) Name() string { return "fake" }
func (f *fakeAgent) Run(ctx context.Context, prompt string, cwd string, opts agent.RunOptions) (agent.AgentResult, error) {
	return agent.AgentResult{}, nil
}
