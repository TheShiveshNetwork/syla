package provider

import (
	"context"
	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"strings"
	"testing"
)

func TestCodexBuildArgs(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		args := BuildCodexArgs("prompt", "/tmp/schema.json", nil, "")
		if len(args) == 0 || args[0] != "exec" {
			t.Fatalf("should start with exec")
		}
		hasBypass := false
		for _, a := range args {
			if a == "--dangerously-bypass-approvals-and-sandbox" {
				hasBypass = true
			}
		}
		if !hasBypass {
			t.Fatalf("should have bypass")
		}
	})
	t.Run("user specified mode", func(t *testing.T) {
		args := BuildCodexArgs("prompt", "/tmp/schema.json", []string{"--full-auto"}, "")
		count := 0
		for _, a := range args {
			if a == "--dangerously-bypass-approvals-and-sandbox" {
				count++
			}
		}
		if count != 0 {
			t.Fatalf("should not have bypass when user specified, got %v", args)
		}
	})
	t.Run("model", func(t *testing.T) {
		args := BuildCodexArgs("prompt", "/tmp/schema.json", nil, "gpt-4")
		hasModel := false
		for i, a := range args {
			if a == "--model" && i+1 < len(args) && args[i+1] == "gpt-4" {
				hasModel = true
			}
		}
		if !hasModel {
			t.Fatalf("should have model")
		}
	})
}

func TestCodexAgentRunWithFake(t *testing.T) {
	script := `cat <<'EOF'
{"type":"item.completed","item":{"type":"agent_message","text":"{\"success\":true,\"summary\":\"codex done\",\"key_changes_made\":[],\"key_learnings\":[]}"}}
{"type":"turn.completed","usage":{"input_tokens":5,"cached_input_tokens":1,"output_tokens":10}}
EOF`
	bin := writeFakeScript(t, script)
	a := NewCodexAgent("/tmp/schema.json", WithCodexBin(bin))
	a.Schema = agent.BuildAgentOutputSchema(false, nil)
	result, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
	if err != nil {
		t.Fatalf("codex run %v", err)
	}
	if !result.Output.Success || result.Output.Summary != "codex done" {
		t.Fatalf("output %#v", result.Output)
	}
	if result.Usage.InputTokens != 4 {
		t.Fatalf("usage %v", result.Usage)
	}
}

func TestCodexAgentRunErrors(t *testing.T) {
	t.Run("no message", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"turn.completed","usage":{"input_tokens":1,"cached_input_tokens":0,"output_tokens":1}}
EOF`
		bin := writeFakeScript(t, script)
		a := NewCodexAgent("/tmp/schema.json", WithCodexBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "no agent message") {
			t.Fatalf("should fail no message, got %v", err)
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"item.completed","item":{"type":"agent_message","text":"not json"}}
EOF`
		bin := writeFakeScript(t, script)
		a := NewCodexAgent("/tmp/schema.json", WithCodexBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "Failed to parse") {
			t.Fatalf("should fail parse, got %v", err)
		}
	})
	t.Run("non-zero exit", func(t *testing.T) {
		script := `echo "fail" >&2; exit 1`
		bin := writeFakeScript(t, script)
		a := NewCodexAgent("/tmp/schema.json", WithCodexBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "exited with code 1") {
			t.Fatalf("should be exit error, got %v", err)
		}
	})
	t.Run("usage and message callbacks", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"item.completed","item":{"type":"agent_message","text":"{\"success\":true,\"summary\":\"x\",\"key_changes_made\":[],\"key_learnings\":[]}"}}
{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":2,"output_tokens":5}}
EOF`
		bin := writeFakeScript(t, script)
		a := NewCodexAgent("/tmp/schema.json", WithCodexBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		var gotUsage agent.TokenUsage
		var gotMsg string
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{
			OnUsage:   func(u agent.TokenUsage) { gotUsage = u },
			OnMessage: func(s string) { gotMsg = s },
		})
		if err != nil {
			t.Fatalf("run err %v", err)
		}
		if gotUsage.InputTokens != 8 {
			t.Fatalf("usage input %d", gotUsage.InputTokens)
		}
		if gotMsg == "" {
			t.Fatalf("should have message")
		}
	})
}

func TestCodexAbort(t *testing.T) {
	script := `sleep 0.5; echo '{"type":"item.completed","item":{"type":"agent_message","text":"{}"}}'`
	bin := writeFakeScript(t, script)
	a := NewCodexAgent("/tmp/schema.json", WithCodexBin(bin))
	a.Schema = agent.BuildAgentOutputSchema(false, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Run(ctx, "prompt", t.TempDir(), agent.RunOptions{})
	if err == nil || err.Error() != "Agent was aborted" {
		t.Fatalf("should be aborted, got %v", err)
	}
}
