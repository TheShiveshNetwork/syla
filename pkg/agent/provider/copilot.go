package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"github.com/TheShiveshNetwork/syla/pkg/agent/internal"
)

// CopilotAgent is the native copilot adapter.
type CopilotAgent struct {
	Bin       string
	ExtraArgs []string
	Model     string
	Schema    agent.AgentOutputSchema
}

func NewCopilotAgent(opts ...CopilotOption) *CopilotAgent {
	c := &CopilotAgent{
		Bin:    "copilot",
		Schema: agent.BuildAgentOutputSchema(false, nil),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type CopilotOption func(*CopilotAgent)

func WithCopilotBin(bin string) CopilotOption { return func(c *CopilotAgent) { c.Bin = bin } }
func WithCopilotExtraArgs(args []string) CopilotOption {
	return func(c *CopilotAgent) { c.ExtraArgs = args }
}
func WithCopilotModel(m string) CopilotOption { return func(c *CopilotAgent) { c.Model = m } }
func WithCopilotSchema(s agent.AgentOutputSchema) CopilotOption {
	return func(c *CopilotAgent) { c.Schema = s }
}

func (a *CopilotAgent) Name() string { return "copilot" }

func BuildCopilotPrompt(prompt string, schema agent.AgentOutputSchema) string {
	b, _ := json.MarshalIndent(schema, "", "  ")
	return fmt.Sprintf("%s\n\n## gnhf final output contract\n\nWhen the iteration is complete, your final answer must be a single JSON object that matches this JSON Schema:\n\n```json\n%s\n```\n\nReturn only the JSON object in the final answer. Do not wrap it in Markdown. Do not include explanatory prose outside the JSON object.", prompt, string(b))
}

func BuildCopilotArgs(prompt string, schema agent.AgentOutputSchema, extraArgs []string, model string) []string {
	args := []string{}
	args = append(args, extraArgs...)
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "-p", BuildCopilotPrompt(prompt, schema), "--output-format", "json", "--stream", "off", "--no-color")
	if !internal.ContainsAny(extraArgs, []string{"--allow-all", "--yolo"}) {
		// use full check for copilot permission modes
		if !userSpecifiedCopilotPermissionMode(extraArgs) {
			args = append(args, "--allow-all")
		}
	}
	return args
}

func userSpecifiedCopilotPermissionMode(args []string) bool {
	for _, a := range args {
		if a == "--allow-all" || a == "--yolo" || a == "--allow-all-tools" || a == "--allow-all-paths" || a == "--allow-all-urls" || a == "--allow-tool" || strings.HasPrefix(a, "--allow-tool=") || a == "--allow-url" || strings.HasPrefix(a, "--allow-url=") || a == "--deny-tool" || strings.HasPrefix(a, "--deny-tool=") || a == "--deny-url" || strings.HasPrefix(a, "--deny-url=") || a == "--available-tools" || strings.HasPrefix(a, "--available-tools=") || a == "--excluded-tools" || strings.HasPrefix(a, "--excluded-tools=") {
			return true
		}
	}
	return false
}

func copilotUsageFromRecord(m map[string]any) *agent.TokenUsage {
	var has bool
	u := agent.TokenUsage{}
	if v := internal.IntVal(m, "inputTokens"); v != 0 {
		u.InputTokens = v
		has = true
	} else if v := internal.IntVal(m, "input_tokens"); v != 0 {
		u.InputTokens = v
		has = true
	}
	if v := internal.IntVal(m, "outputTokens"); v != 0 {
		u.OutputTokens = v
		has = true
	} else if v := internal.IntVal(m, "output_tokens"); v != 0 {
		u.OutputTokens = v
		has = true
	}
	if v := internal.IntVal(m, "cacheReadTokens"); v != 0 {
		u.CacheReadTokens = v
		has = true
	} else if v := internal.IntVal(m, "cache_read_tokens"); v != 0 {
		u.CacheReadTokens = v
		has = true
	} else if v := internal.IntVal(m, "cache_read_input_tokens"); v != 0 {
		u.CacheReadTokens = v
		has = true
	}
	if v := internal.IntVal(m, "cacheCreationTokens"); v != 0 {
		u.CacheCreationTokens = v
		has = true
	} else if v := internal.IntVal(m, "cacheWriteTokens"); v != 0 {
		u.CacheCreationTokens = v
		has = true
	} else if v := internal.IntVal(m, "cache_creation_tokens"); v != 0 {
		u.CacheCreationTokens = v
		has = true
	} else if v := internal.IntVal(m, "cache_creation_input_tokens"); v != 0 {
		u.CacheCreationTokens = v
		has = true
	} else if v := internal.IntVal(m, "cache_write_tokens"); v != 0 {
		u.CacheCreationTokens = v
		has = true
	}
	if !has {
		return nil
	}
	return &u
}

func (a *CopilotAgent) Run(ctx context.Context, prompt string, cwd string, opts agent.RunOptions) (agent.AgentResult, error) {
	if ctx.Err() != nil {
		return agent.AgentResult{}, fmt.Errorf("Agent was aborted")
	}
	model := opts.Model
	if model == "" {
		model = a.Model
	}
	args := BuildCopilotArgs(prompt, a.Schema, a.ExtraArgs, model)

	var lastAgentMessage *string
	cumulative := agent.TokenUsage{}

	handleEvent := func(ev map[string]any) {
		if typ, _ := ev["type"].(string); typ == "assistant.message" {
			if dataRaw, ok := ev["data"].(map[string]any); ok {
				if content, ok := dataRaw["content"].(string); ok {
					lastAgentMessage = &content
					if opts.OnMessage != nil {
						opts.OnMessage(content)
					}
				}
				if ot, ok := dataRaw["outputTokens"].(float64); ok {
					cumulative.OutputTokens += int(ot)
					if opts.OnUsage != nil {
						opts.OnUsage(cumulative)
					}
				}
			}
		}
		if usageRaw, ok := ev["usage"].(map[string]any); ok {
			if u := copilotUsageFromRecord(usageRaw); u != nil {
				cumulative.InputTokens = u.InputTokens
				if u.OutputTokens > cumulative.OutputTokens {
					cumulative.OutputTokens = u.OutputTokens
				}
				cumulative.CacheReadTokens = u.CacheReadTokens
				cumulative.CacheCreationTokens = u.CacheCreationTokens
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
		Detached: true,
	}, handleEvent)
	if err != nil {
		if strings.Contains(err.Error(), "Agent was aborted") {
			return agent.AgentResult{}, err
		}
		return agent.AgentResult{}, err
	}
	if exitCode != 0 {
		failure := internal.DescribeChildProcessExit("copilot", &exitCode, stdoutTail, stderrStr)
		return agent.AgentResult{}, fmt.Errorf("%s", failure.Detail)
	}
	if lastAgentMessage == nil {
		return agent.AgentResult{}, fmt.Errorf("copilot returned no agent message")
	}
	output, err := agent.ParseAgentOutput(*lastAgentMessage, a.Schema, "copilot")
	if err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to parse copilot output: %v", err)
	}
	return agent.AgentResult{Output: output, Usage: cumulative}, nil
}
