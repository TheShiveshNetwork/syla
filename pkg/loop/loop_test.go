package loop

import (
	"context"
	"testing"
	"time"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"github.com/TheShiveshNetwork/syla/pkg/workspace"
)

type fakeAgent struct {
	output string
	usage  agent.TokenUsage
}

func (f *fakeAgent) Name() string { return "fake" }
func (f *fakeAgent) Run(ctx context.Context, prompt string, cwd string, opts agent.RunOptions) (agent.AgentResult, error) {
	return agent.AgentResult{
		Output: agent.AgentOutput{Success: true, Summary: f.output},
		Usage:  f.usage,
	}, nil
}

func TestBudget(t *testing.T) {
	b := &Budget{MaxTokens: 10, MaxUSD: 5, MaxDuration: time.Second, Start: time.Now()}
	b.AddTokens(5)
	if b.ExceededTokens() {
		t.Fatal("should not exceed")
	}
	b.AddTokens(6)
	if !b.ExceededTokens() {
		t.Fatal("should exceed")
	}
	b.AddCost(3)
	if b.ExceededUSD() {
		t.Fatal("should not exceed usd")
	}
	b.AddCost(3)
	if !b.ExceededUSD() {
		t.Fatal("should exceed usd")
	}
}

func TestStopConditions(t *testing.T) {
	ctx := context.Background()
	s := &RunState{Iteration: 5, Elapsed: 5 * time.Second, LastOutput: "DONE", WorkspaceDiff: ""}

	if c := MaxIterations(3); c != nil {
		if ok, _, _ := c.ShouldStop(ctx, s); !ok {
			t.Fatal("should stop max iterations")
		}
	}
	if c := MaxIterations(10); c != nil {
		if ok, _, _ := c.ShouldStop(ctx, s); ok {
			t.Fatal("should not stop")
		}
	}
	if c := MaxDuration(time.Second); c != nil {
		s.Elapsed = 2 * time.Second
		if ok, _, _ := c.ShouldStop(ctx, s); !ok {
			t.Fatal("should stop duration")
		}
	}
	if c := NoDiffFor(2); c != nil {
		// first no diff
		ok, _, _ := c.ShouldStop(ctx, &RunState{WorkspaceDiff: ""})
		if ok {
			t.Fatal("should not stop at 1")
		}
		ok, _, _ = c.ShouldStop(ctx, &RunState{WorkspaceDiff: ""})
		if !ok {
			t.Fatal("should stop at 2")
		}
	}
	if c, _ := OutputMatches("DONE"); c != nil {
		if ok, _, _ := c.ShouldStop(ctx, s); !ok {
			t.Fatal("should match output")
		}
	}
}

func TestEngineShouldContinue(t *testing.T) {
	ws, _ := workspace.Get("none", t.TempDir())
	eng, _ := New(Config{
		Agent:     &fakeAgent{output: "hi"},
		Workspace: ws,
		RunID:     "test",
		WorkDir:   t.TempDir(),
		MaxIterations: 2,
	})
	if !eng.ShouldContinue(context.Background()) {
		t.Fatal("should continue")
	}
	eng.state.Iteration = 2
	if eng.ShouldContinue(context.Background()) {
		t.Fatal("should not continue at max")
	}
}

func TestEngineStepAndBudget(t *testing.T) {
	ws, _ := workspace.Get("none", t.TempDir())
	fake := &fakeAgent{output: "done", usage: agent.TokenUsage{InputTokens: 5, OutputTokens: 5}}
	eng, _ := New(Config{
		Agent:     fake,
		Workspace: ws,
		WorkDir:   t.TempDir(),
		RunID:     "test-budget",
		MaxTokens: 15,
	})
	defer eng.Close()
	ctx := context.Background()
	_, err := eng.Step(ctx, "prompt")
	if err != nil {
		t.Fatalf("step: %v", err)
	}
	// next step should exceed budget
	_, _ = eng.Step(ctx, "prompt2")
	if eng.ShouldContinue(ctx) {
		t.Fatal("should not continue after budget")
	}
}
