package internal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const MaxExitOutputChars = 4000
const MaxRawTailChars = 400
const rawTailElision = "[...truncated, full output in the iteration log] "

// AppendExitOutputTail keeps only the end of a stream so long-running processes stay bounded.
func AppendExitOutputTail(existing, chunk string) string {
	combined := existing + chunk
	if len(combined) > MaxExitOutputChars {
		return combined[len(combined)-MaxExitOutputChars:]
	}
	return combined
}

func errorTextFromEvent(event map[string]any) string {
	if event == nil {
		return ""
	}
	if errVal, ok := event["error"]; ok {
		if s, ok := errVal.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
		if m, ok := errVal.(map[string]any); ok {
			if msg, ok := m["message"].(string); ok && strings.TrimSpace(msg) != "" {
				return strings.TrimSpace(msg)
			}
		}
	}
	isError := event["is_error"] == true || event["type"] == "error"
	if isError {
		for _, key := range []string{"result", "message", "subtype"} {
			if v, ok := event[key].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func assistantTextFromEvent(event map[string]any) string {
	if event == nil || event["type"] != "assistant" {
		return ""
	}
	msgRaw, ok := event["message"]
	if !ok {
		return ""
	}
	msg, ok := msgRaw.(map[string]any)
	if !ok {
		return ""
	}
	content, ok := msg["content"]
	if !ok {
		return ""
	}
	arr, ok := content.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, block := range arr {
		m, ok := block.(map[string]any)
		if !ok {
			continue
		}
		if m["type"] == "text" {
			if t, ok := m["text"].(string); ok {
				parts = append(parts, t)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func elideRawTail(raw string) string {
	if len(raw) > MaxRawTailChars {
		return rawTailElision + raw[len(raw)-MaxRawTailChars:]
	}
	return raw
}

type stdoutFailure struct {
	structured string
	reported   string
}

func extractStdoutError(stdoutTail string) stdoutFailure {
	var structuredMessages []string
	var reportedMessages []string
	for _, line := range strings.Split(stdoutTail, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if errText := errorTextFromEvent(event); errText != "" {
			structuredMessages = append(structuredMessages, errText)
			reportedMessages = append(reportedMessages, errText)
			continue
		}
		if txt := assistantTextFromEvent(event); txt != "" {
			reportedMessages = append(reportedMessages, txt)
		}
	}
	structured := strings.Join(structuredMessages, "\n")
	reported := strings.Join(reportedMessages, "\n")
	if reported == "" {
		reported = elideRawTail(strings.TrimSpace(stdoutTail))
	}
	return stdoutFailure{structured: structured, reported: reported}
}

// ChildProcessExitFailure describes a non-zero exit.
type ChildProcessExitFailure struct {
	Detail      string
	ErrorOutput string
}

// DescribeChildProcessExit describes a non-zero exit.
func DescribeChildProcessExit(agentName string, code *int, stdoutTail, stderr string) ChildProcessExitFailure {
	trimmedStderr := strings.TrimSpace(stderr)
	stdoutErr := extractStdoutError(stdoutTail)
	var segments []string
	if trimmedStderr != "" {
		segments = append(segments, trimmedStderr)
	}
	if stdoutErr.reported != "" {
		segments = append(segments, stdoutErr.reported)
	}
	codeStr := "null"
	if code != nil {
		codeStr = fmt.Sprintf("%d", *code)
	}
	var detail string
	if len(segments) > 0 {
		detail = fmt.Sprintf("%s exited with code %s: %s", agentName, codeStr, strings.Join(segments, "\n"))
	} else {
		detail = fmt.Sprintf("%s exited with code %s and produced no output", agentName, codeStr)
	}
	var errSegs []string
	if trimmedStderr != "" {
		errSegs = append(errSegs, trimmedStderr)
	}
	if stdoutErr.structured != "" {
		errSegs = append(errSegs, stdoutErr.structured)
	}
	return ChildProcessExitFailure{Detail: detail, ErrorOutput: strings.Join(errSegs, "\n")}
}

// ParseJSONLStream reads a JSONL stream line-by-line, calling callback for each parsed event.
func ParseJSONLStream(r io.Reader, logW io.Writer, callback func(map[string]any)) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if logW != nil {
			_, _ = logW.Write(append([]byte(line), '\n'))
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
			continue
		}
		callback(event)
	}
	return scanner.Err()
}

// ParseJSONLStreamFromString is a helper for testing.
func ParseJSONLStreamFromString(s string, callback func(map[string]any)) {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
			continue
		}
		callback(event)
	}
}
