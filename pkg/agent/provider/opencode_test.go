package provider

import (
	"context"
	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"strings"
	"testing"
)

func TestOpencodeBuildArgs(t *testing.T) {
	schema := agent.BuildAgentOutputSchema(false, nil)
	t.Run("default", func(t *testing.T) {
		args := BuildOpenCodeArgs("hello", schema, nil, "")
		if len(args) == 0 || args[0] != "run" {
			t.Fatalf("should start with run")
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
	})
	t.Run("model", func(t *testing.T) {
		args := BuildOpenCodeArgs("hi", schema, nil, "provider/model")
		has := false
		for i, a := range args {
			if a == "--model" && i+1 < len(args) && args[i+1] == "provider/model" {
				has = true
			}
		}
		if !has {
			t.Fatalf("should have model")
		}
	})
}

func TestOpencodeBuildPrompt(t *testing.T) {
	schema := agent.BuildAgentOutputSchema(false, nil)
	prompt := BuildOpenCodePrompt("hello", schema)
	if !strings.Contains(prompt, "only valid JSON") {
		t.Fatalf("should contain JSON instruction")
	}
}

func TestOpenCodeAgentRunWithFake(t *testing.T) {
	script := `echo '{"success":true,"summary":"opencode done","key_changes_made":[],"key_learnings":[]}'`
	bin := writeFakeScript(t, script)
	a := NewOpenCodeAgent(WithOpenCodeBin(bin))
	a.Schema = agent.BuildAgentOutputSchema(false, nil)
	result, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
	if err != nil {
		t.Fatalf("opencode %v", err)
	}
	if result.Output.Summary != "opencode done" {
		t.Fatalf("opencode output %#v", result.Output)
	}
}

func TestOpencodeAgentRunErrors(t *testing.T) {
	t.Run("no output", func(t *testing.T) {
		script := `echo ""`
		bin := writeFakeScript(t, script)
		a := NewOpenCodeAgent(WithOpenCodeBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "no final answer") {
			t.Fatalf("should fail, got %v", err)
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		script := `echo "not json"`
		bin := writeFakeScript(t, script)
		a := NewOpenCodeAgent(WithOpenCodeBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "Failed to parse") {
			t.Fatalf("should fail parse, got %v", err)
		}
	})
	t.Run("non-zero exit", func(t *testing.T) {
		script := `exit 1`
		bin := writeFakeScript(t, script)
		a := NewOpenCodeAgent(WithOpenCodeBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "exited with code") {
			t.Fatalf("should be exit, got %v", err)
		}
	})
	t.Run("with tokens", func(t *testing.T) {
		script := `cat <<'EOF'
{"tokens":{"input":1,"output":2,"cache_read":3,"cache_write":4}}
{"text":"{\"success\":true,\"summary\":\"x\",\"key_changes_made\":[],\"key_learnings\":[]}"}
EOF`
		bin := writeFakeScript(t, script)
		a := NewOpenCodeAgent(WithOpenCodeBin(bin))
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

func TestOpencodeAbort(t *testing.T) {
	script := `sleep 0.5; echo '{}'`
	bin := writeFakeScript(t, script)
	a := NewOpenCodeAgent(WithOpenCodeBin(bin))
	a.Schema = agent.BuildAgentOutputSchema(false, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Run(ctx, "prompt", t.TempDir(), agent.RunOptions{})
	if err == nil || err.Error() != "Agent was aborted" {
		t.Fatalf("should be aborted, got %v", err)
	}
}

func TestOpencodeClose(t *testing.T) {
	a := NewOpenCodeAgent()
	if err := a.Close(); err != nil {
		t.Fatalf("close err %v", err)
	}
}
