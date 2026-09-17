package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/TheShiveshNetwork/syla/pkg/agent"
	"github.com/TheShiveshNetwork/syla/pkg/loop"
	"github.com/TheShiveshNetwork/syla/pkg/tui"
	"github.com/TheShiveshNetwork/syla/pkg/workspace"
)

// This example shows how to use syla as an SDK for a scraping bot that runs overnight.
// The scraping logic itself is bespoke Go code; syla provides the reliability harness:
// - loop retries, budget enforcement, checkpointing
// - keep-awake lock (via loop.Config.PreventSleep)
// - beautiful TUI via tui.Attach

func main() {
	// Use a fake agent for demo; replace with provider.NewAcpAgent or provider.NewClaudeAgent etc.
	// For a real scraper you might use an agent that can browse, or you might not need an LLM at all
	// and just use the loop as a retry harness around your own HTTP calls.

	// Example 1: Use syla with a real agent (e.g., codex) to generate scraping plans
	// agent, _ := provider.CreateAgent("codex", provider.RunInfo{RunID: "scraper", RunDir: ".syla/runs/scraper"}, nil, nil, provider.CreateAgentOptions{})

	// Example 2: Use a stub agent for demo; the loop still gives you budget, retry, and TUI
	ws, _ := workspace.Get("none", ".")
	fake := &stubAgent{}

	eng, err := loop.New(loop.Config{
		Agent:         fake,
		Workspace:     ws,
		WorkDir:       ".",
		RunID:         "scraper-demo",
		MaxIterations: 5,
		BudgetUSD:     5,
		PreventSleep:  true,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer eng.Close()

	// Attach TUI in background (optional, comment out for headless)
	go func() {
		_ = tui.Attach(eng)
	}()

	ctx := context.Background()
	urls := []string{
		"https://example.com/page1",
		"https://example.com/page2",
		"https://example.com/page3",
	}

	for _, url := range urls {
		if !eng.ShouldContinue(ctx) {
			break
		}
		prompt := fmt.Sprintf("scrape %s, extract all product rows as JSON", url)
		result, err := eng.Step(ctx, prompt)
		if err != nil {
			log.Printf("step failed for %s: %v", url, err)
			eng.RecordFailure(err)
			continue
		}
		// Your own code: persist rows, decide next URL
		fmt.Printf("Scraped %s: %s (tokens %d)\n", url, result.Output.Summary, result.Usage.InputTokens+result.Usage.OutputTokens)
		// Example: write to file
		_ = os.WriteFile(fmt.Sprintf("rows-%d.json", result.Iteration), []byte(result.Output.Summary), 0644)
	}

	fmt.Println("Scraping complete. Run `syla status` to see summary.")
}

// stubAgent is a no-op agent for demo; replace with a real provider agent.
type stubAgent struct{}

func (s *stubAgent) Name() string { return "stub-scraper" }
func (s *stubAgent) Run(ctx context.Context, prompt string, cwd string, opts agent.RunOptions) (agent.AgentResult, error) {
	// In a real scraper, you might do HTTP fetching here and return structured output
	// For demo, just return fake success
	return agent.AgentResult{
		Output: agent.AgentOutput{Success: true, Summary: `[{"name":"Widget","price":9.99}]`},
		Usage:  agent.TokenUsage{InputTokens: 10, OutputTokens: 20},
	}, nil
}
