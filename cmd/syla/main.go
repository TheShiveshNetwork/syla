package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/TheShiveshNetwork/syla/pkg/agent/provider"
	"github.com/TheShiveshNetwork/syla/pkg/daemon"
	"github.com/TheShiveshNetwork/syla/pkg/loop"
	"github.com/TheShiveshNetwork/syla/pkg/taskfile"
	"github.com/TheShiveshNetwork/syla/pkg/tui"
	"github.com/TheShiveshNetwork/syla/pkg/workspace"
)

var (
	flagAgent            string
	flagModel            string
	flagMaxIterations    int
	flagMaxTokens        int
	flagMaxRateLimitWait string
	flagStopWhen         string
	flagPreventSleep     bool
	flagWorkspace        string
	flagBudgetUSD        float64
	flagMaxDuration      string
	daemonRunID          string
	daemonRunDir         string
)

func main() {
	// Check if invoked as daemon
	if isDaemon, runID, runDir := checkDaemon(); isDaemon {
		if err := runDaemon(runID, runDir); err != nil {
			fmt.Fprintf(os.Stderr, "daemon error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	root := &cobra.Command{
		Use:   "syla",
		Short: "see you later, alligator - supervised agent loops",
		Run: func(cmd *cobra.Command, args []string) {
			// bare `syla` shows status/dashboard via TUI or status list
			_ = runStatus(cmd, args)
		},
	}

	// Global flags
	root.PersistentFlags().StringVar(&flagAgent, "agent", "", "agent to use (claude, codex, copilot, cursor, pi, opencode, rovodev, gemini, acp:<target>)")
	root.PersistentFlags().StringVar(&flagModel, "model", "", "model override")
	root.PersistentFlags().IntVar(&flagMaxIterations, "max-iterations", 0, "max iterations (0 = unlimited)")
	root.PersistentFlags().IntVar(&flagMaxTokens, "max-tokens", 0, "max tokens (0 = unlimited)")
	root.PersistentFlags().StringVar(&flagMaxRateLimitWait, "max-rate-limit-wait", "24h", "max wait for rate limit (e.g. 24h)")
	root.PersistentFlags().StringVar(&flagStopWhen, "stop-when", "", "stop condition (e.g. no_diff_for(3), output_matches(\"DONE\"))")
	root.PersistentFlags().BoolVar(&flagPreventSleep, "prevent-sleep", true, "hold sleep-prevention lock while running")
	root.PersistentFlags().StringVar(&flagWorkspace, "workspace", "", "workspace kind (git, none, file-snapshot)")
	root.PersistentFlags().Float64Var(&flagBudgetUSD, "budget-usd", 0, "budget in USD")
	root.PersistentFlags().StringVar(&flagMaxDuration, "max-duration", "", "max duration (e.g. 8h)")

	runCmd := &cobra.Command{
		Use:   "run <task.md>",
		Short: "run a task file in a supervised loop",
		Args:  cobra.ExactArgs(1),
		RunE:  runRun,
	}
	runCmd.Flags().StringVar(&flagAgent, "agent", "", "agent to use")
	runCmd.Flags().StringVar(&flagModel, "model", "", "model override")
	runCmd.Flags().IntVar(&flagMaxIterations, "max-iterations", 0, "max iterations")
	runCmd.Flags().IntVar(&flagMaxTokens, "max-tokens", 0, "max tokens")
	runCmd.Flags().StringVar(&flagMaxRateLimitWait, "max-rate-limit-wait", "24h", "max wait for rate limit")
	runCmd.Flags().StringVar(&flagStopWhen, "stop-when", "", "stop condition")
	runCmd.Flags().BoolVar(&flagPreventSleep, "prevent-sleep", true, "hold sleep-prevention lock")
	// also allow workspace etc. via taskfile, but flags override

	attachCmd := &cobra.Command{
		Use:   "attach <run-id>",
		Short: "reattach TUI to a running run",
		Args:  cobra.ExactArgs(1),
		RunE:  runAttach,
	}
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "list active and recent runs",
		RunE:  runStatus,
	}
	stopCmd := &cobra.Command{
		Use:   "stop <run-id>",
		Short: "stop a run for good",
		Args:  cobra.ExactArgs(1),
		RunE:  runStop,
	}
	closeCmd := &cobra.Command{
		Use:   "close <session-id>",
		Short: "abort and close a daemon session safely (aborted status)",
		Args:  cobra.ExactArgs(1),
		RunE:  runClose,
	}
	// alias: abort
	abortCmd := &cobra.Command{
		Use:   "abort <session-id>",
		Short: "abort a session (alias for close)",
		Args:  cobra.ExactArgs(1),
		RunE:  runClose,
	}

	// Hidden daemon flag for internal use
	root.Flags().StringVar(&daemonRunID, "daemon", "", "internal daemon mode")
	_ = root.Flags().MarkHidden("daemon")
	root.Flags().StringVar(&daemonRunDir, "daemon-dir", "", "internal")
	_ = root.Flags().MarkHidden("daemon-dir")

	root.AddCommand(runCmd, attachCmd, statusCmd, stopCmd, closeCmd, abortCmd)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func checkDaemon() (isDaemon bool, runID, runDir string) {
	for i, a := range os.Args {
		if a == "--daemon" && i+2 < len(os.Args) {
			return true, os.Args[i+1], os.Args[i+2]
		}
	}
	return false, "", ""
}

func runDaemon(runID, runDir string) error {
	// Use daemon's persisted config mechanism; just call RunDaemon with empty agent/ws to trigger load from daemon.json
	// We still need to provide WorkDir for workspace resolution
	workDir := "."
	// Try to load task.md for fallback, but daemon will handle full reload from daemon.json
	return daemon.RunDaemon(runID, runDir, nil, nil, "", loop.Config{WorkDir: workDir})
}

func runRun(cmd *cobra.Command, args []string) error {
	taskPath := args[0]
	tf, err := taskfile.Parse(taskPath)
	if err != nil {
		return fmt.Errorf("parse taskfile: %w", err)
	}

	workDir, _ := os.Getwd()
	if tf.Frontmatter.Workspace != "" {
		// workspace kind from file
	}
	wsKind := flagWorkspace
	if wsKind == "" {
		wsKind = tf.Frontmatter.Workspace
		if wsKind == "" {
			wsKind = "none"
		}
	}
	ws, err := workspace.Get(wsKind, workDir)
	if err != nil {
		return err
	}

	agentName := flagAgent
	if agentName == "" {
		agentName = tf.Frontmatter.Agent
		if agentName == "" {
			return fmt.Errorf("--agent is required (or set in taskfile frontmatter)")
		}
	}
	model := flagModel
	if model == "" {
		model = tf.Frontmatter.Model
	}
	maxIters := flagMaxIterations
	if maxIters == 0 && tf.Frontmatter.MaxIterations != nil {
		maxIters = *tf.Frontmatter.MaxIterations
	}
	maxTokens := flagMaxTokens
	if maxTokens == 0 && tf.Frontmatter.MaxTokens != nil {
		maxTokens = *tf.Frontmatter.MaxTokens
	}
	stopWhen := flagStopWhen
	if stopWhen == "" {
		stopWhen = tf.Frontmatter.StopWhen
		if stopWhen == "" {
			stopWhen = tf.Frontmatter.StopOn
		}
	}
	preventSleep := flagPreventSleep
	if cmd.Flags().Changed("prevent-sleep") == false && tf.Frontmatter.PreventSleep != nil {
		preventSleep = *tf.Frontmatter.PreventSleep
	} else if tf.Frontmatter.KeepAwake != nil && !cmd.Flags().Changed("prevent-sleep") {
		preventSleep = *tf.Frontmatter.KeepAwake
	}
	maxDuration := tf.Frontmatter.MaxDuration
	if flagMaxDuration != "" {
		maxDuration = flagMaxDuration
	}
	var maxDur time.Duration
	if maxDuration != "" {
		maxDur, _ = time.ParseDuration(maxDuration)
	}
	maxRateLimitWait := flagMaxRateLimitWait
	if maxRateLimitWait == "" {
		maxRateLimitWait = tf.Frontmatter.MaxRateLimitWait
		if maxRateLimitWait == "" {
			maxRateLimitWait = "24h"
		}
	}
	waitDur, _ := time.ParseDuration(maxRateLimitWait)

	budgetUSD := flagBudgetUSD
	if budgetUSD == 0 && tf.Frontmatter.BudgetUSD != nil {
		budgetUSD = *tf.Frontmatter.BudgetUSD
	}

	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	runDir := filepath.Join(workDir, ".syla", "runs", runID)

	// Create agent via provider
	runInfo := provider.RunInfo{RunID: runID, RunDir: runDir, SchemaPath: filepath.Join(runDir, "schema.json")}
	ag, err := provider.CreateAgent(agentName, runInfo, nil, nil, provider.CreateAgentOptions{Model: model})
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	cfg := loop.Config{
		Agent:                ag,
		Workspace:            ws,
		WorkDir:              workDir,
		RunID:                runID,
		RunDir:               runDir,
		MaxIterations:        maxIters,
		MaxTokens:            maxTokens,
		BudgetUSD:            budgetUSD,
		MaxDuration:          maxDur,
		MaxRateLimitWait:     waitDur,
		StopWhen:             stopWhen,
		Model:                model,
		PreventSleep:         preventSleep,
	}

	// Persist full task file content (including frontmatter) for daemon to reload
	rawTaskContent, _ := os.ReadFile(taskPath)
	if len(rawTaskContent) == 0 {
		rawTaskContent = []byte(tf.Body)
	}
	// Spawn daemon with full task content
	daemonProc, err := daemon.Spawn(workDir, cfg, string(rawTaskContent))
	if err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	fmt.Printf("Started run %s (pid %d) in %s\n", daemonProc.RunID, daemonProc.PID, daemonProc.RunDir)
	fmt.Printf("Attaching TUI... (Ctrl+C to detach, run `syla stop %s` to stop)\n", runID)

	// Try to attach TUI via loop.Engine directly for first client
	// For simplicity, create a local engine that tails the daemon's checkpoint? Instead, just attach via daemon IPC.
	// For MVP, we will run TUI that polls daemon status.
	// To keep it simple, we will create a local engine that mirrors daemon's run by just showing status.
	// Better: directly run TUI attached to a dummy engine that subscribes via daemon client.

	// For now, just show status and wait
	// If daemon is running, we can try to dial it after a short wait
	time.Sleep(500 * time.Millisecond)
	return runAttach(cmd, []string{runID})
}

func runAttach(cmd *cobra.Command, args []string) error {
	runID := args[0]
	workDir, _ := os.Getwd()
	runDir := filepath.Join(workDir, ".syla", "runs", runID)
	return tui.AttachRemote(runID, runDir)
}

func runStatus(cmd *cobra.Command, args []string) error {
	workDir, _ := os.Getwd()
	runs, err := daemon.ListRuns(workDir)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Println("No runs yet. Start one with `syla run <task.md>`")
		return nil
	}
	fmt.Printf("%-20s %-12s %-8s %-12s %-10s\n", "RUN ID", "STATUS", "ITER", "DURATION", "PROVIDER")
	for _, r := range runs {
		duration := r.Duration
		if duration == "" {
			duration = r.Elapsed
		}
		if duration == "" {
			duration = "-"
		}
		provider := r.Provider
		if provider == "" {
			provider = "-"
		}
		fmt.Printf("%-20s %-12s %-8d %-12s %-10s\n", r.RunID, r.Status, r.Iteration, duration, provider)
	}
	return nil
}

func runStop(cmd *cobra.Command, args []string) error {
	runID := args[0]
	workDir, _ := os.Getwd()
	runDir := filepath.Join(workDir, ".syla", "runs", runID)
	client, err := daemon.Dial(runDir)
	if err != nil {
		fmt.Printf("Could not connect to daemon, marking as stopped: %v\n", err)
		return nil
	}
	defer client.Close()
	var res map[string]string
	if err := client.Call("Stop", nil, &res); err != nil {
		return err
	}
	fmt.Printf("Stopped %s\n", runID)
	return nil
}

func runClose(cmd *cobra.Command, args []string) error {
	runID := args[0]
	workDir, _ := os.Getwd()
	runDir := filepath.Join(workDir, ".syla", "runs", runID)
	// Check current status first - don't overwrite completed/failed runs
	if cp, err := loop.LoadCheckpoint(runDir); err == nil {
		if cp.Status == "completed" || cp.Status == "failed" {
			fmt.Printf("Run %s already %s, not aborting\n", runID, cp.Status)
			return nil
		}
		if cp.Status == "aborted" {
			fmt.Printf("Run %s already aborted\n", runID)
			return nil
		}
	}
	client, err := daemon.Dial(runDir)
	if err != nil {
		// fallback: mark as aborted via checkpoint directly if still running
		fmt.Printf("Could not connect to daemon, marking as aborted: %v\n", err)
		if cp, err := loop.LoadCheckpoint(runDir); err == nil {
			if cp.Status == "completed" || cp.Status == "failed" || cp.Status == "aborted" {
				fmt.Printf("Run %s already %s\n", runID, cp.Status)
				return nil
			}
			cp.Status = "aborted"
			cp.StopReason = "aborted"
			_ = loop.SaveCheckpoint(runDir, cp)
		} else {
			cp := &loop.Checkpoint{
				RunID:  runID,
				RunDir: runDir,
				Status: "aborted",
				StopReason: "aborted",
			}
			_ = loop.SaveCheckpoint(runDir, cp)
		}
		if data, err := os.ReadFile(filepath.Join(runDir, "daemon.pid")); err == nil {
			var pid int
			if _, err := fmt.Sscanf(string(data), "%d", &pid); err == nil {
				_ = killPID(pid)
			}
		}
		fmt.Printf("Closed %s (aborted)\n", runID)
		return nil
	}
	defer client.Close()
	var res map[string]string
	if err := client.Call("Close", nil, &res); err != nil {
		if err2 := client.Call("Abort", nil, &res); err2 != nil {
			return err
		}
	}
	fmt.Printf("Closed %s (aborted)\n", runID)
	return nil
}

func killPID(pid int) error {
	// try to kill the daemon process
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
