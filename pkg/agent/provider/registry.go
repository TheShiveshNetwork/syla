package provider

import (
	"fmt"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
)

// Get returns an Agent for the given name using defaults (no custom bin, args, schema).
// This is a convenience for SDK usage: loop.New(loop.Config{Agent: agent.Get("codex")})
func Get(name string) (agent.Agent, error) {
	if IsAcpSpec(name) {
		return nil, fmt.Errorf("acp agents require run context - use CreateAgent")
	}
	if !IsAgentName(name) {
		return nil, fmt.Errorf("unknown agent %q", name)
	}
	// minimal RunInfo for SDK - callers can override via options if needed
	runInfo := RunInfo{RunID: "sdk", RunDir: ".", SchemaPath: ".syla/schema.json"}
	agentInstance, err := CreateAgent(name, runInfo, nil, nil, CreateAgentOptions{IncludeStopField: false})
	if err != nil {
		return nil, err
	}
	return agentInstance, nil
}

// MustGet is like Get but panics on error.
func MustGet(name string) agent.Agent {
	a, err := Get(name)
	if err != nil {
		panic(err)
	}
	return a
}
