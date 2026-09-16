package provider

import (
	"context"
	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"strings"
	"testing"
)

func TestRovodevBuildArgs(t *testing.T) {
	schema := agent.BuildAgentOutputSchema(false, nil)
	args := BuildRovoDevArgs("hello", schema, nil)
	if len(args) == 0 || args[0] != "rovodev" {
		t.Fatalf("should start with rovodev")
	}
	found := false
	for _, a := range args {
		if strings.Contains(a, "hello") {
			found = true
		}
	}
	if !found {
		t.Fatalf("should contain prompt")
	}
	// with extra args
	args2 := BuildRovoDevArgs("hi", schema, []string{"--extra"})
	hasExtra := false
	for _, a := range args2 {
		if a == "--extra" {
			hasExtra = true
		}
	}
	if !hasExtra {
		t.Fatalf("should have extra")
	}
}

func TestRovoDevAgentRunWithFake(t *testing.T) {
	script := `cat <<'EOF'
{"event_kind":"text","content":"{\"success\":true,\"summary\":\"rovodev done\",\"key_changes_made\":[],\"key_learnings\":[]}"}
EOF`
	bin := writeFakeScript(t, script)
	a := NewRovoDevAgent("/tmp/schema.json", WithRovoDevBin(bin))
	a.Schema = agent.BuildAgentOutputSchema(false, nil)
	result, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
	if err != nil {
		t.Fatalf("rovodev %v", err)
	}
	if result.Output.Summary != "rovodev done" {
		t.Fatalf("rovodev output %#v", result.Output)
	}
}

func TestRovodevAgentRunErrors(t *testing.T) {
	t.Run("no output", func(t *testing.T) {
		script := `echo ""`
		bin := writeFakeScript(t, script)
		a := NewRovoDevAgent("/tmp/schema.json", WithRovoDevBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "no text output") {
			t.Fatalf("should fail, got %v", err)
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		script := `echo '{"event_kind":"text","content":"not json"}'`
		bin := writeFakeScript(t, script)
		a := NewRovoDevAgent("/tmp/schema.json", WithRovoDevBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "Failed to parse") {
			t.Fatalf("should fail parse, got %v", err)
		}
	})
	t.Run("non-zero exit", func(t *testing.T) {
		script := `exit 1`
		bin := writeFakeScript(t, script)
		a := NewRovoDevAgent("/tmp/schema.json", WithRovoDevBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "exited with code") {
			t.Fatalf("should be exit, got %v", err)
		}
	})
	t.Run("usage", func(t *testing.T) {
		script := `cat <<'EOF'
{"input_tokens":1,"output_tokens":2,"cache_read_tokens":3,"cache_write_tokens":4}
{"event_kind":"text","content":"{\"success\":true,\"summary\":\"x\",\"key_changes_made\":[],\"key_learnings\":[]}"}
EOF`
		bin := writeFakeScript(t, script)
		a := NewRovoDevAgent("/tmp/schema.json", WithRovoDevBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		var gotUsage agent.TokenUsage
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{
			OnUsage: func(u agent.TokenUsage) { gotUsage = u },
		})
		if err != nil {
			t.Fatalf("run err %v", err)
		}
		if gotUsage.InputTokens != 1 {
			t.Fatalf("usage %v", gotUsage)
		}
	})
}

func TestRovodevAbort(t *testing.T) {
	script := `sleep 0.5; echo '{"event_kind":"text","content":"{}"}'`
	bin := writeFakeScript(t, script)
	a := NewRovoDevAgent("/tmp/schema.json", WithRovoDevBin(bin))
	a.Schema = agent.BuildAgentOutputSchema(false, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Run(ctx, "prompt", t.TempDir(), agent.RunOptions{})
	if err == nil || err.Error() != "Agent was aborted" {
		t.Fatalf("should be aborted, got %v", err)
	}
}

func TestRovodevClose(t *testing.T) {
	a := NewRovoDevAgent("/tmp/schema.json")
	if err := a.Close(); err != nil {
		t.Fatalf("close err %v", err)
	}
}
