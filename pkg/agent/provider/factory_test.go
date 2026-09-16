package provider

import (
	"testing"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
)

func TestCreateAgent(t *testing.T) {
	runInfo := RunInfo{RunID: "test-run", RunDir: "/tmp/run", SchemaPath: "/tmp/schema.json"}

	t.Run("claude", func(t *testing.T) {
		a, err := CreateAgent("claude", runInfo, nil, nil, CreateAgentOptions{IncludeStopField: false})
		if err != nil {
			t.Fatalf("err %v", err)
		}
		if a.Name() != "claude" {
			t.Fatalf("name %q", a.Name())
		}
	})
	t.Run("codex", func(t *testing.T) {
		a, _ := CreateAgent("codex", runInfo, nil, nil, CreateAgentOptions{})
		if a.Name() != "codex" {
			t.Fatalf("codex name %q", a.Name())
		}
	})
	t.Run("acp gemini", func(t *testing.T) {
		a, _ := CreateAgent("acp:gemini", runInfo, nil, nil, CreateAgentOptions{})
		if a.Name() != "acp:gemini" {
			t.Fatalf("acp name %q", a.Name())
		}
	})
	t.Run("acp custom", func(t *testing.T) {
		a, _ := CreateAgent("acp:custom --flag", runInfo, stringPtr("/custom"), []string{"--model", "x"}, CreateAgentOptions{})
		if a.Name() != "acp:custom --flag" {
			t.Fatalf("custom name %q", a.Name())
		}
	})
	t.Run("unknown", func(t *testing.T) {
		_, err := CreateAgent("unknown", runInfo, nil, nil, CreateAgentOptions{})
		if err == nil {
			t.Fatalf("should fail unknown")
		}
	})
	t.Run("isAcpSpec", func(t *testing.T) {
		if !IsAcpSpec("acp:gemini") {
			t.Fatalf("should be acp")
		}
		if IsAcpSpec("claude") {
			t.Fatalf("should not be acp")
		}
		if IsAcpSpec("acp:") {
			t.Fatalf("empty target should not be acp")
		}
	})
	t.Run("with stop field", func(t *testing.T) {
		a, _ := CreateAgent("claude", runInfo, nil, nil, CreateAgentOptions{IncludeStopField: true})
		ca := a.(*ClaudeAgent)
		if _, ok := ca.Schema.Properties["should_fully_stop"]; !ok {
			t.Fatalf("should have stop field")
		}
	})
	t.Run("with commit fields", func(t *testing.T) {
		a, _ := CreateAgent("claude", runInfo, nil, nil, CreateAgentOptions{CommitFields: []agent.CommitField{{Name: "type", Allowed: []string{"feat"}}}})
		ca := a.(*ClaudeAgent)
		if _, ok := ca.Schema.Properties["type"]; !ok {
			t.Fatalf("should have type")
		}
	})
	t.Run("gemini acp", func(t *testing.T) {
		a, _ := CreateAgent("gemini", runInfo, nil, nil, CreateAgentOptions{})
		if a.Name() != "acp:gemini" {
			t.Fatalf("gemini should be acp:gemini, got %q", a.Name())
		}
	})
}

func stringPtr(s string) *string { return &s }

func TestIsAgentSpec(t *testing.T) {
	if !IsAgentSpec("claude") {
		t.Fatalf("claude spec")
	}
	if !IsAgentSpec("acp:gemini") {
		t.Fatalf("acp spec")
	}
	if IsAgentSpec("invalid") {
		t.Fatalf("invalid should not be spec")
	}
}

func TestRedactAcp(t *testing.T) {
	if RedactAcpTargetForLogs("gemini") != "gemini" {
		t.Fatalf("named should not redact")
	}
	if RedactAcpTargetForLogs("custom command with spaces") != "custom" {
		t.Fatalf("custom should redact")
	}
}

func TestBuildClaudeArgs(t *testing.T) {
	schema := agent.BuildAgentOutputSchema(false, nil)
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
	// permission mode
	args2 := BuildClaudeArgs("hi", schema, []string{"--dangerously-skip-permissions"}, "")
	hasPerm := false
	for _, a := range args2 {
		if a == "--dangerously-skip-permissions" {
			hasPerm = true
		}
	}
	if !hasPerm {
		t.Fatalf("should keep user perm")
	}
	// count occurrences should be 1 if user specified
	count := 0
	for _, a := range args2 {
		if a == "--dangerously-skip-permissions" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("should have exactly one perm flag, got %d", count)
	}
}

func TestBuildCodexArgs(t *testing.T) {
	args := BuildCodexArgs("prompt", "/tmp/schema.json", []string{"--model", "old"}, "")
	if len(args) == 0 || args[0] != "exec" {
		t.Fatalf("should start with exec")
	}
}

func TestBuildCopilotArgs(t *testing.T) {
	schema := agent.BuildAgentOutputSchema(false, nil)
	args := BuildCopilotArgs("hi", schema, nil, "")
	hasAllowAll := false
	for _, a := range args {
		if a == "--allow-all" {
			hasAllowAll = true
		}
	}
	if !hasAllowAll {
		t.Fatalf("should have allow-all")
	}
}
