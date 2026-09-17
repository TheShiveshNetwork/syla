package loop

import (
	"time"
)

// Budget tracks token and cost limits.
type Budget struct {
	MaxTokens          int
	MaxUSD             float64
	MaxDuration        time.Duration
	Start              time.Time
	TotalTokens        int
	TotalUSD           float64
	RateCostPerToken   float64 // optional, default 0
}

func USD(v float64) float64 { return v }

func (b *Budget) AddTokens(n int) {
	b.TotalTokens += n
}

func (b *Budget) AddCost(usd float64) {
	b.TotalUSD += usd
}

func (b *Budget) ExceededTokens() bool {
	if b.MaxTokens <= 0 {
		return false
	}
	return b.TotalTokens >= b.MaxTokens
}

func (b *Budget) ExceededUSD() bool {
	if b.MaxUSD <= 0 {
		return false
	}
	return b.TotalUSD >= b.MaxUSD
}

func (b *Budget) ExceededDuration() bool {
	if b.MaxDuration <= 0 {
		return false
	}
	return time.Since(b.Start) >= b.MaxDuration
}

func (b *Budget) Exceeded() (bool, string) {
	if b.ExceededTokens() {
		return true, "max_tokens"
	}
	if b.ExceededUSD() {
		return true, "budget"
	}
	if b.ExceededDuration() {
		return true, "max_duration"
	}
	return false, ""
}
