package agent

import (
	"errors"
	"fmt"
	"time"
)

// PermanentAgentError indicates an auth/config failure that retrying will not fix.
type PermanentAgentError struct {
	Msg    string
	Detail string
}

func (e *PermanentAgentError) Error() string { return e.Msg }
func (e *PermanentAgentError) Unwrap() error { return errors.New(e.Detail) }

func NewPermanentError(msg, detail string) *PermanentAgentError {
	return &PermanentAgentError{Msg: msg, Detail: detail}
}

// RateLimitAgentError indicates the provider rejected the request due to quota exhaustion.
type RateLimitAgentError struct {
	Msg      string
	Detail   string
	ResumeAt *time.Time
}

func (e *RateLimitAgentError) Error() string { return e.Msg }
func (e *RateLimitAgentError) Unwrap() error { return errors.New(e.Detail) }

func NewRateLimitError(detail string, resumeAt *time.Time) *RateLimitAgentError {
	until := ""
	if resumeAt != nil {
		until = fmt.Sprintf(" until %s", resumeAt.Format(time.RFC3339))
	}
	return &RateLimitAgentError{
		Msg:      fmt.Sprintf("usage limit reached%s", until),
		Detail:   detail,
		ResumeAt: resumeAt,
	}
}

func NewRateLimitErrorWithMsg(msg, detail string, resumeAt *time.Time) *RateLimitAgentError {
	return &RateLimitAgentError{Msg: msg, Detail: detail, ResumeAt: resumeAt}
}

// IsPermanent reports whether err is a PermanentAgentError.
func IsPermanent(err error) bool {
	var t *PermanentAgentError
	return errors.As(err, &t)
}

// IsRateLimit reports whether err is a RateLimitAgentError.
func IsRateLimit(err error) bool {
	var t *RateLimitAgentError
	return errors.As(err, &t)
}
