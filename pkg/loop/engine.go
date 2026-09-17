package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"github.com/TheShiveshNetwork/syla/pkg/workspace"
)

// Agent is the interface loop needs from an agent.
// We reuse agent.Agent directly to avoid duplication; loop is still agnostic because it depends only on this interface.
type Agent = agent.Agent

// Config holds all knobs for a run.
type Config struct {
	Agent              Agent
	Workspace          workspace.Workspace
	WorkDir            string
	RunID              string
	RunDir             string
	MaxIterations      int
	MaxTokens          int
	BudgetUSD          float64
	MaxDuration        time.Duration
	MaxRateLimitWait   time.Duration
	MaxConsecutiveFailures int
	StopWhen           string
	Model              string
	PreventSleep       bool
}

// Prompt helper.
func Prompt(s string) string { return s }

// Result from a single Step.
type Result struct {
	Iteration int
	Output    agent.AgentOutput
	Usage     agent.TokenUsage
	Diff      string
}

// Event emitted on the event bus.
type Event struct {
	Type      string // IterationStarted, IterationCompleted, Failure, StopConditionMet, BudgetWarning, RateLimitWait
	Iteration int
	Message   string
	Result    *Result
	Err       error
}

// Engine is the loop state machine.
type Engine struct {
	cfg        Config
	budget     *Budget
	retry      *RetryPolicy
	stopConds  []StopCondition
	events     chan Event
	state      *RunState
	workspace  workspace.Workspace
	agent      Agent
	checkpoint *Checkpoint
	closed     bool
	cancel     context.CancelFunc
	cancelMu   sync.Mutex
}

func New(cfg Config) (*Engine, error) {
	if cfg.Agent == nil {
		return nil, fmt.Errorf("agent is required")
	}
	if cfg.Workspace == nil {
		cfg.Workspace, _ = workspace.Get("none", cfg.WorkDir)
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = "."
	}
	if cfg.RunID == "" {
		cfg.RunID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	if cfg.RunDir == "" {
		cfg.RunDir = filepath.Join(cfg.WorkDir, ".syla", "runs", cfg.RunID)
	}
	if err := os.MkdirAll(cfg.RunDir, 0755); err != nil {
		return nil, err
	}
	budget := &Budget{
		MaxTokens:   cfg.MaxTokens,
		MaxUSD:      cfg.BudgetUSD,
		MaxDuration: cfg.MaxDuration,
		Start:       time.Now(),
	}
	retry := NewRetryPolicy(cfg.MaxConsecutiveFailures, cfg.MaxRateLimitWait)
	// stop conditions
	var conds []StopCondition
	if cfg.MaxIterations > 0 {
		conds = append(conds, MaxIterations(cfg.MaxIterations))
	}
	if cfg.MaxDuration > 0 {
		conds = append(conds, MaxDuration(cfg.MaxDuration))
	}
	if cfg.MaxTokens > 0 || cfg.BudgetUSD > 0 {
		conds = append(conds, BudgetStop(budget))
	}
	if cfg.StopWhen != "" {
		if c, err := ParseStopWhen(cfg.StopWhen, budget); err == nil && c != nil {
			conds = append(conds, c)
		} else if err != nil {
			return nil, fmt.Errorf("invalid stop condition: %w", err)
		}
	}
	// load checkpoint if exists
	cp, _ := LoadCheckpoint(cfg.RunDir)
	state := &RunState{
		Iteration: 0,
		Elapsed:   0,
	}
	started := time.Now()
	providerName := ""
	if cfg.Agent != nil {
		providerName = cfg.Agent.Name()
	}
	if cp != nil {
		state.Iteration = cp.Iteration
		state.SuccessCount = cp.Successes
		state.FailCount = cp.Failures
		state.ConsecutiveFailures = cp.Consecutive
		state.TotalTokens = cp.TotalTokens
		started = cp.StartedAt
		budget.TotalTokens = cp.TotalTokens
		budget.TotalUSD = cp.TotalUSD
		budget.Start = started
		if cp.Provider != "" {
			providerName = cp.Provider
		}
	}
	e := &Engine{
		cfg:       cfg,
		budget:    budget,
		retry:     retry,
		stopConds: conds,
		events:    make(chan Event, 64),
		state:     state,
		workspace: cfg.Workspace,
		agent:     cfg.Agent,
		checkpoint: &Checkpoint{
			RunID:     cfg.RunID,
			RunDir:    cfg.RunDir,
			Provider:  providerName,
			StartedAt: started,
			Status:    "running",
		},
	}
	// also track no_diff condition state via workspace
	return e, nil
}

func (e *Engine) Events() <-chan Event { return e.events }

func (e *Engine) emit(ev Event) {
	if e.closed {
		return
	}
	defer func() { recover() }()
	select {
	case e.events <- ev:
	default:
	}
}

func (e *Engine) ShouldContinue(ctx context.Context) bool {
	if e.closed {
		return false
	}
	e.state.Elapsed = time.Since(e.budget.Start)
	// check stop conditions
	for _, c := range e.stopConds {
		if ok, reason, _ := c.ShouldStop(ctx, e.state); ok {
			e.emit(Event{Type: "StopConditionMet", Message: reason})
			e.checkpoint.StopReason = reason
			e.checkpoint.Status = "completed"
			_ = SaveCheckpoint(e.cfg.RunDir, e.checkpoint)
			return false
		}
	}
	if e.state.ShouldFullyStop {
		return false
	}
	if ok, _ := e.budget.Exceeded(); ok {
		return false
	}
	return true
}

func (e *Engine) RecordFailure(err error) {
	e.state.FailCount++
	e.state.ConsecutiveFailures++
	shouldAbort, wait := e.retry.RecordFailure(err)
	e.emit(Event{Type: "Failure", Err: err, Message: err.Error()})
	if wait > 0 {
		e.emit(Event{Type: "RateLimitWait", Message: fmt.Sprintf("waiting %s for rate limit", wait)})
	}
	if shouldAbort {
		e.checkpoint.Status = "failed"
		e.checkpoint.StopReason = err.Error()
	}
	_ = e.persist()
}

func (e *Engine) persist() error {
	cp := e.checkpoint
	cp.Iteration = e.state.Iteration
	cp.Successes = e.state.SuccessCount
	cp.Failures = e.state.FailCount
	cp.Consecutive = e.state.ConsecutiveFailures
	cp.TotalTokens = e.state.TotalTokens
	cp.TotalUSD = e.state.TotalUSD
	return SaveCheckpoint(e.cfg.RunDir, cp)
}

// Step runs a single iteration with the given prompt.
func (e *Engine) Step(ctx context.Context, prompt string) (*Result, error) {
	if !e.ShouldContinue(ctx) {
		return nil, fmt.Errorf("loop should not continue")
	}
	iter := e.state.Iteration + 1
	e.emit(Event{Type: "IterationStarted", Iteration: iter, Message: prompt})

	snapshotID, _ := e.workspace.Snapshot(ctx)

	// run agent
	opts := agent.RunOptions{
		Model:   e.cfg.Model,
		LogPath: filepath.Join(e.cfg.RunDir, fmt.Sprintf("iter-%d.log", iter)),
		OnUsage: func(u agent.TokenUsage) {
			e.emit(Event{Type: "BudgetWarning", Message: fmt.Sprintf("tokens %d", u.InputTokens+u.OutputTokens)})
		},
		OnMessage: func(s string) {
			e.emit(Event{Type: "IterationCompleted", Message: s})
		},
	}
	result, err := e.agent.Run(ctx, prompt, e.cfg.WorkDir, opts)
	if err != nil {
		// handle rate limit wait
		if wait, ok := e.retry.ShouldWait(err); ok {
			if wait > 0 {
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			// retry same iteration without counting as failure? per spec, rate limit is not counted
			// rollback workspace
			_ = e.workspace.Rollback(ctx, snapshotID)
			e.emit(Event{Type: "RateLimitWait", Message: fmt.Sprintf("rate limited, waited %s", wait)})
			return nil, err
		}
		// check if should abort
		shouldAbort, _ := e.retry.RecordFailure(err)
		e.state.FailCount++
		e.state.ConsecutiveFailures = e.retry.ConsecutiveFailures
		e.state.Iteration = iter
		e.emit(Event{Type: "Failure", Iteration: iter, Err: err})
		_ = e.workspace.Rollback(ctx, snapshotID)
		_ = e.persist()
		if shouldAbort {
			e.checkpoint.Status = "failed"
			_ = SaveCheckpoint(e.cfg.RunDir, e.checkpoint)
		}
		return nil, err
	}

	// success
	e.retry.RecordSuccess()
	e.state.Iteration = iter
	e.state.SuccessCount++
	e.state.ConsecutiveFailures = 0
	e.state.TotalTokens += result.Usage.InputTokens + result.Usage.OutputTokens
	e.state.LastOutput = result.Output.Summary
	e.state.LastSummary = result.Output.Summary
	e.state.ShouldFullyStop = result.Output.ShouldFullyStop != nil && *result.Output.ShouldFullyStop
	e.budget.AddTokens(result.Usage.InputTokens + result.Usage.OutputTokens)
	// workspace diff & commit
	diff, _ := e.workspace.Diff(ctx, snapshotID)
	e.state.WorkspaceDiff = diff
	if diff != "" {
		_ = e.workspace.Commit(ctx, fmt.Sprintf("syla iter %d: %s", iter, result.Output.Summary))
	}
	// evaluate stop conditions that depend on diff/output
	for _, c := range e.stopConds {
		if ok, reason, _ := c.ShouldStop(ctx, e.state); ok {
			e.checkpoint.StopReason = reason
			e.checkpoint.Status = "completed"
			_ = e.persist()
			e.emit(Event{Type: "StopConditionMet", Iteration: iter, Message: reason})
			break
		}
	}
	_ = e.persist()
	_ = SaveIteration(e.cfg.RunDir, iter, prompt, result.Output.Summary, map[string]int{"input": result.Usage.InputTokens, "output": result.Usage.OutputTokens})
	res := &Result{
		Iteration: iter,
		Output:    result.Output,
		Usage:     result.Usage,
		Diff:      diff,
	}
	e.emit(Event{Type: "IterationCompleted", Iteration: iter, Result: res})
	return res, nil
}

func (e *Engine) Run(ctx context.Context, taskBody string) error {
	// hard wall for max_duration: derive deadline from budget
	if e.cfg.MaxDuration > 0 {
		deadline := e.budget.Start.Add(e.cfg.MaxDuration)
		remaining := time.Until(deadline)
		if remaining <= 0 {
			e.checkpoint.Status = "completed"
			e.checkpoint.StopReason = fmt.Sprintf("max_duration(%s)", e.cfg.MaxDuration)
			_ = SaveCheckpoint(e.cfg.RunDir, e.checkpoint)
			return nil
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
		e.cancelMu.Lock()
		e.cancel = cancel
		e.cancelMu.Unlock()
		defer func() {
			e.cancelMu.Lock()
			e.cancel = nil
			e.cancelMu.Unlock()
		}()
	} else {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		e.cancelMu.Lock()
		e.cancel = cancel
		e.cancelMu.Unlock()
		defer func() {
			e.cancelMu.Lock()
			e.cancel = nil
			e.cancelMu.Unlock()
			cancel()
		}()
	}
	defer func() {
		if !e.closed {
			e.closed = true
			close(e.events)
		}
	}()
	for e.ShouldContinue(ctx) {
		// check if deadline exceeded before starting next iteration
		select {
		case <-ctx.Done():
			// hard timeout hit mid-run
			e.checkpoint.Status = "completed"
			e.checkpoint.StopReason = fmt.Sprintf("max_duration(%s)", e.cfg.MaxDuration)
			_ = SaveCheckpoint(e.cfg.RunDir, e.checkpoint)
			_ = writeAbortSummary(e.cfg.RunDir, e)
			return nil
		default:
		}
		prompt := taskBody
		if _, err := e.Step(ctx, prompt); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				e.checkpoint.Status = "completed"
				e.checkpoint.StopReason = fmt.Sprintf("max_duration(%s)", e.cfg.MaxDuration)
				_ = SaveCheckpoint(e.cfg.RunDir, e.checkpoint)
				return nil
			}
			if e.checkpoint.Status == "failed" || e.checkpoint.Status == "aborted" {
				return err
			}
			continue
		}
		if e.state.ShouldFullyStop {
			break
		}
	}
	return nil
}

func (e *Engine) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	close(e.events)
	if closer, ok := e.agent.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	return nil
}

func (e *Engine) Abort() error {
	if e.closed {
		return nil
	}
	// don't overwrite already completed/failed runs
	if e.checkpoint.Status == "completed" || e.checkpoint.Status == "failed" || e.checkpoint.Status == "aborted" {
		return nil
	}
	e.cancelMu.Lock()
	if e.cancel != nil {
		e.cancel()
	}
	e.cancelMu.Unlock()
	e.checkpoint.Status = "aborted"
	e.checkpoint.StopReason = "aborted"
	e.state.StopReason = "aborted"
	_ = SaveCheckpoint(e.cfg.RunDir, e.checkpoint)
	_ = writeAbortSummary(e.cfg.RunDir, e)
	return e.Close()
}

func writeAbortSummary(runDir string, e *Engine) error {
	st := e.Status()
	summary := fmt.Sprintf("run %s aborted after %d iterations, elapsed %s\n", e.RunID(), st.Iteration, st.Elapsed)
	return os.WriteFile(filepath.Join(runDir, "exit-summary.txt"), []byte(summary), 0644)
}

func (e *Engine) Status() RunState {
	// update elapsed
	e.state.Elapsed = time.Since(e.budget.Start)
	e.state.StopReason = e.checkpoint.StopReason
	return *e.state
}

func (e *Engine) RunID() string { return e.cfg.RunID }
func (e *Engine) RunDir() string { return e.cfg.RunDir }

func (e *Engine) SetStatusForTUI(iter int, status string) {
	e.state.Iteration = iter
	e.checkpoint.Status = status
}
