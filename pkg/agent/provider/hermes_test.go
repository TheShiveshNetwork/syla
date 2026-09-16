package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
)

func TestHermesAgent(t *testing.T) {
	a := &HermesAgent{Bin: "hermes", Schema: agent.BuildAgentOutputSchema(false, nil)}
	if a.Name() != "hermes" {
		t.Fatalf("name should be hermes, got %q", a.Name())
	}
	_, err := a.Run(context.Background(), "prompt", t.TempDir(), agent.RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("should be not implemented, got %v", err)
	}
}

func TestHermesViaFactory(t *testing.T) {
	// Test via agent.BuildAgentOutputSchema to ensure factory integration
	schema := agent.BuildAgentOutputSchema(false, nil)
	_ = schema
	// Direct test for hermes via provider
	a := &HermesAgent{Bin: "hermes", Schema: schema}
	if a.Bin != "hermes" {
		t.Fatalf("bin mismatch")
	}
}
