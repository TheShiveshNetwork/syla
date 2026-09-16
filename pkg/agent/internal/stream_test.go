package internal

import (
	"strings"
	"testing"
)

func TestAppendExitOutputTail(t *testing.T) {
	got := AppendExitOutputTail("hello ", "world")
	if got != "hello world" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("a", 3000)
	got2 := AppendExitOutputTail(long, strings.Repeat("b", 2000))
	if len(got2) != 4000 {
		t.Fatalf("len %d want 4000", len(got2))
	}
	if !strings.HasSuffix(got2, strings.Repeat("b", 2000)) {
		t.Fatalf("should end with b's")
	}
	if got3 := AppendExitOutputTail("", ""); got3 != "" {
		t.Fatalf("empty should be empty")
	}
}

func TestDescribeChildProcessExit(t *testing.T) {
	code := 1
	t.Run("with stderr", func(t *testing.T) {
		f := DescribeChildProcessExit("codex", &code, "", "boom")
		if f.Detail != "codex exited with code 1: boom" {
			t.Fatalf("got %q", f.Detail)
		}
		if f.ErrorOutput != "boom" {
			t.Fatalf("errorOutput %q", f.ErrorOutput)
		}
	})
	t.Run("structured stdout error", func(t *testing.T) {
		stdout := `{"type":"error","error":{"message":"login required"}}`
		f := DescribeChildProcessExit("codex", &code, stdout, "")
		if f.Detail != "codex exited with code 1: login required" {
			t.Fatalf("got %q", f.Detail)
		}
		if f.ErrorOutput != "login required" {
			t.Fatalf("errorOutput %q", f.ErrorOutput)
		}
	})
	t.Run("no output", func(t *testing.T) {
		f := DescribeChildProcessExit("pi", &code, "", "")
		if f.Detail != "pi exited with code 1 and produced no output" {
			t.Fatalf("got %q", f.Detail)
		}
	})
	t.Run("elides raw tail", func(t *testing.T) {
		long := strings.Repeat("x", 500)
		f := DescribeChildProcessExit("codex", &code, long, "")
		if !strings.Contains(f.Detail, "[...truncated") {
			t.Fatalf("should elide, got %q", f.Detail)
		}
		if !strings.Contains(f.ErrorOutput, "") {
			// errorOutput should be empty for unstructured stdout
			if f.ErrorOutput != "" {
				t.Fatalf("errorOutput should be empty for raw, got %q", f.ErrorOutput)
			}
		}
	})
	t.Run("null code", func(t *testing.T) {
		f := DescribeChildProcessExit("codex", nil, "", "err")
		if !strings.Contains(f.Detail, "code null") {
			t.Fatalf("should contain null code, got %q", f.Detail)
		}
	})
	t.Run("both streams", func(t *testing.T) {
		stdout := `{"type":"error","error":{"message":"stdout err"}}`
		f := DescribeChildProcessExit("claude", &code, stdout, "stderr err")
		if !strings.Contains(f.Detail, "stderr err") || !strings.Contains(f.Detail, "stdout err") {
			t.Fatalf("should contain both, got %q", f.Detail)
		}
	})
	t.Run("assistant text", func(t *testing.T) {
		stdout := `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`
		f := DescribeChildProcessExit("codex", &code, stdout, "")
		if !strings.Contains(f.Detail, "hello") {
			t.Fatalf("should contain assistant text, got %q", f.Detail)
		}
		if f.ErrorOutput != "" {
			t.Fatalf("assistant text should not be in ErrorOutput, got %q", f.ErrorOutput)
		}
	})
}

func TestParseJSONLStreamFromString(t *testing.T) {
	input := "\nnot json\n{\"ok\":true}\n   \n{\"ok\":false}\n"
	var events []map[string]any
	ParseJSONLStreamFromString(input, func(m map[string]any) {
		events = append(events, m)
	})
	if len(events) != 2 {
		t.Fatalf("expected 2 events got %d", len(events))
	}
	if events[0]["ok"] != true || events[1]["ok"] != false {
		t.Fatalf("wrong events %v", events)
	}
}

func TestParseJSONLStream(t *testing.T) {
	input := "{\"a\":1}\n{\"b\":2}\nnot json\n{\"c\":3}\n"
	r := strings.NewReader(input)
	var events []map[string]any
	var logBuf strings.Builder
	err := ParseJSONLStream(r, &logBuf, func(m map[string]any) {
		events = append(events, m)
	})
	if err != nil {
		t.Fatalf("unexpected err %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events got %d", len(events))
	}
	if events[0]["a"] != float64(1) || events[1]["b"] != float64(2) || events[2]["c"] != float64(3) {
		t.Fatalf("wrong events %v", events)
	}
	if !strings.Contains(logBuf.String(), "\"a\":1") {
		t.Fatalf("log should contain raw lines")
	}
	// test empty and blank handling
	r2 := strings.NewReader("\n\n   \n")
	events = nil
	err = ParseJSONLStream(r2, nil, func(m map[string]any) {
		events = append(events, m)
	})
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("should be 0 events for blank")
	}
}
