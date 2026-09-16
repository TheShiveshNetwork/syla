package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"github.com/TheShiveshNetwork/syla/pkg/agent/internal"
)

// CursorAgent is the native Cursor adapter.
type CursorAgent struct {
	Bin                string
	ExtraArgs          []string
	Model              string
	Schema             agent.AgentOutputSchema
	FinalResultGraceMs time.Duration
}

func NewCursorAgent(opts ...CursorOption) *CursorAgent {
	c := &CursorAgent{
		Bin:                "cursor-agent",
		Schema:             agent.BuildAgentOutputSchema(false, nil),
		FinalResultGraceMs: defaultFinalResultExitGrace,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type CursorOption func(*CursorAgent)

func WithCursorBin(bin string) CursorOption { return func(c *CursorAgent) { c.Bin = bin } }
func WithCursorExtraArgs(args []string) CursorOption {
	return func(c *CursorAgent) { c.ExtraArgs = args }
}
func WithCursorModel(m string) CursorOption { return func(c *CursorAgent) { c.Model = m } }
func WithCursorSchema(s agent.AgentOutputSchema) CursorOption {
	return func(c *CursorAgent) { c.Schema = s }
}

func (a *CursorAgent) Name() string { return "cursor" }

func BuildCursorPrompt(prompt string, schema agent.AgentOutputSchema) string {
	b, _ := json.MarshalIndent(schema, "", "  ")
	return fmt.Sprintf("%s\n\n## gnhf final output contract\n\nWhen the iteration is complete, your final answer must be a single JSON object that matches this JSON Schema:\n\n```json\n%s\n```\n\nReturn only the JSON object in the final answer. Do not wrap it in Markdown. Do not include explanatory prose outside the JSON object.", prompt, string(b))
}

func BuildCursorArgs(extraArgs []string, model string) []string {
	args := []string{}
	args = append(args, extraArgs...)
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "-p", "--output-format", "stream-json")
	if !containsAny(extraArgs, []string{"--force", "-f", "--yolo", "--auto-review"}) {
		args = append(args, "--force")
	}
	if !containsAny(extraArgs, []string{"--trust"}) {
		args = append(args, "--trust")
	}
	if !containsAny(extraArgs, []string{"--approve-mcps"}) {
		args = append(args, "--approve-mcps")
	}
	return args
}

func cursorUsageFromRecord(m map[string]any) *agent.TokenUsage {
	var has bool
	u := agent.TokenUsage{}
	if v := intVal(m, "inputTokens"); v != 0 || hasKey(m, "inputTokens") {
		u.InputTokens = v
		has = true
	} else if v := intVal(m, "input_tokens"); v != 0 || hasKey(m, "input_tokens") {
		u.InputTokens = v
		has = true
	}
	if v := intVal(m, "outputTokens"); v != 0 || hasKey(m, "outputTokens") {
		u.OutputTokens = v
		has = true
	} else if v := intVal(m, "output_tokens"); v != 0 || hasKey(m, "output_tokens") {
		u.OutputTokens = v
		has = true
	}
	if v := intVal(m, "cacheReadTokens"); v != 0 || hasKey(m, "cacheReadTokens") {
		u.CacheReadTokens = v
		has = true
	} else if v := intVal(m, "cache_read_tokens"); v != 0 || hasKey(m, "cache_read_tokens") {
		u.CacheReadTokens = v
		has = true
	} else if v := intVal(m, "cache_read_input_tokens"); v != 0 || hasKey(m, "cache_read_input_tokens") {
		u.CacheReadTokens = v
		has = true
	}
	if v := intVal(m, "cacheWriteTokens"); v != 0 || hasKey(m, "cacheWriteTokens") {
		u.CacheCreationTokens = v
		has = true
	} else if v := intVal(m, "cacheCreationTokens"); v != 0 || hasKey(m, "cacheCreationTokens") {
		u.CacheCreationTokens = v
		has = true
	} else if v := intVal(m, "cache_write_tokens"); v != 0 || hasKey(m, "cache_write_tokens") {
		u.CacheCreationTokens = v
		has = true
	} else if v := intVal(m, "cache_creation_tokens"); v != 0 || hasKey(m, "cache_creation_tokens") {
		u.CacheCreationTokens = v
		has = true
	} else if v := intVal(m, "cache_creation_input_tokens"); v != 0 || hasKey(m, "cache_creation_input_tokens") {
		u.CacheCreationTokens = v
		has = true
	}
	if !has {
		return nil
	}
	return &u
}
func hasKey(m map[string]any, k string) bool { _, ok := m[k]; return ok }

func textFromContentBlock(block any) string {
	if s, ok := block.(string); ok {
		return s
	}
	if m, ok := block.(map[string]any); ok {
		if t, ok := m["text"].(string); ok {
			return t
		}
		if c, ok := m["content"].(string); ok {
			return c
		}
	}
	return ""
}
func textFromAssistantMessage(msg map[string]any) string {
	if msg == nil {
		return ""
	}
	if c, ok := msg["content"].(string); ok {
		return c
	}
	if arr, ok := msg["content"].([]any); ok {
		var parts []string
		for _, b := range arr {
			if t := textFromContentBlock(b); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func isPermanentCursorError(output string) bool {
	l := strings.ToLower(output)
	return strings.Contains(l, "authentication required") || strings.Contains(l, "not logged in") || strings.Contains(l, "not authenticated")
}

func (a *CursorAgent) Run(ctx context.Context, prompt string, cwd string, opts agent.RunOptions) (agent.AgentResult, error) {
	if ctx.Err() != nil {
		return agent.AgentResult{}, fmt.Errorf("Agent was aborted")
	}
	args := BuildCursorArgs(a.ExtraArgs, a.Model)
	if opts.Model != "" {
		// override model in args if provided via agent.RunOptions
		args = BuildCursorArgs(a.ExtraArgs, opts.Model)
	}
	cmd := exec.CommandContext(ctx, a.Bin, args...)
	cmd.Dir = cwd
	cmd.SysProcAttr = internal.NewSysProcAttr(true)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to spawn cursor: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to spawn cursor: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to spawn cursor: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to spawn cursor: %w", err)
	}
	// write prompt to stdin
	go func() {
		_, _ = io.WriteString(stdin, BuildCursorPrompt(prompt, a.Schema))
		_ = stdin.Close()
	}()
	go func() {
		<-ctx.Done()
		_ = cmd.Process.Signal(os.Interrupt)
	}()

	var logFile *os.File
	if opts.LogPath != "" {
		f, _ := os.Create(opts.LogPath)
		if f != nil {
			logFile = f
			defer f.Close()
		}
	}

	var stderrBuilder strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		data, _ := io.ReadAll(stderrPipe)
		stderrBuilder.WriteString(string(data))
	}()

	var lastAssistantText *string
	var resultText *string
	var resultError *string
	usage := agent.TokenUsage{}
	var finalTimer *time.Timer
	closedAfterFinalCleanup := false
	var timerMu sync.Mutex

	handleEvent := func(ev map[string]any) {
		typ, _ := ev["type"].(string)
		if typ == "assistant" {
			txt := textFromAssistantMessage(ev["message"].(map[string]any))
			if strings.TrimSpace(txt) != "" {
				lastAssistantText = &txt
				if opts.OnMessage != nil {
					opts.OnMessage(strings.TrimSpace(txt))
				}
			}
			return
		}
		if typ != "result" {
			return
		}
		if txt, ok := ev["result"].(string); ok {
			resultText = &txt
		}
		if isErr, _ := ev["is_error"].(bool); isErr {
			msg := "cursor reported an error result"
			if txt, ok := ev["result"].(string); ok && strings.TrimSpace(txt) != "" {
				msg = txt
			}
			resultError = &msg
		} else if subtype, _ := ev["subtype"].(string); subtype == "error" {
			msg := "cursor reported an error result"
			if txt, ok := ev["result"].(string); ok && strings.TrimSpace(txt) != "" {
				msg = txt
			}
			resultError = &msg
		}
		if usageRaw, ok := ev["usage"].(map[string]any); ok {
			if u := cursorUsageFromRecord(usageRaw); u != nil {
				usage = *u
				if opts.OnUsage != nil {
					opts.OnUsage(usage)
				}
			}
		}
		if !isCursorErrorResult(ev) {
			timerMu.Lock()
			if finalTimer != nil {
				finalTimer.Stop()
			}
			finalTimer = time.AfterFunc(a.FinalResultGraceMs, func() {
				closedAfterFinalCleanup = true
				_ = internal.SignalChildProcess(cmd, true, os.Interrupt)
				time.Sleep(100 * time.Millisecond)
				_ = cmd.Process.Kill()
			})
			timerMu.Unlock()
		}
	}

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 10*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			raw := line + "\n"
			if logFile != nil {
				_, _ = logFile.Write([]byte(raw))
			}
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			var ev map[string]any
			if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
				continue
			}
			handleEvent(ev)
		}
	}()

	err = cmd.Wait()
	wg.Wait()
	timerMu.Lock()
	if finalTimer != nil {
		finalTimer.Stop()
	}
	timerMu.Unlock()

	if ctx.Err() != nil {
		return agent.AgentResult{}, fmt.Errorf("Agent was aborted")
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			if !closedAfterFinalCleanup {
				detail := fmt.Sprintf("cursor exited with code %d: %s", code, stderrBuilder.String())
				if isPermanentCursorError(stderrBuilder.String()) {
					return agent.AgentResult{}, agent.NewPermanentError("cursor is not signed in - run `cursor-agent login`", detail)
				}
				return agent.AgentResult{}, fmt.Errorf("%s", detail)
			}
		}
	}
	if resultError != nil {
		if isPermanentCursorError(*resultError) {
			return agent.AgentResult{}, agent.NewPermanentError("cursor is not signed in - run `cursor-agent login`", *resultError)
		}
		return agent.AgentResult{}, fmt.Errorf("%s", *resultError)
	}
	finalText := ""
	if lastAssistantText != nil {
		finalText = strings.TrimSpace(*lastAssistantText)
	} else if resultText != nil {
		finalText = strings.TrimSpace(*resultText)
	}
	if finalText == "" {
		return agent.AgentResult{}, fmt.Errorf("cursor returned no text output")
	}
	output, err := agent.ParseAgentOutput(finalText, a.Schema, "cursor")
	if err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to parse cursor output: %v", err)
	}
	return agent.AgentResult{Output: output, Usage: usage}, nil
}

func isCursorErrorResult(ev map[string]any) bool {
	isErr, _ := ev["is_error"].(bool)
	subtype, _ := ev["subtype"].(string)
	return isErr || subtype == "error"
}
