package agent

import (
	"testing"
)

func TestBuildAgentOutputSchema(t *testing.T) {
	schema := BuildAgentOutputSchema(false, []CommitField{
		{Name: "type", Allowed: []string{"feat", "fix"}},
		{Name: "scope"},
	})
	if schema.Properties["type"].Enum[0] != "feat" {
		t.Fatalf("expected feat enum")
	}
	if schema.Properties["scope"].Type != "string" {
		t.Fatalf("scope type string")
	}
	foundType := false
	foundScope := false
	for _, r := range schema.Required {
		if r == "type" {
			foundType = true
		}
		if r == "scope" {
			foundScope = true
		}
	}
	if !foundType || !foundScope {
		t.Fatalf("required missing")
	}
	if _, ok := schema.Properties["should_fully_stop"]; ok {
		t.Fatalf("should not have stop field")
	}
	schema2 := BuildAgentOutputSchema(true, nil)
	if _, ok := schema2.Properties["should_fully_stop"]; !ok {
		t.Fatalf("should have stop")
	}
}

func TestValidateAgentOutput(t *testing.T) {
	schema := BuildAgentOutputSchema(false, nil)
	valid := map[string]any{
		"success": true, "summary": "ok", "key_changes_made": []any{"a"}, "key_learnings": []any{"b"},
	}
	if _, err := ValidateAgentOutput(valid, schema); err != nil {
		t.Fatalf("valid should pass %v", err)
	}
	invalid := map[string]any{
		"success": true, "summary": "ok", "key_changes_made": []any{"a"},
	}
	if _, err := ValidateAgentOutput(invalid, schema); err == nil {
		t.Fatalf("missing required should fail")
	}
	extra := map[string]any{
		"success": true, "summary": "ok", "key_changes_made": []any{}, "key_learnings": []any{}, "extra": "x",
	}
	if _, err := ValidateAgentOutput(extra, schema); err == nil {
		t.Fatalf("extra property should fail")
	}
}

func TestParseAgentOutput(t *testing.T) {
	schema := BuildAgentOutputSchema(false, nil)
	text := `{"success": true, "summary": "done", "key_changes_made": ["x"], "key_learnings": ["y"]}`
	out, err := ParseAgentOutput(text, schema, "test")
	if err != nil {
		t.Fatalf("parse failed %v", err)
	}
	if !out.Success || out.Summary != "done" {
		t.Fatalf("wrong output %#v", out)
	}
	// with fences
	text2 := "```json\n" + text + "\n```"
	out2, err := ParseAgentOutput(text2, schema, "test")
	if err != nil {
		t.Fatalf("fence parse failed %v", err)
	}
	if out2.Summary != "done" {
		t.Fatalf("fence wrong")
	}
	// invalid
	_, err = ParseAgentOutput("no json here", schema, "claude")
	if err == nil {
		t.Fatalf("should fail")
	}
}
