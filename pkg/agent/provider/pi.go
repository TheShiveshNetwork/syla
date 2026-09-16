package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"github.com/TheShiveshNetwork/syla/pkg/agent/internal"
)

// PiAgent is the native pi adapter.
type PiAgent struct {
	Bin       string
	ExtraArgs []string
	Model     string
	Schema    agent.AgentOutputSchema
}

func NewPiAgent(opts ...PiOption) *PiAgent {
	p := &PiAgent{
		Bin:    "pi",
		Schema: agent.BuildAgentOutputSchema(false, nil),
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

type PiOption func(*PiAgent)

func WithPiBin(bin string) PiOption                   { return func(p *PiAgent) { p.Bin = bin } }
func WithPiExtraArgs(args []string) PiOption          { return func(p *PiAgent) { p.ExtraArgs = args } }
func WithPiModel(m string) PiOption                   { return func(p *PiAgent) { p.Model = m } }
func WithPiSchema(s agent.AgentOutputSchema) PiOption { return func(p *PiAgent) { p.Schema = s } }

func (a *PiAgent) Name() string { return "pi" }

func BuildPiPrompt(prompt string, schema agent.AgentOutputSchema) string {
	b, _ := json.MarshalIndent(schema, "", "  ")
	return fmt.Sprintf("%s\n\n## gnhf final output contract\n\nWhen the iteration is complete, your final assistant response must be only valid JSON matching this JSON Schema. Do not wrap it in Markdown fences. Do not include prose before or after the JSON object.\n\n%s", prompt, string(b))
}

func BuildPiArgs(extraArgs []string, model string) []string {
	args := []string{}
	args = append(args, extraArgs...)
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "--mode", "json", "--no-session")
	return args
}

func piToTokenUsage(m map[string]any) *agent.TokenUsage {
	if m == nil {
		return nil
	}
	return &agent.TokenUsage{
		InputTokens:         internal.IntVal(m, "input"),
		OutputTokens:        internal.IntVal(m, "output"),
		CacheReadTokens:     internal.IntVal(m, "cacheRead"),
		CacheCreationTokens: internal.IntVal(m, "cacheWrite"),
	}
}

func piMessageKey(m map[string]any) *string {
	if id, ok := m["responseId"].(string); ok && id != "" {
		return &id
	}
	if id, ok := m["id"].(string); ok && id != "" {
		return &id
	}
	if ts, ok := m["timestamp"].(string); ok && ts != "" {
		s := "timestamp:" + ts
		return &s
	}
	if ts, ok := m["timestamp"].(float64); ok {
		s := fmt.Sprintf("timestamp:%v", ts)
		return &s
	}
	return nil
}

func piTextFromAssistantMessage(m map[string]any) string {
	if m == nil {
		return ""
	}
	if t, ok := m["text"].(string); ok {
		return t
	}
	if c, ok := m["content"].(string); ok {
		return c
	}
	if arr, ok := m["content"].([]any); ok {
		var parts []string
		for _, b := range arr {
			if s, ok := b.(string); ok {
				parts = append(parts, s)
			} else if mm, ok := b.(map[string]any); ok {
				if t, ok := mm["text"].(string); ok {
					parts = append(parts, t)
				} else if c, ok := mm["content"].(string); ok {
					parts = append(parts, c)
				}
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func (a *PiAgent) Run(ctx context.Context, prompt string, cwd string, opts agent.RunOptions) (agent.AgentResult, error) {
	if ctx.Err() != nil {
		return agent.AgentResult{}, fmt.Errorf("Agent was aborted")
	}
	model := opts.Model
	if model == "" {
		model = a.Model
	}
	args := BuildPiArgs(a.ExtraArgs, model)

	var latestAssistantMessage map[string]any
	streamTextByIndex := map[int]string{}
	completeTextByIndex := map[int]string{}
	usageByKey := map[string]agent.TokenUsage{}
	var lastEmittedUsage agent.TokenUsage
	var anonymousSeq int
	var currentStreamingKey *string

	updateUsage := func(msg map[string]any, streaming bool) {
		usageRaw, ok := msg["usage"].(map[string]any)
		if !ok {
			return
		}
		u := piToTokenUsage(usageRaw)
		if u == nil {
			return
		}
		key := piMessageKey(msg)
		if key == nil {
			if streaming && currentStreamingKey != nil {
				key = currentStreamingKey
			} else {
				k := fmt.Sprintf("assistant-anonymous-%d", anonymousSeq)
				anonymousSeq++
				key = &k
				if streaming {
					currentStreamingKey = key
				}
			}
		}
		usageByKey[*key] = *u
		var cum agent.TokenUsage
		for _, v := range usageByKey {
			cum.InputTokens += v.InputTokens
			cum.OutputTokens += v.OutputTokens
			cum.CacheReadTokens += v.CacheReadTokens
			cum.CacheCreationTokens += v.CacheCreationTokens
		}
		if !isSameUsage(cum, lastEmittedUsage) {
			lastEmittedUsage = cum
			if opts.OnUsage != nil {
				opts.OnUsage(cum)
			}
		}
	}

	rememberAssistant := func(msg any, streaming bool) {
		m, ok := msg.(map[string]any)
		if !ok {
			return
		}
		role, _ := m["role"].(string)
		if role != "assistant" {
			return
		}
		latestAssistantMessage = m
		updateUsage(m, streaming)
	}

	handleEvent := func(ev map[string]any) {
		if ev["type"] == "message_update" {
			rememberAssistant(ev["message"], true)
			if amev, ok := ev["assistantMessageEvent"].(map[string]any); ok {
				idx := internal.IntVal(amev, "contentIndex")
				if _, ok := amev["content_index"]; ok && idx == 0 {
					idx = internal.IntVal(amev, "content_index")
				}
				if amev["type"] == "text_delta" {
					delta := ""
					if d, ok := amev["delta"].(string); ok {
						delta = d
					} else if d, ok := amev["text"].(string); ok {
						delta = d
					} else if d, ok := amev["content"].(string); ok {
						delta = d
					}
					if delta != "" {
						next := streamTextByIndex[idx] + delta
						streamTextByIndex[idx] = next
						if strings.TrimSpace(next) != "" && opts.OnMessage != nil {
							opts.OnMessage(strings.TrimSpace(next))
						}
					}
				}
				if amev["type"] == "text_end" {
					txt := ""
					if t, ok := amev["text"].(string); ok {
						txt = t
					} else if t, ok := amev["content"].(string); ok {
						txt = t
					} else {
						txt = streamTextByIndex[idx]
					}
					completeTextByIndex[idx] = txt
					if strings.TrimSpace(txt) != "" && opts.OnMessage != nil {
						opts.OnMessage(strings.TrimSpace(txt))
					}
				}
			}
		}
		if ev["type"] == "message_end" || ev["type"] == "turn_end" {
			rememberAssistant(ev["message"], true)
			currentStreamingKey = nil
		}
		if ev["type"] == "agent_end" && latestAssistantMessage == nil {
			if msgs, ok := ev["messages"].([]any); ok {
				for i := len(msgs) - 1; i >= 0; i-- {
					if m, ok := msgs[i].(map[string]any); ok && m["role"] == "assistant" {
						rememberAssistant(m, false)
						break
					}
				}
			}
		}
	}

	stdoutTail, stderrStr, exitCode, err := internal.RunJSONL(ctx, internal.RunnerConfig{
		Bin:          a.Bin,
		Args:         args,
		Cwd:          cwd,
		LogPath:      opts.LogPath,
		StdinContent: BuildPiPrompt(prompt, a.Schema),
		Detached:     true,
	}, handleEvent)
	_ = stdoutTail
	if err != nil {
		if strings.Contains(err.Error(), "Agent was aborted") {
			return agent.AgentResult{}, err
		}
		return agent.AgentResult{}, err
	}
	if exitCode != 0 {
		failure := internal.DescribeChildProcessExit("pi", &exitCode, "", stderrStr)
		return agent.AgentResult{}, fmt.Errorf("%s", failure.Detail)
	}
	if latestAssistantMessage != nil {
		if sr, _ := latestAssistantMessage["stopReason"].(string); sr == "error" || sr == "aborted" {
			msg := ""
			if em, ok := latestAssistantMessage["errorMessage"].(string); ok {
				msg = em
			} else if em, ok := latestAssistantMessage["error"].(string); ok {
				msg = em
			} else if em, ok := latestAssistantMessage["message"].(string); ok {
				msg = em
			} else {
				b, _ := json.Marshal(latestAssistantMessage)
				msg = string(b)
			}
			return agent.AgentResult{}, fmt.Errorf("pi reported error: %s", msg)
		}
	}
	finalText := strings.TrimSpace(piTextFromAssistantMessage(latestAssistantMessage))
	if finalText == "" {
		finalText = textByIndexToString(completeTextByIndex)
		if strings.TrimSpace(finalText) == "" {
			finalText = textByIndexToString(streamTextByIndex)
		}
		finalText = strings.TrimSpace(finalText)
	}
	if finalText == "" {
		return agent.AgentResult{}, fmt.Errorf("pi returned no text output")
	}
	output, err := agent.ParseAgentOutput(finalText, a.Schema, "pi")
	if err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to parse pi output: %v", err)
	}
	return agent.AgentResult{Output: output, Usage: lastEmittedUsage}, nil
}

func textByIndexToString(m map[int]string) string {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(m[k])
	}
	return b.String()
}
