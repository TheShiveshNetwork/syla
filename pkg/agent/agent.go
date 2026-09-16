package agent

import (
	"context"
	"time"
)

// AgentOutput is the structured JSON the agent must return at the end of each iteration.
type AgentOutput struct {
	Success         bool     `json:"success"`
	Summary         string   `json:"summary"`
	KeyChangesMade  []string `json:"key_changes_made"`
	KeyLearnings    []string `json:"key_learnings"`
	ShouldFullyStop *bool    `json:"should_fully_stop,omitempty"`

	// Extras holds any additional commit-message fields (e.g. type, scope) injected via schema.
	Extras map[string]string `json:"-"`
}

// TokenUsage is mutually-exclusive buckets so totals can sum without double-counting.
type TokenUsage struct {
	InputTokens         int  `json:"inputTokens"`
	OutputTokens        int  `json:"outputTokens"`
	CacheReadTokens     int  `json:"cacheReadTokens"`
	CacheCreationTokens int  `json:"cacheCreationTokens"`
	Estimated           bool `json:"estimated,omitempty"`
}

// UsageOverage indicates the provider served the request from paid extra usage
// because the included window was exhausted. Reported via OnOverage orthogonal to result.
type UsageOverage struct {
	ResumeAt *time.Time `json:"resumeAt"`
}

// AgentResult is the terminal result of a single agent iteration.
type AgentResult struct {
	Output AgentOutput `json:"output"`
	Usage  TokenUsage  `json:"usage"`
}

// RunOptions configures a single Run invocation.
type RunOptions struct {
	Model     string
	OnUsage   func(TokenUsage)
	OnMessage func(string)
	OnOverage func(*UsageOverage)
	LogPath   string
}

// Agent is the interface every adapter must satisfy. Close is optional.
type Agent interface {
	Name() string
	Run(ctx context.Context, prompt string, cwd string, opts RunOptions) (AgentResult, error)
}

// Closer is implemented by agents that need explicit shutdown (e.g. ACP, opencode, rovodev).
type Closer interface {
	Close() error
}
