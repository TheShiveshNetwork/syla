package provider

import (
	"context"
	"fmt"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
)

// HermesAgent is a placeholder for pty-based hermes.
type HermesAgent struct {
	Bin    string
	Schema agent.AgentOutputSchema
}

func (a *HermesAgent) Name() string { return "hermes" }
func (a *HermesAgent) Run(ctx context.Context, prompt string, cwd string, opts agent.RunOptions) (agent.AgentResult, error) {
	return agent.AgentResult{}, fmt.Errorf("hermes agent not implemented - requires pty fallback contribution")
}
