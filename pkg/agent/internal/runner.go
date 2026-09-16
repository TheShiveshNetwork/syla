package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// RunnerConfig controls a JSONL child process execution.
type RunnerConfig struct {
	Bin          string
	Args         []string
	Cwd          string
	LogPath      string
	StdinContent string
	Detached     bool
}

// RunJSONL spawns the child, streams JSONL events to handleEvent, captures stdout tail and stderr, and respects ctx cancellation.
func RunJSONL(ctx context.Context, cfg RunnerConfig, handleEvent func(map[string]any)) (stdoutTail string, stderrStr string, exitCode int, err error) {
	if ctx.Err() != nil {
		return "", "", -1, fmt.Errorf("Agent was aborted")
	}
	cmd := exec.CommandContext(ctx, cfg.Bin, cfg.Args...)
	cmd.Dir = cfg.Cwd
	cmd.SysProcAttr = NewSysProcAttr(cfg.Detached)

	var stdin io.WriteCloser
	if cfg.StdinContent != "" {
		var e error
		stdin, e = cmd.StdinPipe()
		if e != nil {
			return "", "", -1, fmt.Errorf("Failed to spawn %s: %w", cfg.Bin, e)
		}
	}
	stdoutPipe, e := cmd.StdoutPipe()
	if e != nil {
		return "", "", -1, fmt.Errorf("Failed to spawn %s: %w", cfg.Bin, e)
	}
	stderrPipe, e := cmd.StderrPipe()
	if e != nil {
		return "", "", -1, fmt.Errorf("Failed to spawn %s: %w", cfg.Bin, e)
	}
	if err := cmd.Start(); err != nil {
		return "", "", -1, fmt.Errorf("Failed to spawn %s: %w", cfg.Bin, err)
	}
	if stdin != nil {
		go func() {
			_, _ = io.WriteString(stdin, cfg.StdinContent)
			_ = stdin.Close()
		}()
	}
	// abort on context cancel
	go func() {
		<-ctx.Done()
		_ = SignalChildProcess(cmd, cfg.Detached, os.Interrupt)
	}()

	var logFile *os.File
	if cfg.LogPath != "" {
		f, err := os.Create(cfg.LogPath)
		if err == nil {
			logFile = f
			defer f.Close()
		}
	}

	var mu sync.Mutex
	var stderrBuilder strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		data, _ := io.ReadAll(stderrPipe)
		stderrBuilder.WriteString(string(data))
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 10*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			raw := line + "\n"
			mu.Lock()
			stdoutTail = AppendExitOutputTail(stdoutTail, raw)
			mu.Unlock()
			if logFile != nil {
				_, _ = logFile.Write([]byte(raw))
			}
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			var ev map[string]any
			if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
				continue
			}
			handleEvent(ev)
		}
	}()
	waitErr := cmd.Wait()
	wg.Wait()
	if ctx.Err() != nil {
		return stdoutTail, stderrBuilder.String(), -1, fmt.Errorf("Agent was aborted")
	}
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			return stdoutTail, stderrBuilder.String(), exitErr.ExitCode(), nil
		}
		return stdoutTail, stderrBuilder.String(), -1, waitErr
	}
	return stdoutTail, stderrBuilder.String(), 0, nil
}
