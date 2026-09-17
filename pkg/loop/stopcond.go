package loop

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// StopCondition decides when the loop should stop.
type StopCondition interface {
	Name() string
	ShouldStop(ctx context.Context, state *RunState) (bool, string, error)
}

// RunState is a snapshot of loop progress for stop conditions.
type RunState struct {
	Iteration           int
	SuccessCount        int
	FailCount           int
	ConsecutiveFailures int
	TotalTokens         int
	TotalUSD            float64
	Elapsed             time.Duration
	LastOutput          string
	LastSummary         string
	WorkspaceDiff       string
	ShouldFullyStop     bool
	StopReason          string
}

// Built-in stop conditions.

type maxIterations struct{ n int }

func MaxIterations(n int) StopCondition { return &maxIterations{n: n} }
func (m *maxIterations) Name() string { return fmt.Sprintf("max_iterations(%d)", m.n) }
func (m *maxIterations) ShouldStop(ctx context.Context, s *RunState) (bool, string, error) {
	if m.n <= 0 {
		return false, "", nil
	}
	if s.Iteration >= m.n {
		return true, m.Name(), nil
	}
	return false, "", nil
}

type maxDuration struct{ d time.Duration }

func MaxDuration(d time.Duration) StopCondition { return &maxDuration{d: d} }
func (m *maxDuration) Name() string { return fmt.Sprintf("max_duration(%s)", m.d) }
func (m *maxDuration) ShouldStop(ctx context.Context, s *RunState) (bool, string, error) {
	if m.d <= 0 {
		return false, "", nil
	}
	if s.Elapsed >= m.d {
		return true, m.Name(), nil
	}
	return false, "", nil
}

type noDiffFor struct {
	n       int
	counter int
}

func NoDiffFor(n int) StopCondition { return &noDiffFor{n: n} }
func (m *noDiffFor) Name() string { return fmt.Sprintf("no_diff_for(%d)", m.n) }
func (m *noDiffFor) ShouldStop(ctx context.Context, s *RunState) (bool, string, error) {
	if strings.TrimSpace(s.WorkspaceDiff) == "" {
		m.counter++
	} else {
		m.counter = 0
	}
	if m.n > 0 && m.counter >= m.n {
		return true, m.Name(), nil
	}
	return false, "", nil
}

type outputMatches struct {
	re *regexp.Regexp
	raw string
}

func OutputMatches(pattern string) (StopCondition, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &outputMatches{re: re, raw: pattern}, nil
}
func (o *outputMatches) Name() string { return fmt.Sprintf("output_matches(%q)", o.raw) }
func (o *outputMatches) ShouldStop(ctx context.Context, s *RunState) (bool, string, error) {
	if o.re.MatchString(s.LastOutput) || o.re.MatchString(s.LastSummary) {
		return true, o.Name(), nil
	}
	return false, "", nil
}

type budgetStop struct{ budget *Budget }

func BudgetStop(b *Budget) StopCondition { return &budgetStop{budget: b} }
func (b *budgetStop) Name() string { return "budget" }
func (b *budgetStop) ShouldStop(ctx context.Context, s *RunState) (bool, string, error) {
	if b.budget == nil {
		return false, "", nil
	}
	if ok, reason := b.budget.Exceeded(); ok {
		return true, reason, nil
	}
	return false, "", nil
}

type customScript struct {
	path string
}

func CustomScript(path string) StopCondition { return &customScript{path: path} }
func (c *customScript) Name() string { return fmt.Sprintf("custom(%s)", c.path) }
func (c *customScript) ShouldStop(ctx context.Context, s *RunState) (bool, string, error) {
	// invoke script with run state JSON on stdin, exit 0 = stop
	// For now, simple: run script, if exit 0 then stop
	cmd := exec.CommandContext(ctx, c.path)
	// pass state via env? For now just run and check exit code
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 0 {
				return true, c.Name(), nil
			}
			return false, "", nil
		}
		return false, "", err
	}
	return true, c.Name(), nil
}

// ParseStopWhen parses the --stop-when / stop_on frontmatter value.
func ParseStopWhen(s string, budget *Budget) (StopCondition, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "never" {
		return nil, nil
	}
	if strings.HasPrefix(s, "max_iterations(") || strings.HasPrefix(s, "max_iterations:") {
		// already handled via MaxIterations, but support here
		return nil, nil
	}
	if strings.HasPrefix(s, "no_diff_for(") {
		var n int
		if _, err := fmt.Sscanf(s, "no_diff_for(%d)", &n); err == nil {
			return NoDiffFor(n), nil
		}
	}
	if strings.HasPrefix(s, "output_matches(") {
		inner := strings.TrimPrefix(s, "output_matches(")
		inner = strings.TrimSuffix(inner, ")")
		inner = strings.Trim(inner, `" '`)
		return OutputMatches(inner)
	}
	if s == "budget" || s == "budget_exceeded" {
		return BudgetStop(budget), nil
	}
	// treat as script path if contains / or .
	if strings.Contains(s, "/") || strings.Contains(s, ".sh") || strings.Contains(s, ".py") {
		return CustomScript(s), nil
	}
	return nil, fmt.Errorf("unknown stop condition: %q", s)
}
