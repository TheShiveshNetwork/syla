package provider

import (
	"context"
	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"strings"
	"testing"
)

func TestCopilotBuildArgs(t *testing.T) {
	schema := agent.BuildAgentOutputSchema(false, nil)
	t.Run("default has allow-all", func(t *testing.T) {
		args := BuildCopilotArgs("hi", schema, nil, "")
		has := false
		for _, a := range args {
			if a == "--allow-all" {
				has = true
			}
		}
		if !has {
			t.Fatalf("should have allow-all")
		}
	})
	t.Run("user specified", func(t *testing.T) {
		args := BuildCopilotArgs("hi", schema, []string{"--allow-all"}, "")
		count := 0
		for _, a := range args {
			if a == "--allow-all" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("should have exactly one, got %d", count)
		}
	})
	t.Run("model", func(t *testing.T) {
		args := BuildCopilotArgs("hi", schema, nil, "gpt-4")
		has := false
		for i, a := range args {
			if a == "--model" && i+1 < len(args) && args[i+1] == "gpt-4" {
				has = true
			}
		}
		if !has {
			t.Fatalf("should have model")
		}
	})
	t.Run("prompt contains schema", func(t *testing.T) {
		args := BuildCopilotArgs("hello", schema, nil, "")
		found := false
		for _, a := range args {
			if strings.Contains(a, "gnhf final output contract") {
				found = true
			}
		}
		if !found {
			t.Fatalf("should contain contract")
		}
	})
}

func TestCopilotAgentRunWithFake(t *testing.T) {
	script := `cat <<'EOF'
{"type":"assistant.message","data":{"content":"{\"success\":true,\"summary\":\"copilot done\",\"key_changes_made\":[],\"key_learnings\":[]}"}}
EOF`
	bin := writeFakeScript(t, script)
	a := NewCopilotAgent(WithCopilotBin(bin))
	a.Schema = agent.BuildAgentOutputSchema(false, nil)
	result, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
	if err != nil {
		t.Fatalf("copilot %v", err)
	}
	if result.Output.Summary != "copilot done" {
		t.Fatalf("copilot output %#v", result.Output)
	}
}

func TestCopilotAgentRunErrors(t *testing.T) {
	t.Run("no message", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"other","data":{}}
EOF`
		bin := writeFakeScript(t, script)
		a := NewCopilotAgent(WithCopilotBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "no agent message") {
			t.Fatalf("should fail, got %v", err)
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"assistant.message","data":{"content":"not json"}}
EOF`
		bin := writeFakeScript(t, script)
		a := NewCopilotAgent(WithCopilotBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "Failed to parse") {
			t.Fatalf("should fail parse, got %v", err)
		}
	})
	t.Run("non-zero exit", func(t *testing.T) {
		script := `exit 1`
		bin := writeFakeScript(t, script)
		a := NewCopilotAgent(WithCopilotBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "exited with code") {
			t.Fatalf("should be exit, got %v", err)
		}
	})
	t.Run("usage", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"assistant.message","data":{"content":"{\"success\":true,\"summary\":\"x\",\"key_changes_made\":[],\"key_learnings\":[]}","outputTokens":5}}
{"usage":{"input_tokens":10,"output_tokens":7}}
EOF`
		bin := writeFakeScript(t, script)
		a := NewCopilotAgent(WithCopilotBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		var gotUsage agent.TokenUsage
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{
			OnUsage: func(u agent.TokenUsage) { gotUsage = u },
		})
		if err != nil {
			t.Fatalf("run err %v", err)
		}
		if gotUsage.InputTokens != 10 {
			t.Fatalf("usage input %d", gotUsage.InputTokens)
		}
	})
}

func TestCopilotAbort(t *testing.T) {
	script := `sleep 0.5; echo '{"type":"assistant.message","data":{"content":"{}"}}'`
	bin := writeFakeScript(t, script)
	a := NewCopilotAgent(WithCopilotBin(bin))
	a.Schema = agent.BuildAgentOutputSchema(false, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Run(ctx, "prompt", t.TempDir(), agent.RunOptions{})
	if err == nil || err.Error() != "Agent was aborted" {
		t.Fatalf("should be aborted, got %v", err)
	}
}
