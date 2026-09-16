package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"github.com/TheShiveshNetwork/syla/pkg/agent/internal"
)

// OpenCodeAgent is the native opencode adapter.
type OpenCodeAgent struct {
	Bin       string
	ExtraArgs []string
	Model     string
	Schema    agent.AgentOutputSchema
}

func NewOpenCodeAgent(opts ...OpenCodeOption) *OpenCodeAgent {
	o := &OpenCodeAgent{
		Bin:    "opencode",
		Schema: agent.BuildAgentOutputSchema(false, nil),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

type OpenCodeOption func(*OpenCodeAgent)

func WithOpenCodeBin(bin string) OpenCodeOption { return func(o *OpenCodeAgent) { o.Bin = bin } }
func WithOpenCodeExtraArgs(args []string) OpenCodeOption {
	return func(o *OpenCodeAgent) { o.ExtraArgs = args }
}
func WithOpenCodeModel(m string) OpenCodeOption { return func(o *OpenCodeAgent) { o.Model = m } }
func WithOpenCodeSchema(s agent.AgentOutputSchema) OpenCodeOption {
	return func(o *OpenCodeAgent) { o.Schema = s }
}

func (a *OpenCodeAgent) Name() string { return "opencode" }

func BuildOpenCodePrompt(prompt string, schema agent.AgentOutputSchema) string {
	return fmt.Sprintf("%s\n\nWhen you finish, reply with only valid JSON. Do not wrap the JSON in markdown fences. Do not include any prose before or after the JSON. The JSON must match this schema exactly: %s", prompt, mustMarshal(schema))
}
func mustMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func BuildOpenCodeArgs(prompt string, schema agent.AgentOutputSchema, extraArgs []string, model string) []string {
	args := []string{"run"}
	args = append(args, extraArgs...)
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, BuildOpenCodePrompt(prompt, schema))
	return args
}

func (a *OpenCodeAgent) Run(ctx context.Context, prompt string, cwd string, opts agent.RunOptions) (agent.AgentResult, error) {
	if ctx.Err() != nil {
		return agent.AgentResult{}, fmt.Errorf("Agent was aborted")
	}
	model := opts.Model
	if model == "" {
		model = a.Model
	}
	args := BuildOpenCodeArgs(prompt, a.Schema, a.ExtraArgs, model)

	var lastOutputText string
	cumulative := agent.TokenUsage{}

	handleEvent := func(ev map[string]any) {
		if tokens, ok := ev["tokens"].(map[string]any); ok {
			u := agent.TokenUsage{
				InputTokens:         internal.IntVal(tokens, "input"),
				OutputTokens:        internal.IntVal(tokens, "output"),
				CacheReadTokens:     internal.IntVal(tokens, "cache_read"),
				CacheCreationTokens: internal.IntVal(tokens, "cache_write"),
			}
			cumulative.InputTokens += u.InputTokens
			cumulative.OutputTokens += u.OutputTokens
			cumulative.CacheReadTokens += u.CacheReadTokens
			cumulative.CacheCreationTokens += u.CacheCreationTokens
			if opts.OnUsage != nil {
				opts.OnUsage(cumulative)
			}
		}
		if text, ok := ev["text"].(string); ok && text != "" {
			lastOutputText = text
			if opts.OnMessage != nil {
				opts.OnMessage(strings.TrimSpace(text))
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
		// fallback: if event is not object with tokens/text, treat as text line is handled inside runner? runner only calls handleEvent for JSON lines, so non-JSON lines are ignored here.
		// For opencode we also want to capture raw text lines: runner doesn't expose raw lines, so we rely on stdoutTail fallback after.
	})
	if err != nil {
		if strings.Contains(err.Error(), "Agent was aborted") {
			return agent.AgentResult{}, err
		}
		return agent.AgentResult{}, err
	}
	if exitCode != 0 {
		failure := internal.DescribeChildProcessExit("opencode", &exitCode, stdoutTail, stderrStr)
		return agent.AgentResult{}, fmt.Errorf("%s", failure.Detail)
	}
	finalText := strings.TrimSpace(lastOutputText)
	if finalText == "" {
		finalText = strings.TrimSpace(stdoutTail)
	}
	// If stdoutTail contains JSONL, try to extract last json object text
	if finalText == "" || !strings.Contains(finalText, "\"success\"") {
		if extracted := internal.ParseAgentJSON(stdoutTail, nil); extracted != nil {
			if b, err := json.Marshal(extracted); err == nil {
				finalText = string(b)
			}
		}
	}
	if finalText == "" {
		return agent.AgentResult{}, fmt.Errorf("OpenCode produced no final answer")
	}
	// If lastOutputText was not JSON but stdoutTail was, try to use stdoutTail directly
	if _, err := agent.ParseAgentOutput(finalText, a.Schema, "opencode"); err != nil {
		// fallback to stdoutTail
		if alt := strings.TrimSpace(stdoutTail); alt != finalText {
			if _, err2 := agent.ParseAgentOutput(alt, a.Schema, "opencode"); err2 == nil {
				finalText = alt
			}
		}
	}
	output, err := agent.ParseAgentOutput(finalText, a.Schema, "opencode")
	if err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to parse opencode output: %v", err)
	}
	return agent.AgentResult{Output: output, Usage: cumulative}, nil
}

func (a *OpenCodeAgent) Close() error { return nil }
