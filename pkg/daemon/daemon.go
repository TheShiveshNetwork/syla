package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/TheShiveshNetwork/syla/pkg/agent/provider"
	"github.com/TheShiveshNetwork/syla/pkg/keepawake"
	"github.com/TheShiveshNetwork/syla/pkg/loop"
	"github.com/TheShiveshNetwork/syla/pkg/workspace"
)

// Daemon holds a detached run.
type Daemon struct {
	RunID  string
	RunDir string
	PID    int
}

// Spawn starts a detached daemon process for the given loop config and task body.
// It returns the run ID and run dir.
func Spawn(workDir string, cfg loop.Config, taskBody string) (*Daemon, error) {
	if cfg.RunID == "" {
		return nil, fmt.Errorf("run ID required")
	}
	runDir := cfg.RunDir
	if runDir == "" {
		runDir = filepath.Join(workDir, ".syla", "runs", cfg.RunID)
	}
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return nil, err
	}
	// write task body for daemon to read
	_ = os.WriteFile(filepath.Join(runDir, "task.md"), []byte(taskBody), 0644)
	// persist effective config for daemon to reload (CLI overrides already applied)
	_ = writeDaemonConfig(runDir, cfg, taskBody)

	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// Re-exec self with --daemon flag.
	cmd := exec.Command(exe, "--daemon", cfg.RunID, runDir)
	cmd.Dir = workDir
	// Detach: new session, no SIGHUP on parent exit.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout, _ = os.OpenFile(filepath.Join(runDir, "daemon.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	cmd.Stderr = cmd.Stdout
	cmd.Stdin = nil
	// Pass config via env? For simplicity, daemon will re-load from runDir/task.md and checkpoint.
	// We also pass via extra env: SYLA_RUN_* not needed.
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Don't wait; let it run.
	_ = cmd.Process.Release()
	return &Daemon{RunID: cfg.RunID, RunDir: runDir, PID: cmd.Process.Pid}, nil
}

type daemonPersistedConfig struct {
	Agent              string `json:"agent"`
	Model              string `json:"model"`
	MaxIterations      int    `json:"max_iterations"`
	MaxTokens          int    `json:"max_tokens"`
	BudgetUSD          float64 `json:"budget_usd"`
	MaxDuration        string `json:"max_duration"`
	MaxRateLimitWait   string `json:"max_rate_limit_wait"`
	StopWhen           string `json:"stop_when"`
	PreventSleep       bool   `json:"prevent_sleep"`
	WorkDir            string `json:"work_dir"`
	Workspace          string `json:"workspace"`
	TaskBody           string `json:"task_body"`
}

func writeDaemonConfig(runDir string, cfg loop.Config, taskBody string) error {
	// If taskBody is full task.md with frontmatter, extract just the body for loop
	body := taskBody
	if tf, err := parseTaskBody(taskBody); err == nil {
		body = tf
	}
	pc := daemonPersistedConfig{
		Model:            cfg.Model,
		MaxIterations:    cfg.MaxIterations,
		MaxTokens:        cfg.MaxTokens,
		BudgetUSD:        cfg.BudgetUSD,
		MaxDuration:      cfg.MaxDuration.String(),
		MaxRateLimitWait: cfg.MaxRateLimitWait.String(),
		StopWhen:         cfg.StopWhen,
		PreventSleep:     cfg.PreventSleep,
		WorkDir:          cfg.WorkDir,
		TaskBody:         body,
	}
	// we need agent name and workspace kind - try to infer from cfg if possible
	// For now, store empty and daemon will re-parse task.md frontmatter; CLI overrides for agent/workspace are already in taskBody? No, taskBody is just body, not frontmatter.
	// Instead, we store cfg's Agent name via cfg.Agent.Name() if available
	if cfg.Agent != nil {
		pc.Agent = cfg.Agent.Name()
	}
	if cfg.Workspace != nil {
		pc.Workspace = cfg.Workspace.Kind()
	}
	data, _ := json.MarshalIndent(pc, "", "  ")
	return os.WriteFile(filepath.Join(runDir, "daemon.json"), data, 0644)
}

func loadDaemonConfig(runDir string) (*daemonPersistedConfig, error) {
	data, err := os.ReadFile(filepath.Join(runDir, "daemon.json"))
	if err != nil {
		return nil, err
	}
	var pc daemonPersistedConfig
	if err := json.Unmarshal(data, &pc); err != nil {
		return nil, err
	}
	return &pc, nil
}

func parseTaskBody(full string) (string, error) {
	// Try to parse as taskfile, return body if frontmatter present
	if len(full) > 3 && full[:3] == "---" {
		parts := 0
		// Find second ---
		idx := -1
		for i := 3; i < len(full)-3; i++ {
			if full[i:i+3] == "---" {
				idx = i
				parts++
				break
			}
		}
		if idx != -1 {
			body := full[idx+3:]
			return body, nil
		}
	}
	return full, nil
}

// RunDaemon is the entrypoint for the detached process (invoked with --daemon).
// It runs the loop engine and serves IPC.
func RunDaemon(runID, runDir string, agent loop.Agent, ws workspace.Workspace, taskBody string, cfg loop.Config) error {
	// If called via re-exec, agent/ws/taskBody/cfg will be zero; try to load persisted config
	if agent == nil || taskBody == "" {
		if pc, err := loadDaemonConfig(runDir); err == nil {
			if taskBody == "" {
				taskBody = pc.TaskBody
			}
			if cfg.WorkDir == "" {
				cfg.WorkDir = pc.WorkDir
			}
			if cfg.MaxIterations == 0 {
				cfg.MaxIterations = pc.MaxIterations
			}
			if cfg.MaxTokens == 0 {
				cfg.MaxTokens = pc.MaxTokens
			}
			if cfg.BudgetUSD == 0 {
				cfg.BudgetUSD = pc.BudgetUSD
			}
			if cfg.StopWhen == "" {
				cfg.StopWhen = pc.StopWhen
			}
			if cfg.Model == "" {
				cfg.Model = pc.Model
			}
			cfg.PreventSleep = pc.PreventSleep
			if d, err := time.ParseDuration(pc.MaxDuration); err == nil {
				cfg.MaxDuration = d
			}
			if d, err := time.ParseDuration(pc.MaxRateLimitWait); err == nil && d != 0 {
				cfg.MaxRateLimitWait = d
			}
			// try to re-create workspace and agent from persisted names if not provided
			if ws == nil && pc.Workspace != "" {
				ws, _ = workspace.Get(pc.Workspace, cfg.WorkDir)
			}
			if agent == nil && pc.Agent != "" {
				runInfo := provider.RunInfo{RunID: runID, RunDir: runDir, SchemaPath: filepath.Join(runDir, "schema.json")}
				if ag2, err := provider.CreateAgent(pc.Agent, runInfo, nil, nil, provider.CreateAgentOptions{Model: pc.Model}); err == nil {
					agent = ag2
					cfg.Agent = ag2
				}
			}
			// fallback: if still no agent and taskBody empty, try to read task.md directly
			if taskBody == "" {
				if data, err := os.ReadFile(filepath.Join(runDir, "task.md")); err == nil {
					if b, err := parseTaskBody(string(data)); err == nil {
						taskBody = b
					} else {
						taskBody = string(data)
					}
				}
			} else {
				// taskBody may still contain frontmatter if it came from daemon.json that was full content (legacy)
				if b, err := parseTaskBody(taskBody); err == nil && b != taskBody {
					// Only replace if parse succeeded and body is different and not empty
					// Check if original taskBody looks like frontmatter
					if len(taskBody) > 3 && taskBody[:3] == "---" {
						taskBody = b
					}
				}
			}
		}
	}
	// Acquire keep-awake if requested.
	var keeper *keepawake.Keeper
	if cfg.PreventSleep {
		keeper = keepawake.Acquire(fmt.Sprintf("syla run %s", runID))
		if keeper.Err != nil {
			fmt.Fprintf(os.Stderr, "keep-awake warning: %v\n", keeper.Err)
		}
		defer keeper.Release()
	}

	// Ensure workspace and agent are set
	if ws == nil {
		ws, _ = workspace.Get("none", cfg.WorkDir)
	}
	cfg.RunID = runID
	cfg.RunDir = runDir
	cfg.Workspace = ws

	eng, err := loop.New(cfg)
	if err != nil {
		return err
	}
	defer eng.Close()

	// IPC server
	sock := SockPath(runDir)
	srv := NewServer(sock)
	srv.Handle("Status", func(raw json.RawMessage) (any, error) {
		st := eng.Status()
		// Prefer checkpoint for status/provider/duration (more accurate for completed runs)
		status := st.StopReason
		if status == "" {
			if cp, err := loop.LoadCheckpoint(runDir); err == nil && cp.Status != "" {
				status = cp.Status
				if cp.StopReason != "" {
					status = cp.StopReason
				}
			} else {
				status = "running"
				if st.Iteration == 0 && st.StopReason == "" {
					status = "running"
				}
			}
		}
		if status == "" {
			status = "running"
		}
		providerName := ""
		if cp, err := loop.LoadCheckpoint(runDir); err == nil && cp.Provider != "" {
			providerName = cp.Provider
		}
		if providerName == "" {
			if pc, err := loadDaemonConfig(runDir); err == nil {
				providerName = pc.Agent
			}
		}
		if providerName == "" && eng != nil {
			// fallback to checkpoint's provider or agent name
			if cp, err := loop.LoadCheckpoint(runDir); err == nil {
				providerName = cp.Provider
			}
		}
		duration := st.Elapsed.String()
		// For completed/aborted, use total duration from checkpoint
		if status == "aborted" || status == "completed" || status == "failed" || strings.Contains(status, "max_") || strings.Contains(status, "budget") {
			if cp, err := loop.LoadCheckpoint(runDir); err == nil && !cp.StartedAt.IsZero() && !cp.UpdatedAt.IsZero() {
				duration = cp.UpdatedAt.Sub(cp.StartedAt).Truncate(time.Second).String()
			}
		}
		return Status{
			RunID:     runID,
			RunDir:    runDir,
			Provider:  providerName,
			Status:    status,
			Iteration: st.Iteration,
			Elapsed:   duration,
			Duration:  duration,
			StopReason: st.StopReason,
		}, nil
	})
	srv.Handle("Stop", func(raw json.RawMessage) (any, error) {
		_ = eng.Close()
		go func() { _ = srv.Close() }()
		return map[string]string{"status": "stopping"}, nil
	})
	srv.Handle("Close", func(raw json.RawMessage) (any, error) {
		_ = eng.Abort()
		go func() {
			_ = srv.Close()
			time.Sleep(200 * time.Millisecond)
			os.Exit(0)
		}()
		return map[string]string{"status": "aborted"}, nil
	})
	srv.Handle("Abort", func(raw json.RawMessage) (any, error) {
		_ = eng.Abort()
		go func() {
			_ = srv.Close()
			time.Sleep(200 * time.Millisecond)
			os.Exit(0)
		}()
		return map[string]string{"status": "aborted"}, nil
	})
	srv.Handle("Subscribe", func(raw json.RawMessage) (any, error) {
		return map[string]string{"subscribed": "ok"}, nil
	})
	if err := srv.Listen(); err != nil {
		return err
	}
	defer srv.Close()

	// Forward loop events to IPC subscribers
	go func() {
		for ev := range eng.Events() {
			srv.Broadcast(ev)
		}
	}()

	// Write PID file
	_ = os.WriteFile(filepath.Join(runDir, "daemon.pid"), []byte(fmt.Sprintf("%d", os.Getpid())), 0644)

	// Run loop
	ctx := context.Background()
	err = eng.Run(ctx, taskBody)
	// Update checkpoint status
	status := "completed"
	if err != nil {
		status = "failed"
	}
	_ = writeExitSummary(runDir, status, err, eng)
	return err
}

func writeExitSummary(runDir, status string, err error, eng *loop.Engine) error {
	st := eng.Status()
	summary := fmt.Sprintf("run %s %s after %d iterations, elapsed %s\n", eng.RunID(), status, st.Iteration, st.Elapsed)
	if err != nil {
		summary += fmt.Sprintf("error: %v\n", err)
	}
	if st.StopReason != "" {
		summary += fmt.Sprintf("stop reason: %s\n", st.StopReason)
	}
	return os.WriteFile(filepath.Join(runDir, "exit-summary.txt"), []byte(summary), 0644)
}

// IsDaemon checks if current process was invoked as daemon.
func IsDaemon() bool {
	for _, a := range os.Args {
		if a == "--daemon" {
			return true
		}
	}
	return false
}

func DaemonArgs() (runID, runDir string, isDaemon bool) {
	for i, a := range os.Args {
		if a == "--daemon" && i+2 < len(os.Args) {
			return os.Args[i+1], os.Args[i+2], true
		}
	}
	return "", "", false
}
