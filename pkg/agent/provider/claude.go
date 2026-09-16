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

const defaultFinalResultExitGrace = 15 * time.Second

// ClaudeAgent implements the native Claude adapter.
type ClaudeAgent struct {
	Bin                string
	ExtraArgs          []string
	Model              string
	Schema             agent.AgentOutputSchema
	FinalResultGraceMs time.Duration
}

func NewClaudeAgent(opts ...ClaudeOption) *ClaudeAgent {
	c := &ClaudeAgent{
		Bin:                "claude",
		Schema:             agent.BuildAgentOutputSchema(false, nil),
		FinalResultGraceMs: defaultFinalResultExitGrace,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type ClaudeOption func(*ClaudeAgent)

func WithClaudeBin(bin string) ClaudeOption {
	return func(c *ClaudeAgent) { c.Bin = bin }
}
func WithClaudeExtraArgs(args []string) ClaudeOption {
	return func(c *ClaudeAgent) { c.ExtraArgs = args }
}
func WithClaudeModel(m string) ClaudeOption {
	return func(c *ClaudeAgent) { c.Model = m }
}
func WithClaudeSchema(s agent.AgentOutputSchema) ClaudeOption {
	return func(c *ClaudeAgent) { c.Schema = s }
}

func (a *ClaudeAgent) Name() string { return "claude" }

// BuildClaudeArgs builds CLI args for testing.
func BuildClaudeArgs(prompt string, schema agent.AgentOutputSchema, extraArgs []string, model string) []string {
	filtered := extraArgs
	if model != "" {
		filtered = filterModelArgs(extraArgs, model)
	}
	userSpecifiedPermissionMode := containsAny(filtered, []string{"--dangerously-skip-permissions", "--permission-mode", "--permission-prompt-tool"})
	schemaJSON, _ := json.Marshal(schema)
	args := []string{}
	args = append(args, filtered...)
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "-p", prompt, "--verbose", "--output-format", "stream-json", "--json-schema", string(schemaJSON))
	if !userSpecifiedPermissionMode {
		args = append(args, "--dangerously-skip-permissions")
	}
	return args
}

func filterModelArgs(args []string, model string) []string {
	if model == "" {
		return args
	}
	var out []string
	skipNext := false
	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--model" || strings.HasPrefix(arg, "--model=") {
			if arg == "--model" {
				skipNext = true
			}
			continue
		}
		// also check prev arg was --model (handled via skip)
		_ = i
		out = append(out, arg)
	}
	return out
}

func containsAny(args []string, prefixes []string) bool {
	for _, a := range args {
		for _, p := range prefixes {
			if a == p || strings.HasPrefix(a, p+"=") || strings.HasPrefix(a, p) {
				// for permission mode we need exact or prefix
				if p == "--permission-mode" || p == "--permission-prompt-tool" {
					if a == p || strings.HasPrefix(a, p+"=") {
						return true
					}
				} else if a == p {
					return true
				}
			}
		}
	}
	return false
}

func toTokenUsageClaude(m map[string]any) agent.TokenUsage {
	return agent.TokenUsage{
		InputTokens:         intVal(m, "input_tokens"),
		OutputTokens:        intVal(m, "output_tokens"),
		CacheReadTokens:     intVal(m, "cache_read_input_tokens"),
		CacheCreationTokens: intVal(m, "cache_creation_input_tokens"),
	}
}

func intVal(m map[string]any, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return 0
}

func isSameUsage(a, b agent.TokenUsage) bool {
	return a.InputTokens == b.InputTokens && a.OutputTokens == b.OutputTokens && a.CacheReadTokens == b.CacheReadTokens && a.CacheCreationTokens == b.CacheCreationTokens
}
func extendsUsage(next, prev agent.TokenUsage) bool {
	return next.InputTokens >= prev.InputTokens && next.OutputTokens >= prev.OutputTokens && next.CacheReadTokens >= prev.CacheReadTokens && next.CacheCreationTokens >= prev.CacheCreationTokens && !isSameUsage(next, prev)
}

func isPermanentClaudeError(output string) bool {
	return strings.Contains(strings.ToLower(output), "credit balance is too low")
}

func (a *ClaudeAgent) Run(ctx context.Context, prompt string, cwd string, opts agent.RunOptions) (agent.AgentResult, error) {
	if ctx.Err() != nil {
		return agent.AgentResult{}, fmt.Errorf("Agent was aborted")
	}
	model := opts.Model
	if model == "" {
		model = a.Model
	}
	args := BuildClaudeArgs(prompt, a.Schema, a.ExtraArgs, model)

	cmd := exec.CommandContext(ctx, a.Bin, args...)
	cmd.Dir = cwd
	cmd.SysProcAttr = internal.NewSysProcAttr(true)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to spawn claude: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to spawn claude: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return agent.AgentResult{}, fmt.Errorf("Failed to spawn claude: %w", err)
	}

	// Handle context cancellation
	ctxDone := ctx.Done()
	aborted := false
	go func() {
		<-ctxDone
		aborted = true
		_ = internal.SignalChildProcess(cmd, true, os.Interrupt)
	}()

	var logFile *os.File
	if opts.LogPath != "" {
		f, err := os.Create(opts.LogPath)
		if err == nil {
			logFile = f
			defer f.Close()
		}
	}

	var mu sync.Mutex
	var stdoutTail string
	var stderrBuilder strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)

	// stderr
	go func() {
		defer wg.Done()
		data, _ := io.ReadAll(stderrPipe)
		stderrBuilder.WriteString(string(data))
	}()

	// state for usage tracking
	cumulative := agent.TokenUsage{}
	usageByMessageID := map[string]agent.TokenUsage{}
	anonymousCount := 0
	var lastAnonymousID *string
	var lastAnonymousUsage *agent.TokenUsage
	var pendingAnonymousUsage *agent.TokenUsage

	var resultEvent map[string]any
	var finalStructuredEvent map[string]any
	var latestResultUsage map[string]any
	rateLimitRejected := false
	var rateLimitResetsAt *int64
	overageActive := false
	var overageResetsAt *int64

	var finalTimer *time.Timer
	closedAfterFinalCleanup := false
	var timerMu sync.Mutex

	handleEvent := func(ev map[string]any) {
		typ, _ := ev["type"].(string)
		switch typ {
		case "assistant":
			msgRaw, ok := ev["message"].(map[string]any)
			if !ok {
				return
			}
			usageRaw, _ := msgRaw["usage"].(map[string]any)
			nextUsage := toTokenUsageClaude(usageRaw)
			var messageID string
			if id, ok := msgRaw["id"].(string); ok && id != "" {
				messageID = id
				prev, exists := usageByMessageID[messageID]
				if !exists {
					cumulative.InputTokens += nextUsage.InputTokens
					cumulative.OutputTokens += nextUsage.OutputTokens
					cumulative.CacheReadTokens += nextUsage.CacheReadTokens
					cumulative.CacheCreationTokens += nextUsage.CacheCreationTokens
				} else {
					cumulative.InputTokens += nextUsage.InputTokens - prev.InputTokens
					cumulative.OutputTokens += nextUsage.OutputTokens - prev.OutputTokens
					cumulative.CacheReadTokens += nextUsage.CacheReadTokens - prev.CacheReadTokens
					cumulative.CacheCreationTokens += nextUsage.CacheCreationTokens - prev.CacheCreationTokens
				}
				usageByMessageID[messageID] = nextUsage
				lastAnonymousID = nil
				lastAnonymousUsage = nil
				pendingAnonymousUsage = nil
			} else {
				// anonymous handling simplified: treat as anonymous sequence
				// Check pending
				if pendingAnonymousUsage != nil && extendsUsage(nextUsage, *pendingAnonymousUsage) {
					messageID = fmt.Sprintf("assistant-%d", anonymousCount)
					anonymousCount++
					prev := *pendingAnonymousUsage
					cumulative.InputTokens += prev.InputTokens
					cumulative.OutputTokens += prev.OutputTokens
					cumulative.CacheReadTokens += prev.CacheReadTokens
					cumulative.CacheCreationTokens += prev.CacheCreationTokens
					usageByMessageID[messageID] = prev
					pendingAnonymousUsage = nil
					lastAnonymousID = &messageID
					lastAnonymousUsage = &nextUsage
					// now delta
					prev2 := usageByMessageID[messageID]
					cumulative.InputTokens += nextUsage.InputTokens - prev2.InputTokens
					cumulative.OutputTokens += nextUsage.OutputTokens - prev2.OutputTokens
					cumulative.CacheReadTokens += nextUsage.CacheReadTokens - prev2.CacheReadTokens
					cumulative.CacheCreationTokens += nextUsage.CacheCreationTokens - prev2.CacheCreationTokens
					usageByMessageID[messageID] = nextUsage
				} else if lastAnonymousID != nil && lastAnonymousUsage != nil && extendsUsage(nextUsage, *lastAnonymousUsage) {
					messageID = *lastAnonymousID
					prev := usageByMessageID[messageID]
					cumulative.InputTokens += nextUsage.InputTokens - prev.InputTokens
					cumulative.OutputTokens += nextUsage.OutputTokens - prev.OutputTokens
					cumulative.CacheReadTokens += nextUsage.CacheReadTokens - prev.CacheReadTokens
					cumulative.CacheCreationTokens += nextUsage.CacheCreationTokens - prev.CacheCreationTokens
					usageByMessageID[messageID] = nextUsage
					lastAnonymousUsage = &nextUsage
					pendingAnonymousUsage = nil
				} else if lastAnonymousID != nil && lastAnonymousUsage != nil && isSameUsage(nextUsage, *lastAnonymousUsage) {
					messageID = *lastAnonymousID
					if pendingAnonymousUsage == nil {
						pendingAnonymousUsage = &nextUsage
					}
				} else {
					messageID = fmt.Sprintf("assistant-%d", anonymousCount)
					anonymousCount++
					if pendingAnonymousUsage == nil {
						cumulative.InputTokens += nextUsage.InputTokens
						cumulative.OutputTokens += nextUsage.OutputTokens
						cumulative.CacheReadTokens += nextUsage.CacheReadTokens
						cumulative.CacheCreationTokens += nextUsage.CacheCreationTokens
					}
					usageByMessageID[messageID] = nextUsage
					lastAnonymousID = &messageID
					lastAnonymousUsage = &nextUsage
					pendingAnonymousUsage = nil
				}
			}
			if opts.OnUsage != nil {
				opts.OnUsage(cumulative)
			}
			if opts.OnMessage != nil {
				if contentRaw, ok := msgRaw["content"]; ok {
					if arr, ok := contentRaw.([]any); ok {
						for _, b := range arr {
							if m, ok := b.(map[string]any); ok && m["type"] == "text" {
								if t, ok := m["text"].(string); ok && strings.TrimSpace(t) != "" {
									opts.OnMessage(strings.TrimSpace(t))
								}
							}
						}
					}
				}
			}
		case "rate_limit_event":
			infoRaw, _ := ev["rate_limit_info"].(map[string]any)
			if infoRaw != nil {
				if v, ok := infoRaw["isUsingOverage"].(bool); ok {
					if v {
						overageActive = true
						if ra, ok := infoRaw["resetsAt"].(float64); ok {
							iv := int64(ra)
							overageResetsAt = &iv
						}
					} else {
						overageActive = false
						overageResetsAt = nil
					}
				}
				if status, ok := infoRaw["status"].(string); ok && status == "rejected" {
					rateLimitRejected = true
					if ra, ok := infoRaw["resetsAt"].(float64); ok {
						iv := int64(ra)
						rateLimitResetsAt = &iv
					} else {
						rateLimitResetsAt = nil
					}
				} else {
					rateLimitRejected = false
					rateLimitResetsAt = nil
				}
			}
		case "result":
			// store result event
			if isFinalStructuredResult(ev) {
				finalStructuredEvent = ev
				if ru, ok := ev["usage"].(map[string]any); ok {
					latestResultUsage = ru
				}
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
			} else {
				if resultEvent == nil || isErrorResult(ev) {
					resultEvent = ev
				}
				if ru, ok := ev["usage"].(map[string]any); ok {
					latestResultUsage = ru
				}
				if finalStructuredEvent == nil {
					if evIsError(ev) || ev["subtype"] != "success" || ev["structured_output"] != nil {
						resultEvent = ev
					}
				}
			}
		}
	}

	// stdout reader
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 10*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			raw := line + "\n"
			mu.Lock()
			stdoutTail = internal.AppendExitOutputTail(stdoutTail, raw)
			mu.Unlock()
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
	if logFile != nil {
		_ = logFile.Close()
	}

	if aborted || ctx.Err() != nil {
		return agent.AgentResult{}, fmt.Errorf("Agent was aborted")
	}
	if err != nil {
		// check exit code
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			stdoutCopy := stdoutTail
			stderrCopy := stderrBuilder.String()
			// overage callback before error dispatch
			if opts.OnOverage != nil {
				var ov *agent.UsageOverage
				if overageActive {
					var t *time.Time
					if overageResetsAt != nil {
						tt := time.Unix(*overageResetsAt, 0)
						t = &tt
					}
					ov = &agent.UsageOverage{ResumeAt: t}
				}
				opts.OnOverage(ov)
			}
			if !closedAfterFinalCleanup {
				failure := internal.DescribeChildProcessExit("claude", &code, stdoutCopy, stderrCopy)
				if rateLimitRejected {
					var ra *time.Time
					if rateLimitResetsAt != nil {
						tt := time.Unix(*rateLimitResetsAt, 0)
						ra = &tt
					}
					return agent.AgentResult{}, agent.NewRateLimitErrorWithMsg(fmt.Sprintf("claude usage limit reached"), failure.Detail, ra)
				}
				if isPermanentClaudeError(failure.ErrorOutput) {
					return agent.AgentResult{}, agent.NewPermanentError("claude credit balance too low - see gnhf.log", failure.Detail)
				}
				return agent.AgentResult{}, fmt.Errorf("%s", failure.Detail)
			}
		}
	}

	// overage callback
	if opts.OnOverage != nil {
		var ov *agent.UsageOverage
		if overageActive {
			var t *time.Time
			if overageResetsAt != nil {
				tt := time.Unix(*overageResetsAt, 0)
				t = &tt
			}
			ov = &agent.UsageOverage{ResumeAt: t}
		}
		opts.OnOverage(ov)
	}

	terminal := finalStructuredEvent
	if terminal == nil {
		terminal = resultEvent
	}
	if terminal == nil {
		return agent.AgentResult{}, fmt.Errorf("claude returned no result event")
	}
	if isError, _ := terminal["is_error"].(bool); isError {
		detail := fmt.Sprintf("claude reported error: %v", terminal)
		if rateLimitRejected {
			var ra *time.Time
			if rateLimitResetsAt != nil {
				tt := time.Unix(*rateLimitResetsAt, 0)
				ra = &tt
			}
			return agent.AgentResult{}, agent.NewRateLimitError(detail, ra)
		}
		return agent.AgentResult{}, fmt.Errorf("%s", detail)
	}
	if subtype, _ := terminal["subtype"].(string); subtype != "success" {
		detail := fmt.Sprintf("claude reported error: %v", terminal)
		if rateLimitRejected {
			var ra *time.Time
			if rateLimitResetsAt != nil {
				tt := time.Unix(*rateLimitResetsAt, 0)
				ra = &tt
			}
			return agent.AgentResult{}, agent.NewRateLimitError(detail, ra)
		}
		return agent.AgentResult{}, fmt.Errorf("%s", detail)
	}
	structured, ok := terminal["structured_output"]
	if !ok || structured == nil {
		return agent.AgentResult{}, fmt.Errorf("claude returned no structured_output")
	}
	m, ok := structured.(map[string]any)
	if !ok {
		// try to handle already typed?
		b, _ := json.Marshal(structured)
		var mm map[string]any
		_ = json.Unmarshal(b, &mm)
		m = mm
	}
	output, err := agent.ValidateAgentOutput(m, a.Schema)
	if err != nil {
		return agent.AgentResult{}, err
	}
	var usage agent.TokenUsage
	if latestResultUsage != nil {
		usage = toTokenUsageClaude(latestResultUsage)
	} else if u, ok := terminal["usage"].(map[string]any); ok {
		usage = toTokenUsageClaude(u)
	}
	if opts.OnUsage != nil {
		opts.OnUsage(usage)
	}
	return agent.AgentResult{Output: output, Usage: usage}, nil
}

func isFinalStructuredResult(ev map[string]any) bool {
	isErr, _ := ev["is_error"].(bool)
	if isErr {
		return false
	}
	subtype, _ := ev["subtype"].(string)
	if subtype != "success" {
		return false
	}
	so, ok := ev["structured_output"]
	return ok && so != nil
}
func isErrorResult(ev map[string]any) bool {
	isErr, _ := ev["is_error"].(bool)
	subtype, _ := ev["subtype"].(string)
	return isErr || subtype != "success"
}
func evIsError(ev map[string]any) bool {
	isErr, _ := ev["is_error"].(bool)
	return isErr
}
