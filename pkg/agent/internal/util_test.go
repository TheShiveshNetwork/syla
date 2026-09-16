package internal

import (
	"reflect"
	"testing"
)

func TestIntVal(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want int
	}{
		{"float64", map[string]any{"a": float64(5)}, "a", 5},
		{"int", map[string]any{"a": int(7)}, "a", 7},
		{"int64", map[string]any{"a": int64(9)}, "a", 9},
		{"int32", map[string]any{"a": int32(11)}, "a", 11},
		{"missing", map[string]any{"b": 1}, "a", 0},
		{"wrong type", map[string]any{"a": "str"}, "a", 0},
		{"nil map", nil, "a", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IntVal(tc.m, tc.key); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestHasKey(t *testing.T) {
	if !HasKey(map[string]any{"a": 1}, "a") {
		t.Fatalf("should have key")
	}
	if HasKey(map[string]any{"a": 1}, "b") {
		t.Fatalf("should not have key")
	}
	if HasKey(nil, "a") {
		t.Fatalf("nil should not have key")
	}
}

func TestContainsAny(t *testing.T) {
	if !ContainsAny([]string{"--force", "other"}, []string{"--force", "-f"}) {
		t.Fatalf("should contain")
	}
	if !ContainsAny([]string{"--permission-mode=strict"}, []string{"--permission-mode"}) {
		t.Fatalf("should contain prefix")
	}
	if ContainsAny([]string{"--other"}, []string{"--force"}) {
		t.Fatalf("should not contain")
	}
	if !ContainsAny([]string{"--permission-mode=strict"}, []string{"--permission-mode", "--permission-prompt-tool"}) {
		t.Fatalf("permission mode")
	}
	if ContainsAny([]string{"--permission-mode-other"}, []string{"--permission-mode"}) {
		// this will match because ContainsAny checks HasPrefix with p+"=" but also p without =? need to check logic
		// for --permission-mode it checks exact or prefix with =, so --permission-mode-other should not match
		// Let's verify - it checks HasPrefix(p+"=") for permission mode, so it should not match
		t.Fatalf("should not match partial")
	}
}

func TestFilterModelArgs(t *testing.T) {
	if got := FilterModelArgs([]string{"a", "b"}, ""); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("no filter when model empty")
	}
	got := FilterModelArgs([]string{"--model", "old", "keep", "--model=old2", "x"}, "new")
	if len(got) != 2 || got[0] != "keep" || got[1] != "x" {
		t.Fatalf("filter failed got %v", got)
	}
	got = FilterModelArgs([]string{"--model", "old"}, "new")
	if len(got) != 0 {
		t.Fatalf("should filter all, got %v", got)
	}
}

func TestTextFromContentBlock(t *testing.T) {
	if got := TextFromContentBlock("hello"); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := TextFromContentBlock(map[string]any{"text": "t"}); got != "t" {
		t.Fatalf("got %q", got)
	}
	if got := TextFromContentBlock(map[string]any{"content": "c"}); got != "c" {
		t.Fatalf("got %q", got)
	}
	if got := TextFromContentBlock(map[string]any{"other": "x"}); got != "" {
		t.Fatalf("should be empty")
	}
	if got := TextFromContentBlock(123); got != "" {
		t.Fatalf("should be empty for int")
	}
}

func TestTextFromAssistantContent(t *testing.T) {
	if got := TextFromAssistantContent("hi"); got != "hi" {
		t.Fatalf("got %q", got)
	}
	if got := TextFromAssistantContent([]any{"a", map[string]any{"text": "b"}, "c"}); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := TextFromAssistantContent([]any{map[string]any{"content": "x"}}); got != "x" {
		t.Fatalf("got %q", got)
	}
	if got := TextFromAssistantContent(nil); got != "" {
		t.Fatalf("nil should be empty")
	}
	if got := TextFromAssistantContent(123); got != "" {
		t.Fatalf("should be empty")
	}
}

func TestIsSameUsage(t *testing.T) {
	a := TokenUsageLike{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheCreationTokens: 4}
	b := TokenUsageLike{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheCreationTokens: 4}
	if !IsSameUsage(a, b) {
		t.Fatalf("should be same")
	}
	b.OutputTokens = 99
	if IsSameUsage(a, b) {
		t.Fatalf("should not be same")
	}
}

func TestExtendsUsage(t *testing.T) {
	prev := TokenUsageLike{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheCreationTokens: 4}
	next := TokenUsageLike{InputTokens: 2, OutputTokens: 3, CacheReadTokens: 3, CacheCreationTokens: 4}
	if !ExtendsUsage(next, prev) {
		t.Fatalf("should extend")
	}
	same := prev
	if ExtendsUsage(same, prev) {
		t.Fatalf("same should not extend")
	}
	less := TokenUsageLike{InputTokens: 0, OutputTokens: 2, CacheReadTokens: 3, CacheCreationTokens: 4}
	if ExtendsUsage(less, prev) {
		t.Fatalf("less should not extend")
	}
}
