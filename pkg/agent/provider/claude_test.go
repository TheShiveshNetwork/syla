package provider

import (
	"context"
	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeBuildArgs(t *testing.T) {
	schema := agent.BuildAgentOutputSchema(false, nil)
	t.Run("model filtering", func(t *testing.T) {
		args := BuildClaudeArgs("hello", schema, []string{"--model", "old"}, "new")
		found := false
		for i, a := range args {
			if a == "--model" && i+1 < len(args) && args[i+1] == "new" {
				found = true
			}
			if a == "old" {
				t.Fatalf("old model should be filtered")
			}
		}
		if !found {
			t.Fatalf("new model not found in %v", args)
		}
	})
	t.Run("permission mode default", func(t *testing.T) {
		args := BuildClaudeArgs("hi", schema, nil, "")
		has := false
		for _, a := range args {
			if a == "--dangerously-skip-permissions" {
				has = true
			}
		}
		if !has {
			t.Fatalf("should have skip permissions")
		}
	})
	t.Run("permission mode user specified", func(t *testing.T) {
		args := BuildClaudeArgs("hi", schema, []string{"--dangerously-skip-permissions"}, "")
		count := 0
		for _, a := range args {
			if a == "--dangerously-skip-permissions" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("should have exactly one, got %d", count)
		}
	})
	t.Run("contains schema", func(t *testing.T) {
		args := BuildClaudeArgs("hello", schema, nil, "")
		found := false
		for _, a := range args {
			if strings.Contains(a, `"success"`) {
				found = true
			}
		}
		if !found {
			t.Fatalf("should contain schema json")
		}
	})
}

func TestClaudeAgentRunWithFake(t *testing.T) {
	schema := agent.BuildAgentOutputSchema(false, nil)
	script := `cat <<'EOF'
{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"working"}],"usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":1,"cache_creation_input_tokens":2}}}
{"type":"result","subtype":"success","is_error":false,"structured_output":{"success":true,"summary":"done","key_changes_made":["x"],"key_learnings":["y"]},"usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":1,"cache_creation_input_tokens":2},"total_cost_usd":0}
EOF`
	bin := writeFakeScript(t, script)
	a := NewClaudeAgent(WithClaudeBin(bin), WithClaudeSchema(schema))
	a.FinalResultGraceMs = 100
	var gotUsage agent.TokenUsage
	var gotMessages []string
	opts := agent.RunOptions{
		OnUsage:   func(u agent.TokenUsage) { gotUsage = u },
		OnMessage: func(s string) { gotMessages = append(gotMessages, s) },
		LogPath:   filepath.Join(t.TempDir(), "claude.log"),
	}
	result, err := a.Run(context.Background(), "test prompt", t.TempDir(), opts)
	if err != nil {
		t.Fatalf("run failed %v", err)
	}
	if !result.Output.Success || result.Output.Summary != "done" {
		t.Fatalf("output %#v", result.Output)
	}
	if gotUsage.InputTokens != 10 || gotUsage.OutputTokens != 20 {
		t.Fatalf("usage %#v", gotUsage)
	}
	if len(gotMessages) == 0 || gotMessages[0] != "working" {
		t.Fatalf("messages %v", gotMessages)
	}
	if result.Usage.InputTokens != 10 {
		t.Fatalf("result usage %v", result.Usage)
	}
}

func TestClaudeAgentRunErrors(t *testing.T) {
	schema := agent.BuildAgentOutputSchema(false, nil)
	t.Run("no result event", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":1}}}
EOF`
		bin := writeFakeScript(t, script)
		a := NewClaudeAgent(WithClaudeBin(bin), WithClaudeSchema(schema))
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "no result event") {
			t.Fatalf("should fail with no result, got %v", err)
		}
	})
	t.Run("error result", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"result","subtype":"error","is_error":true,"result":"boom"}
EOF`
		bin := writeFakeScript(t, script)
		a := NewClaudeAgent(WithClaudeBin(bin), WithClaudeSchema(schema))
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "reported error") {
			t.Fatalf("should report error, got %v", err)
		}
	})
	t.Run("non-zero exit", func(t *testing.T) {
		script := `echo "stderr boom" >&2; exit 2`
		bin := writeFakeScript(t, script)
		a := NewClaudeAgent(WithClaudeBin(bin), WithClaudeSchema(schema))
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "exited with code 2") {
			t.Fatalf("should be exit error, got %v", err)
		}
	})
	t.Run("rate limit", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":9999999999}}
{"type":"result","subtype":"success","is_error":false,"structured_output":{"success":true,"summary":"x","key_changes_made":[],"key_learnings":[]}}
EOF
echo "exit 1" >&2; exit 1`
		bin := writeFakeScript(t, script)
		a := NewClaudeAgent(WithClaudeBin(bin), WithClaudeSchema(schema))
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "usage limit") {
			t.Fatalf("should be rate limit, got %v", err)
		}
		var rl *agent.RateLimitAgentError
		if !agent.IsRateLimit(err) || rl == nil {
			// check type via errors.As
			if !agent.IsRateLimit(err) {
				t.Fatalf("should be agent.RateLimitAgentError")
			}
		}
	})
	t.Run("overage callback", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"rate_limit_event","rate_limit_info":{"isUsingOverage":true,"resetsAt":9999999999}}
{"type":"result","subtype":"success","is_error":false,"structured_output":{"success":true,"summary":"x","key_changes_made":[],"key_learnings":[]}}
EOF`
		bin := writeFakeScript(t, script)
		a := NewClaudeAgent(WithClaudeBin(bin), WithClaudeSchema(schema))
		var gotOverage *agent.UsageOverage
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{
			OnOverage: func(o *agent.UsageOverage) { gotOverage = o },
		})
		if err != nil {
			t.Fatalf("should succeed, got %v", err)
		}
		if gotOverage == nil || gotOverage.ResumeAt == nil {
			t.Fatalf("should have overage, got %v", gotOverage)
		}
	})
}

func TestClaudeAbort(t *testing.T) {
	schema := agent.BuildAgentOutputSchema(false, nil)
	script := `sleep 0.5; echo '{"type":"result","subtype":"success","is_error":false,"structured_output":{"success":true,"summary":"late","key_changes_made":[],"key_learnings":[]}}'`
	bin := writeFakeScript(t, script)
	a := NewClaudeAgent(WithClaudeBin(bin), WithClaudeSchema(schema))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Run(ctx, "prompt", t.TempDir(), agent.RunOptions{})
	if err == nil || err.Error() != "Agent was aborted" {
		t.Fatalf("should be aborted, got %v", err)
	}
}

func TestClaudeTokenUsage(t *testing.T) {
	m := map[string]any{"input_tokens": float64(5), "output_tokens": float64(10), "cache_read_input_tokens": float64(2), "cache_creation_input_tokens": float64(3)}
	u := toTokenUsageClaude(m)
	if u.InputTokens != 5 || u.OutputTokens != 10 || u.CacheReadTokens != 2 || u.CacheCreationTokens != 3 {
		t.Fatalf("wrong usage %v", u)
	}
}
