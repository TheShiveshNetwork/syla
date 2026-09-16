package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"github.com/TheShiveshNetwork/syla/pkg/agent/internal"
)

// RovoDevAgent is the native rovodev adapter (via acli).
type RovoDevAgent struct {
	Bin        string
	ExtraArgs  []string
	SchemaPath string
	Schema     agent.AgentOutputSchema
}

func NewRovoDevAgent(schemaPath string, opts ...RovoDevOption) *RovoDevAgent {
	r := &RovoDevAgent{
		Bin:        "acli",
		SchemaPath: schemaPath,
		Schema:     agent.BuildAgentOutputSchema(false, nil),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

type RovoDevOption func(*RovoDevAgent)

func WithRovoDevBin(bin string) RovoDevOption { return func(r *RovoDevAgent) { r.Bin = bin } }
func WithRovoDevExtraArgs(args []string) RovoDevOption {
	return func(r *RovoDevAgent) { r.ExtraArgs = args }
}
func WithRovoDevSchema(s agent.AgentOutputSchema) RovoDevOption {
	return func(r *RovoDevAgent) { r.Schema = s }
}

func (a *RovoDevAgent) Name() string { return "rovodev" }

func BuildRovoDevArgs(prompt string, schema agent.AgentOutputSchema, extraArgs []string) []string {
	args := []string{"rovodev", "run"}
	args = append(args, extraArgs...)
	args = append(args, prompt, "--json-schema", mustMarshal(schema))
	return args
}

func (a *RovoDevAgent) Run(ctx context.Context, prompt string, cwd string, opts agent.RunOptions) (agent.AgentResult, error) {
	if ctx.Err() != nil {
		return agent.AgentResult{}, fmt.Errorf("Agent was aborted")
	}
	args := BuildRovoDevArgs(prompt, a.Schema, a.ExtraArgs)

	var lastText string
	cumulative := agent.TokenUsage{}

	handleEvent := func(ev map[string]any) {
		if _, ok := ev["input_tokens"]; ok {
			cumulative.InputTokens += internal.IntVal(ev, "input_tokens")
			cumulative.OutputTokens += internal.IntVal(ev, "output_tokens")
			cumulative.CacheReadTokens += internal.IntVal(ev, "cache_read_tokens")
			cumulative.CacheCreationTokens += internal.IntVal(ev, "cache_write_tokens")
			if opts.OnUsage != nil {
				opts.OnUsage(cumulative)
			}
		}
		if c, ok := ev["content"].(string); ok && c != "" {
			lastText = c
			if opts.OnMessage != nil {
				opts.OnMessage(strings.TrimSpace(c))
			}
		}
	}

	stdoutTail, stderrStr, exitCode, err := internal.RunJSONL(ctx, internal.RunnerConfig{
		Bin:      a.Bin,
		Args:     args,
		Cwd:      cwd,
		LogPath:  opts.LogPath,
		Detached: false,
	}, func(ev map[string]any) {
		handleEvent(ev)
	})
	// also handle raw text lines that are not JSON: runner's handleEvent only receives JSON lines.
	// For rovodev, non-JSON stdout lines are captured via stdoutTail fallback below.
	if err != nil {
		if strings.Contains(err.Error(), "Agent was aborted") {
			return agent.AgentResult{}, err
		}
		return agent.AgentResult{}, err
	}
	if exitCode != 0 {
		failure := internal.DescribeChildProcessExit("rovodev", &exitCode, stdoutTail, stderrStr)
		return agent.AgentResult{}, fmt.Errorf("%s", failure.Detail)
	}
	finalText := strings.TrimSpace(lastText)
	if finalText == "" {
		finalText = strings.TrimSpace(stdoutTail)
	}
	if finalText == "" {
		return agent.AgentResult{}, fmt.Errorf("rovodev returned no text output")
	}
	parsed := internal.ParseAgentJSON(finalText, func(v any) bool {
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		_, err := agent.ValidateAgentOutput(m, a.Schema)
		return err == nil
	})
	if parsed == nil {
		parsed = internal.ParseAgentJSON(finalText, nil)
		if parsed != nil {
			if m, ok := parsed.(map[string]any); ok {
				if _, err := agent.ValidateAgentOutput(m, a.Schema); err != nil {
					return agent.AgentResult{}, fmt.Errorf("Failed to parse rovodev output: %v", err)
				}
			}
		}
		if parsed == nil {
			return agent.AgentResult{}, fmt.Errorf("Failed to parse rovodev output: rovodev output did not contain a parseable JSON object")
		}
	}
	m, ok := parsed.(map[string]any)
	if !ok {
		return agent.AgentResult{}, fmt.Errorf("Failed to parse rovodev output")
	}
	output, err := agent.ValidateAgentOutput(m, a.Schema)
	if err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to parse rovodev output: %v", err)
	}
	return agent.AgentResult{Output: output, Usage: cumulative}, nil
}

func (a *RovoDevAgent) Close() error { return nil }
