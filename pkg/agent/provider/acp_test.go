package provider

import (
	"context"
	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"strings"
	"testing"
)

func TestAcpIsNamedTarget(t *testing.T) {
	if !IsNamedAcpTarget("gemini") {
		t.Fatalf("should be named")
	}
	if IsNamedAcpTarget("custom command") {
		t.Fatalf("should not be named with space")
	}
	if IsNamedAcpTarget("") {
		t.Fatalf("empty should not be named")
	}
	if IsNamedAcpTarget(".hidden") {
		t.Fatalf("dot start should not be named")
	}
	if !IsNamedAcpTarget("a-b_c.d:e") {
		t.Fatalf("should be named with allowed chars")
	}
}

func TestAcpRedact(t *testing.T) {
	if got := RedactAcpTargetForLogs("gemini"); got != "gemini" {
		t.Fatalf("should not redact named, got %q", got)
	}
	if got := RedactAcpTargetForLogs("custom with spaces"); got != "custom" {
		t.Fatalf("should redact custom, got %q", got)
	}
}

func TestAcpAgentName(t *testing.T) {
	a := NewAcpAgent("gemini", agent.BuildAgentOutputSchema(false, nil), "run1", "/tmp/sessions", nil)
	if a.Name() != "acp:gemini" {
		t.Fatalf("name %q", a.Name())
	}
}

func TestAcpAgentRunWithFake(t *testing.T) {
	a := NewAcpAgent("test", agent.BuildAgentOutputSchema(false, nil), "run1", "/tmp/sessions", nil)
	var gotUsage agent.TokenUsage
	var gotMsg string
	result, err := a.Run(context.Background(), "hello acp", t.TempDir(), agent.RunOptions{
		OnUsage:   func(u agent.TokenUsage) { gotUsage = u },
		OnMessage: func(s string) { gotMsg = s },
	})
	if err != nil {
		t.Fatalf("acp test %v", err)
	}
	if !result.Output.Success {
		t.Fatalf("acp should succeed")
	}
	if !gotUsage.Estimated {
		t.Fatalf("acp usage should be estimated")
	}
	if gotMsg == "" {
		t.Fatalf("should have message")
	}
	if result.Output.Summary != "acp test success" {
		t.Fatalf("summary %q", result.Output.Summary)
	}
}

func TestAcpAgentRunErrors(t *testing.T) {
	t.Run("unknown target", func(t *testing.T) {
		a := NewAcpAgent("unknown-target-xyz", agent.BuildAgentOutputSchema(false, nil), "run1", "/tmp/sessions", nil)
		_, err := a.Run(context.Background(), "hi", t.TempDir(), agent.RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "requires acpx runtime") {
			t.Fatalf("should error, got %v", err)
		}
	})
	t.Run("abort", func(t *testing.T) {
		a := NewAcpAgent("test", agent.BuildAgentOutputSchema(false, nil), "run1", "/tmp/sessions", nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := a.Run(ctx, "hi", t.TempDir(), agent.RunOptions{})
		if err == nil || err.Error() != "Agent was aborted" {
			t.Fatalf("should be aborted, got %v", err)
		}
	})
	t.Run("echo target", func(t *testing.T) {
		a := NewAcpAgent("echo", agent.BuildAgentOutputSchema(false, nil), "run1", "/tmp/sessions", nil)
		result, err := a.Run(context.Background(), "hello", t.TempDir(), agent.RunOptions{})
		if err != nil {
			t.Fatalf("echo should succeed, got %v", err)
		}
		if !result.Output.Success {
			t.Fatalf("should succeed")
		}
	})
}

func TestAcpClose(t *testing.T) {
	a := NewAcpAgent("test", agent.BuildAgentOutputSchema(false, nil), "run1", "/tmp/sessions", nil)
	if err := a.Close(); err != nil {
		t.Fatalf("close err %v", err)
	}
}

func TestEstimateTokens(t *testing.T) {
	if estimateTokens(0) != 0 {
		t.Fatalf("0 should be 0")
	}
	if estimateTokens(4) != 1 {
		t.Fatalf("4 should be 1")
	}
	if estimateTokens(5) != 2 {
		t.Fatalf("5 should be 2")
	}
}
