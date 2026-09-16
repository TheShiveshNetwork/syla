package provider

import (
	"context"
	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"strings"
	"testing"
)

func TestPiBuildArgs(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		args := BuildPiArgs(nil, "")
		hasMode := false
		hasNoSession := false
		for i, a := range args {
			if a == "--mode" && i+1 < len(args) && args[i+1] == "json" {
				hasMode = true
			}
			if a == "--no-session" {
				hasNoSession = true
			}
		}
		if !hasMode || !hasNoSession {
			t.Fatalf("should have mode and no-session %v", args)
		}
	})
	t.Run("model", func(t *testing.T) {
		args := BuildPiArgs(nil, "pi-model")
		has := false
		for i, a := range args {
			if a == "--model" && i+1 < len(args) && args[i+1] == "pi-model" {
				has = true
			}
		}
		if !has {
			t.Fatalf("should have model")
		}
	})
}

func TestPiBuildPrompt(t *testing.T) {
	schema := agent.BuildAgentOutputSchema(false, nil)
	prompt := BuildPiPrompt("hello", schema)
	if !strings.Contains(prompt, "gnhf final output contract") {
		t.Fatalf("should contain contract")
	}
}

func TestPiAgentRunWithFake(t *testing.T) {
	script := `cat <<'EOF'
{"type":"message_update","message":{"role":"assistant","content":[{"type":"text","text":"pi working"}]}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"{\"success\":"}}
{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"true,\"summary\":\"pi done\",\"key_changes_made\":[],\"key_learnings\":[]}"}}
{"type":"message_update","assistantMessageEvent":{"type":"text_end","contentIndex":0,"text":"{\"success\":true,\"summary\":\"pi done\",\"key_changes_made\":[],\"key_learnings\":[]}"}}
{"type":"message_end","message":{"role":"assistant","text":"{\"success\":true,\"summary\":\"pi done\",\"key_changes_made\":[],\"key_learnings\":[]}"}}
EOF`
	bin := writeFakeScript(t, script)
	a := NewPiAgent(WithPiBin(bin))
	a.Schema = agent.BuildAgentOutputSchema(false, nil)
	result, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
	if err != nil {
		t.Fatalf("pi %v", err)
	}
	if result.Output.Summary != "pi done" {
		t.Fatalf("pi output %#v", result.Output)
	}
}

func TestPiAgentRunErrors(t *testing.T) {
	t.Run("no output", func(t *testing.T) {
		script := `echo '{"type":"message_update","message":{"role":"assistant","content":[]}}'`
		bin := writeFakeScript(t, script)
		a := NewPiAgent(WithPiBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "no text output") {
			t.Fatalf("should fail, got %v", err)
		}
	})
	t.Run("error stopReason", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"message_end","message":{"role":"assistant","stopReason":"error","errorMessage":"boom"}}
EOF`
		bin := writeFakeScript(t, script)
		a := NewPiAgent(WithPiBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "pi reported error") {
			t.Fatalf("should be pi error, got %v", err)
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"message_end","message":{"role":"assistant","text":"not json"}}
EOF`
		bin := writeFakeScript(t, script)
		a := NewPiAgent(WithPiBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "Failed to parse") {
			t.Fatalf("should fail parse, got %v", err)
		}
	})
	t.Run("non-zero exit", func(t *testing.T) {
		script := `exit 1`
		bin := writeFakeScript(t, script)
		a := NewPiAgent(WithPiBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "exited with code") {
			t.Fatalf("should be exit, got %v", err)
		}
	})
	t.Run("usage and message callbacks", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"message_update","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"usage":{"input":1,"output":2,"cacheRead":3,"cacheWrite":4}},"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"hello"}}
{"type":"message_end","message":{"role":"assistant","text":"{\"success\":true,\"summary\":\"x\",\"key_changes_made\":[],\"key_learnings\":[]}","usage":{"input":1,"output":2}}}
EOF`
		bin := writeFakeScript(t, script)
		a := NewPiAgent(WithPiBin(bin))
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
		if gotUsage.InputTokens == 0 {
			t.Fatalf("should have usage")
		}
		if gotMsg == "" {
			t.Fatalf("should have message")
		}
	})
}

func TestPiAbort(t *testing.T) {
	script := `sleep 0.5; echo '{"type":"message_end","message":{"role":"assistant","text":"{}"}}'`
	bin := writeFakeScript(t, script)
	a := NewPiAgent(WithPiBin(bin))
	a.Schema = agent.BuildAgentOutputSchema(false, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Run(ctx, "prompt", t.TempDir(), agent.RunOptions{})
	if err == nil || err.Error() != "Agent was aborted" {
		t.Fatalf("should be aborted, got %v", err)
	}
}
