package e2e

import (
	"errors"
	"testing"
	"time"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"github.com/TheShiveshNetwork/syla/pkg/loop"
)

func TestRetryPolicy(t *testing.T) {
	r := loop.NewRetryPolicy(3, 24*time.Hour)
	if r.MaxConsecutiveFailures != 3 {
		t.Fatalf("max failures")
	}
	// permanent error should abort
	perm := agent.NewPermanentError("perm", "detail")
	shouldAbort, wait := r.RecordFailure(perm)
	if !shouldAbort || wait != 0 {
		t.Fatalf("perm should abort")
	}
	// reset
	r = loop.NewRetryPolicy(3, 24*time.Hour)
	// normal failures
	for i := 0; i < 2; i++ {
		shouldAbort, _ := r.RecordFailure(errors.New("fail"))
		if shouldAbort {
			t.Fatalf("should not abort at %d", i)
		}
	}
	shouldAbort, _ = r.RecordFailure(errors.New("fail"))
	if !shouldAbort {
		t.Fatalf("should abort at 3")
	}
	r.RecordSuccess()
	if r.ConsecutiveFailures != 0 {
		t.Fatalf("should reset after success")
	}
}

func TestRateLimitWait(t *testing.T) {
	r := loop.NewRetryPolicy(3, 1*time.Hour)
	resume := time.Now().Add(10 * time.Minute)
	rl := agent.NewRateLimitError("limited", &resume)
	shouldAbort, wait := r.RecordFailure(rl)
	if shouldAbort {
		t.Fatalf("should not abort rate limit")
	}
	if wait <= 0 {
		t.Fatalf("should wait")
	}
	// exceed max wait
	r2 := loop.NewRetryPolicy(3, 1*time.Minute)
	resumeFar := time.Now().Add(2 * time.Hour)
	rl2 := agent.NewRateLimitError("limited", &resumeFar)
	shouldAbort, wait = r2.RecordFailure(rl2)
	if !shouldAbort {
		t.Fatalf("should abort when wait exceeds max")
	}
	if wait <= 0 {
		t.Fatalf("wait should be set")
	}
}
