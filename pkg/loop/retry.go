package loop

import (
	"errors"
	"time"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
)

// RetryPolicy handles consecutive failures and rate-limit waits.
type RetryPolicy struct {
	MaxConsecutiveFailures int
	MaxRateLimitWait       time.Duration
	ConsecutiveFailures   int
}

func NewRetryPolicy(maxFailures int, maxWait time.Duration) *RetryPolicy {
	if maxFailures <= 0 {
		maxFailures = 3
	}
	if maxWait <= 0 {
		maxWait = 24 * time.Hour
	}
	return &RetryPolicy{
		MaxConsecutiveFailures: maxFailures,
		MaxRateLimitWait:       maxWait,
	}
}

func (r *RetryPolicy) RecordSuccess() {
	r.ConsecutiveFailures = 0
}

func (r *RetryPolicy) RecordFailure(err error) (shouldAbort bool, wait time.Duration) {
	// Rate-limited errors are not counted toward consecutive failures; we wait instead.
	var rl *agent.RateLimitAgentError
	if errors.As(err, &rl) {
		if rl.ResumeAt != nil {
			wait = time.Until(*rl.ResumeAt) + time.Second
			if wait < 0 {
				wait = 0
			}
			if wait > r.MaxRateLimitWait {
				return true, wait
			}
			return false, wait
		}
		// no resume time: treat as retryable without counting
		return false, 0
	}
	// Permanent errors abort immediately without retry.
	if agent.IsPermanent(err) {
		return true, 0
	}
	r.ConsecutiveFailures++
	if r.ConsecutiveFailures >= r.MaxConsecutiveFailures {
		return true, 0
	}
	return false, 0
}

func (r *RetryPolicy) ShouldWait(err error) (time.Duration, bool) {
	var rl *agent.RateLimitAgentError
	if errors.As(err, &rl) {
		if rl.ResumeAt != nil {
			wait := time.Until(*rl.ResumeAt) + time.Second
			if wait > r.MaxRateLimitWait {
				return wait, false
			}
			return wait, true
		}
		return 0, true
	}
	return 0, false
}
