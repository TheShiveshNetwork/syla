package provider

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
)

// Agent names supported natively.
var AgentNames = []string{"claude", "codex", "copilot", "cursor", "opencode", "pi", "rovodev", "gemini", "hermes"}

type AgentName string

const (
	AgentClaude   AgentName = "claude"
	AgentCodex    AgentName = "codex"
	AgentCopilot  AgentName = "copilot"
	AgentCursor   AgentName = "cursor"
	AgentOpencode AgentName = "opencode"
	AgentPi       AgentName = "pi"
	AgentRovoDev  AgentName = "rovodev"
	AgentGemini   AgentName = "gemini"
	AgentHermes   AgentName = "hermes"
)

func IsAgentName(name string) bool {
	for _, n := range AgentNames {
		if n == name {
			return true
		}
	}
	return false
}

func IsAcpSpec(spec string) bool {
	if !strings.HasPrefix(spec, "acp:") {
		return false
	}
	target := strings.TrimPrefix(spec, "acp:")
	if target == "" || strings.TrimSpace(target) != target {
		return false
	}
	for _, ch := range target {
		if ch < 0x20 || ch == 0x7f {
			return false
		}
	}
	return true
}

func IsAgentSpec(spec string) bool {
	return IsAgentName(spec) || IsAcpSpec(spec)
}

func GetAcpTarget(spec string) string {
	return strings.TrimPrefix(spec, "acp:")
}

// RunInfo mirrors TS RunInfo minimal fields needed for factory.
type RunInfo struct {
	RunID      string
	RunDir     string
	SchemaPath string
}

// CreateAgentOptions mirrors TS CreateAgentOptions.
type CreateAgentOptions struct {
	IncludeStopField     bool
	CommitFields         []agent.CommitField
	AcpRegistryOverrides map[string]string
	Model                string
}

func resolveBin(defaultBin string, override *string) string {
	if override != nil && *override != "" {
		return *override
	}
	return defaultBin
}

func acpSessionDir(runInfo RunInfo) string {
	return filepath.Join(runInfo.RunDir, "acp-sessions")
}

// CreateAgent creates an Agent from spec, similar to TS factory.
func CreateAgent(spec string, runInfo RunInfo, pathOverride *string, agentArgsOverride []string, opts CreateAgentOptions) (agent.Agent, error) {
	schema := agent.BuildAgentOutputSchema(opts.IncludeStopField, opts.CommitFields)

	if IsAcpSpec(spec) {
		target := GetAcpTarget(spec)
		return NewAcpAgent(target, schema, runInfo.RunID, acpSessionDir(runInfo), opts.AcpRegistryOverrides), nil
	}

	// native agents via registry map to avoid duplicate bin/config logic
	type builder func() (agent.Agent, error)
	registry := map[string]builder{
		"claude": func() (agent.Agent, error) {
			bin := resolveBin("claude", pathOverride)
			return NewClaudeAgent(WithClaudeBin(bin), WithClaudeExtraArgs(agentArgsOverride), WithClaudeModel(opts.Model), WithClaudeSchema(schema)), nil
		},
		"codex": func() (agent.Agent, error) {
			bin := resolveBin("codex", pathOverride)
			return NewCodexAgent(runInfo.SchemaPath, WithCodexBin(bin), WithCodexExtraArgs(agentArgsOverride), WithCodexModel(opts.Model)), nil
		},
		"copilot": func() (agent.Agent, error) {
			bin := resolveBin("copilot", pathOverride)
			return NewCopilotAgent(WithCopilotBin(bin), WithCopilotExtraArgs(agentArgsOverride), WithCopilotModel(opts.Model), WithCopilotSchema(schema)), nil
		},
		"opencode": func() (agent.Agent, error) {
			bin := resolveBin("opencode", pathOverride)
			return NewOpenCodeAgent(WithOpenCodeBin(bin), WithOpenCodeExtraArgs(agentArgsOverride), WithOpenCodeModel(opts.Model), WithOpenCodeSchema(schema)), nil
		},
		"pi": func() (agent.Agent, error) {
			bin := resolveBin("pi", pathOverride)
			return NewPiAgent(WithPiBin(bin), WithPiExtraArgs(agentArgsOverride), WithPiModel(opts.Model), WithPiSchema(schema)), nil
		},
		"cursor": func() (agent.Agent, error) {
			bin := resolveBin("cursor-agent", pathOverride)
			return NewCursorAgent(WithCursorBin(bin), WithCursorExtraArgs(agentArgsOverride), WithCursorModel(opts.Model), WithCursorSchema(schema)), nil
		},
		"rovodev": func() (agent.Agent, error) {
			bin := resolveBin("acli", pathOverride)
			return NewRovoDevAgent(runInfo.SchemaPath, WithRovoDevBin(bin), WithRovoDevExtraArgs(agentArgsOverride), WithRovoDevSchema(schema)), nil
		},
		"gemini": func() (agent.Agent, error) {
			return NewAcpAgent("gemini", schema, runInfo.RunID, acpSessionDir(runInfo), opts.AcpRegistryOverrides), nil
		},
		"hermes": func() (agent.Agent, error) {
			bin := resolveBin("hermes", pathOverride)
			return &HermesAgent{Bin: bin, Schema: schema}, nil
		},
	}

	if b, ok := registry[spec]; ok {
		return b()
	}
	return nil, fmt.Errorf("unknown agent %q", spec)
}
