package internal

import "strings"

// IntVal extracts int from map with float64/int/int64 handling.
func IntVal(m map[string]any, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		case int32:
			return int(n)
		}
	}
	return 0
}

func HasKey(m map[string]any, k string) bool { _, ok := m[k]; return ok }

func ContainsAny(args []string, prefixes []string) bool {
	for _, a := range args {
		for _, p := range prefixes {
			if a == p || strings.HasPrefix(a, p+"=") || strings.HasPrefix(a, p) {
				if p == "--permission-mode" || p == "--permission-prompt-tool" {
					if a == p || strings.HasPrefix(a, p+"=") {
						return true
					}
				} else if a == p {
					return true
				}
			}
		}
	}
	return false
}

func FilterModelArgs(args []string, model string) []string {
	if model == "" {
		return args
	}
	var out []string
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--model" || strings.HasPrefix(arg, "--model=") {
			if arg == "--model" {
				skipNext = true
			}
			continue
		}
		out = append(out, arg)
	}
	return out
}

func TextFromContentBlock(block any) string {
	if s, ok := block.(string); ok {
		return s
	}
	if m, ok := block.(map[string]any); ok {
		if t, ok := m["text"].(string); ok {
			return t
		}
		if c, ok := m["content"].(string); ok {
			return c
		}
	}
	return ""
}

func TextFromAssistantContent(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	if arr, ok := content.([]any); ok {
		var parts []string
		for _, b := range arr {
			if t := TextFromContentBlock(b); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func IsSameUsage(a, b TokenUsageLike) bool {
	return a.InputTokens == b.InputTokens && a.OutputTokens == b.OutputTokens && a.CacheReadTokens == b.CacheReadTokens && a.CacheCreationTokens == b.CacheCreationTokens
}

func ExtendsUsage(next, prev TokenUsageLike) bool {
	return next.InputTokens >= prev.InputTokens && next.OutputTokens >= prev.OutputTokens && next.CacheReadTokens >= prev.CacheReadTokens && next.CacheCreationTokens >= prev.CacheCreationTokens && !IsSameUsage(next, prev)
}

// TokenUsageLike is a minimal interface for usage comparison without importing agent types.
type TokenUsageLike struct {
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
}
