package provider

import (
	"context"
	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"strings"
	"testing"
)

func TestCursorBuildArgs(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		args := BuildCursorArgs(nil, "")
		hasForce, hasTrust, hasApprove := false, false, false
		for _, a := range args {
			if a == "--force" {
				hasForce = true
			}
			if a == "--trust" {
				hasTrust = true
			}
			if a == "--approve-mcps" {
				hasApprove = true
			}
		}
		if !hasForce || !hasTrust || !hasApprove {
			t.Fatalf("should have defaults %v", args)
		}
	})
	t.Run("user specified", func(t *testing.T) {
		args := BuildCursorArgs([]string{"--force", "--trust"}, "")
		countForce := 0
		for _, a := range args {
			if a == "--force" {
				countForce++
			}
		}
		if countForce != 1 {
			t.Fatalf("should have one force, got %v", args)
		}
	})
	t.Run("model", func(t *testing.T) {
		args := BuildCursorArgs(nil, "cursor-model")
		has := false
		for i, a := range args {
			if a == "--model" && i+1 < len(args) && args[i+1] == "cursor-model" {
				has = true
			}
		}
		if !has {
			t.Fatalf("should have model")
		}
	})
}

func TestCursorBuildPrompt(t *testing.T) {
	schema := agent.BuildAgentOutputSchema(false, nil)
	prompt := BuildCursorPrompt("hello", schema)
	if !strings.Contains(prompt, "gnhf final output contract") {
		t.Fatalf("should contain contract")
	}
	if !strings.Contains(prompt, "hello") {
		t.Fatalf("should contain prompt")
	}
}

func TestCursorAgentRunWithFake(t *testing.T) {
	script := `cat <<'EOF'
{"type":"assistant","message":{"content":[{"type":"text","text":"{\"success\":true,\"summary\":\"cursor done\",\"key_changes_made\":[],\"key_learnings\":[]}"}]}}
{"type":"result","subtype":"success","is_error":false,"result":"{\"success\":true,\"summary\":\"cursor done\",\"key_changes_made\":[],\"key_learnings\":[]}"}
EOF`
	bin := writeFakeScript(t, script)
	a := NewCursorAgent(WithCursorBin(bin))
	a.Schema = agent.BuildAgentOutputSchema(false, nil)
	a.FinalResultGraceMs = 10
	result, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
	if err != nil {
		t.Fatalf("cursor %v", err)
	}
	if result.Output.Summary != "cursor done" {
		t.Fatalf("cursor output %#v", result.Output)
	}
}

func TestCursorAgentRunErrors(t *testing.T) {
	t.Run("no output", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"assistant","message":{"content":[]}}
EOF`
		bin := writeFakeScript(t, script)
		a := NewCursorAgent(WithCursorBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "no text output") {
			t.Fatalf("should fail, got %v", err)
		}
	})
	t.Run("error result", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"result","subtype":"error","is_error":true,"result":"boom"}
EOF`
		bin := writeFakeScript(t, script)
		a := NewCursorAgent(WithCursorBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("should contain boom, got %v", err)
		}
	})
	t.Run("permanent auth error", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"result","subtype":"error","is_error":true,"result":"authentication required"}
EOF`
		bin := writeFakeScript(t, script)
		a := NewCursorAgent(WithCursorBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !agent.IsPermanent(err) {
			t.Fatalf("should be permanent, got %v", err)
		}
	})
	t.Run("non-zero exit", func(t *testing.T) {
		script := `echo "fail" >&2; exit 1`
		bin := writeFakeScript(t, script)
		a := NewCursorAgent(WithCursorBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "exited with code") {
			t.Fatalf("should be exit, got %v", err)
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		script := `cat <<'EOF'
{"type":"assistant","message":{"content":[{"type":"text","text":"not json"}]}}
{"type":"result","subtype":"success","is_error":false,"result":"not json"}
EOF`
		bin := writeFakeScript(t, script)
		a := NewCursorAgent(WithCursorBin(bin))
		a.Schema = agent.BuildAgentOutputSchema(false, nil)
		_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "Failed to parse") {
			t.Fatalf("should fail parse, got %v", err)
		}
	})
}

func TestCursorAbort(t *testing.T) {
	script := `sleep 0.5; echo '{"type":"result","subtype":"success","is_error":false,"result":"{}"}'`
	bin := writeFakeScript(t, script)
	a := NewCursorAgent(WithCursorBin(bin))
	a.Schema = agent.BuildAgentOutputSchema(false, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Run(ctx, "prompt", t.TempDir(), agent.RunOptions{})
	if err == nil || err.Error() != "Agent was aborted" {
		t.Fatalf("should be aborted, got %v", err)
	}
}
