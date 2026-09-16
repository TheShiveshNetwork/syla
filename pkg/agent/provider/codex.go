package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"github.com/TheShiveshNetwork/syla/pkg/agent/internal"
)

// CodexAgent is the native codex adapter.
type CodexAgent struct {
	Bin        string
	ExtraArgs  []string
	Model      string
	SchemaPath string
	Schema     agent.AgentOutputSchema
}

func NewCodexAgent(schemaPath string, opts ...CodexOption) *CodexAgent {
	c := &CodexAgent{
		Bin:        "codex",
		SchemaPath: schemaPath,
		Schema:     agent.BuildAgentOutputSchema(false, nil),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type CodexOption func(*CodexAgent)

func WithCodexBin(bin string) CodexOption          { return func(c *CodexAgent) { c.Bin = bin } }
func WithCodexExtraArgs(args []string) CodexOption { return func(c *CodexAgent) { c.ExtraArgs = args } }
func WithCodexModel(m string) CodexOption          { return func(c *CodexAgent) { c.Model = m } }
func WithCodexSchemaPath(p string) CodexOption     { return func(c *CodexAgent) { c.SchemaPath = p } }

func (a *CodexAgent) Name() string { return "codex" }

func BuildCodexArgs(prompt, schemaPath string, extraArgs []string, model string) []string {
	userSpecifiedExecutionMode := internal.ContainsAny(extraArgs, []string{"--full-auto", "--dangerously-bypass-approvals-and-sandbox", "--sandbox", "-s", "--ask-for-approval", "-a"})
	args := []string{"exec"}
	args = append(args, extraArgs...)
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, prompt, "--json", "--output-schema", schemaPath, "--color", "never")
	if !userSpecifiedExecutionMode {
		args = append(args[:len(args)-2], append([]string{"--dangerously-bypass-approvals-and-sandbox"}, args[len(args)-2:]...)...)
	}
	return args
}

func usageFromCodexTurn(u map[string]any) agent.TokenUsage {
	cacheRead := internal.IntVal(u, "cached_input_tokens")
	input := internal.IntVal(u, "input_tokens")
	output := internal.IntVal(u, "output_tokens")
	return agent.TokenUsage{
		InputTokens:         max(0, input-cacheRead),
		OutputTokens:        output,
		CacheReadTokens:     cacheRead,
		CacheCreationTokens: 0,
	}
}

func (a *CodexAgent) Run(ctx context.Context, prompt string, cwd string, opts agent.RunOptions) (agent.AgentResult, error) {
	if ctx.Err() != nil {
		return agent.AgentResult{}, fmt.Errorf("Agent was aborted")
	}
	model := opts.Model
	if model == "" {
		model = a.Model
	}
	args := BuildCodexArgs(prompt, a.SchemaPath, a.ExtraArgs, model)

	var lastAgentMessage *string
	cumulative := agent.TokenUsage{}

	handleEvent := func(ev map[string]any) {
		typ, _ := ev["type"].(string)
		if typ == "item.completed" {
			if itemRaw, ok := ev["item"].(map[string]any); ok && itemRaw["type"] == "agent_message" {
				if txt, ok := itemRaw["text"].(string); ok {
					lastAgentMessage = &txt
					if opts.OnMessage != nil {
						opts.OnMessage(txt)
					}
				}
			}
		}
		if typ == "turn.completed" {
			if usageRaw, ok := ev["usage"].(map[string]any); ok {
				u := usageFromCodexTurn(usageRaw)
				cumulative.InputTokens += u.InputTokens
				cumulative.OutputTokens += u.OutputTokens
				cumulative.CacheReadTokens += u.CacheReadTokens
				cumulative.CacheCreationTokens += u.CacheCreationTokens
				if opts.OnUsage != nil {
					opts.OnUsage(cumulative)
				}
			}
		}
	}

	stdoutTail, stderrStr, exitCode, err := internal.RunJSONL(ctx, internal.RunnerConfig{
		Bin:      a.Bin,
		Args:     args,
		Cwd:      cwd,
		LogPath:  opts.LogPath,
		Detached: false,
	}, handleEvent)
	if err != nil {
		if strings.Contains(err.Error(), "Agent was aborted") {
			return agent.AgentResult{}, err
		}
		return agent.AgentResult{}, err
	}
	if exitCode != 0 {
		failure := internal.DescribeChildProcessExit("codex", &exitCode, stdoutTail, stderrStr)
		return agent.AgentResult{}, fmt.Errorf("%s", failure.Detail)
	}
	if lastAgentMessage == nil {
		return agent.AgentResult{}, fmt.Errorf("codex returned no agent message")
	}
	var outputMap map[string]any
	if err := json.Unmarshal([]byte(*lastAgentMessage), &outputMap); err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to parse codex output: %v", err)
	}
	if len(a.Schema.Properties) > 0 {
		o, err := agent.ValidateAgentOutput(outputMap, a.Schema)
		if err != nil {
			return agent.AgentResult{}, fmt.Errorf("Failed to parse codex output: %v", err)
		}
		return agent.AgentResult{Output: o, Usage: cumulative}, nil
	}
	b, _ := json.Marshal(outputMap)
	var out agent.AgentOutput
	_ = json.Unmarshal(b, &out)
	return agent.AgentResult{Output: out, Usage: cumulative}, nil
}
