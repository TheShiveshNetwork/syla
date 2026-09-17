package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/TheShiveshNetwork/syla/pkg/daemon"
	"github.com/TheShiveshNetwork/syla/pkg/loop"
	"github.com/TheShiveshNetwork/syla/pkg/tui"
)

func main() {
	workDir, _ := os.Getwd()
	runID := fmt.Sprintf("mock-%d", time.Now().UnixNano())
	runDir := filepath.Join(workDir, ".syla", "runs", runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		panic(err)
	}
	fmt.Printf("Starting mock daemon %s at %s\n", runID, runDir)
	fmt.Println("TUI: q=detach, a=abort (with confirm), p=pause (stub)")
	fmt.Println("This is a FAKE daemon - no real agent, just for UI testing. Press Ctrl+C or q to detach.")

	// Create initial checkpoint
	cp := &loop.Checkpoint{
		RunID:     runID,
		RunDir:    runDir,
		Provider:  "opencode",
		Iteration: 0,
		Status:    "running",
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_ = loop.SaveCheckpoint(runDir, cp)
	// also write daemon.json for provider info
	_ = os.WriteFile(filepath.Join(runDir, "daemon.json"), []byte(fmt.Sprintf(`{"agent":"opencode","work_dir":%q,"task_body":"mock task"}`, workDir)), 0644)

	// Start fake daemon IPC that mimics real daemon's Status/Close handlers
	// It shares the checkpoint file with the updater goroutine below
	srv := daemon.NewServer(daemon.SockPath(runDir))
	srv.Handle("Status", func(raw json.RawMessage) (any, error) {
		st, _ := loop.LoadCheckpoint(runDir)
		if st == nil {
			return daemon.Status{RunID: runID, RunDir: runDir, Status: "running", Iteration: 0, Elapsed: "0s", Provider: "opencode"}, nil
		}
		elapsed := time.Since(st.StartedAt).Truncate(time.Millisecond).String()
		if st.Status == "completed" || st.Status == "failed" || st.Status == "aborted" {
			elapsed = st.UpdatedAt.Sub(st.StartedAt).Truncate(time.Millisecond).String()
		}
		status := st.Status
		if st.StopReason != "" {
			status = st.StopReason
		}
		if status == "" {
			status = "running"
		}
		return daemon.Status{
			RunID:     runID,
			RunDir:    runDir,
			Provider:  st.Provider,
			Status:    status,
			Iteration: st.Iteration,
			Elapsed:   elapsed,
			Duration:  elapsed,
		}, nil
	})
	srv.Handle("Close", func(raw json.RawMessage) (any, error) {
		// mark as aborted
		if cp, err := loop.LoadCheckpoint(runDir); err == nil {
			cp.Status = "aborted"
			cp.StopReason = "aborted"
			_ = loop.SaveCheckpoint(runDir, cp)
			_ = os.WriteFile(filepath.Join(runDir, "exit-summary.txt"), []byte(fmt.Sprintf("run %s aborted after %d iterations, elapsed %s\n", runID, cp.Iteration, time.Since(cp.StartedAt).Truncate(time.Second).String())), 0644)
		}
		go func() {
			_ = srv.Close()
			time.Sleep(200 * time.Millisecond)
			os.Exit(0)
		}()
		return map[string]string{"status": "aborted"}, nil
	})
	srv.Handle("Abort", func(raw json.RawMessage) (any, error) {
		if cp, err := loop.LoadCheckpoint(runDir); err == nil {
			cp.Status = "aborted"
			cp.StopReason = "aborted"
			_ = loop.SaveCheckpoint(runDir, cp)
			_ = os.WriteFile(filepath.Join(runDir, "exit-summary.txt"), []byte(fmt.Sprintf("run %s aborted after %d iterations, elapsed %s\n", runID, cp.Iteration, time.Since(cp.StartedAt).Truncate(time.Second).String())), 0644)
		}
		go func() {
			_ = srv.Close()
			time.Sleep(200 * time.Millisecond)
			os.Exit(0)
		}()
		return map[string]string{"status": "aborted"}, nil
	})
	srv.Handle("Stop", func(raw json.RawMessage) (any, error) {
		if cp, err := loop.LoadCheckpoint(runDir); err == nil {
			cp.Status = "completed"
			_ = loop.SaveCheckpoint(runDir, cp)
		}
		go func() { _ = srv.Close() }()
		return map[string]string{"status": "stopping"}, nil
	})
	srv.Handle("Subscribe", func(raw json.RawMessage) (any, error) {
		return map[string]string{"subscribed": "ok"}, nil
	})
	if err := srv.Listen(); err != nil {
		panic(err)
	}
	defer srv.Close()
	_ = os.WriteFile(filepath.Join(runDir, "daemon.pid"), []byte(fmt.Sprintf("%d", os.Getpid())), 0644)

	// Background fake progress updater - simulates a real run
	go func() {
		ticker := time.NewTicker(700 * time.Millisecond)
		defer ticker.Stop()
		iter := 0
		for range ticker.C {
			cp, err := loop.LoadCheckpoint(runDir)
			if err != nil {
				continue
			}
			if cp.Status == "aborted" || cp.Status == "completed" || cp.Status == "failed" {
				// already finished, stop updating
				return
			}
			iter++
			cp.Iteration = iter
			cp.Successes = iter
			cp.TotalTokens += 120 + iter*2 // fake tokens
			// simulate different stop reasons after some iterations
			if iter >= 15 {
				cp.Status = "completed"
				cp.StopReason = "max_iterations(15)"
			}
			_ = loop.SaveCheckpoint(runDir, cp)
			_ = loop.SaveIteration(runDir, iter, fmt.Sprintf("mock prompt %d", iter), fmt.Sprintf("mock output for iter %d - did some work", iter), map[string]int{"input": 60, "output": 60})
			if iter >= 15 {
				_ = os.WriteFile(filepath.Join(runDir, "exit-summary.txt"), []byte(fmt.Sprintf("run %s completed after %d iterations, elapsed %s\nstop reason: %s\n", runID, iter, time.Since(cp.StartedAt).Truncate(time.Second).String(), cp.StopReason)), 0644)
				return
			}
		}
	}()

	// Attach the SAME TUI as real runs (polls via daemon IPC + checkpoint)
	if err := tui.AttachRemote(runID, runDir); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\nMock TUI detached. Run `syla status` to see it, `syla close", runID, "` to abort, `syla attach", runID, "` to reattach.")
}
