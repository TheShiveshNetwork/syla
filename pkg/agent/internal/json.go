package internal

import (
	"encoding/json"
	"strings"
)

// StripJsonFences removes markdown fences like ```json ... ``` if present.
func StripJsonFences(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	withoutOpen := trimmed
	if idx := strings.Index(withoutOpen, "\n"); idx != -1 {
		_ = withoutOpen[:idx]
		withoutOpen = withoutOpen[idx+1:]
	} else {
		withoutOpen = strings.TrimPrefix(withoutOpen, "```")
		withoutOpen = strings.TrimPrefix(withoutOpen, "json")
		return strings.TrimSpace(withoutOpen)
	}
	trimmed2 := strings.TrimSpace(trimmed)
	if strings.HasPrefix(trimmed2, "```json") {
		withoutOpen = strings.TrimPrefix(trimmed2, "```json")
		withoutOpen = strings.TrimLeft(withoutOpen, " \t\r\n")
		if strings.HasSuffix(strings.TrimSpace(withoutOpen), "```") {
			withoutOpen = strings.TrimSuffix(strings.TrimSpace(withoutOpen), "```")
		}
		return strings.TrimSpace(withoutOpen)
	}
	if strings.HasPrefix(trimmed2, "```") {
		withoutOpen = strings.TrimPrefix(trimmed2, "```")
		withoutOpen = strings.TrimLeft(withoutOpen, " \t\r\n")
		if strings.HasSuffix(strings.TrimSpace(withoutOpen), "```") {
			withoutOpen = strings.TrimSuffix(strings.TrimSpace(withoutOpen), "```")
		}
		return strings.TrimSpace(withoutOpen)
	}
	withoutOpen = strings.TrimSpace(withoutOpen)
	if strings.HasSuffix(withoutOpen, "```") {
		withoutOpen = strings.TrimSuffix(withoutOpen, "```")
	}
	return strings.TrimSpace(withoutOpen)
}

// TryExtractBalancedObject walks from start (which must be '{') and returns first balanced JSON object substring.
func TryExtractBalancedObject(text string, start int) (string, bool) {
	if start < 0 || start >= len(text) || text[start] != '{' {
		return "", false
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if escape {
			escape = false
			continue
		}
		if inString {
			if ch == '\\' {
				escape = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				candidate := text[start : i+1]
				var tmp any
				if err := json.Unmarshal([]byte(candidate), &tmp); err == nil {
					return candidate, true
				}
				return "", false
			}
		}
	}
	return "", false
}

// ExtractLastJsonObject looks for rightmost balanced JSON object, optionally filtered by accepts.
func ExtractLastJsonObject(text string, accepts func(any) bool) (string, bool) {
	cursor := strings.LastIndex(text, "{")
	for cursor >= 0 {
		candidate, ok := TryExtractBalancedObject(text, cursor)
		if ok {
			if accepts == nil {
				return candidate, true
			}
			var parsed any
			if err := json.Unmarshal([]byte(candidate), &parsed); err == nil {
				if accepts(parsed) {
					return candidate, true
				}
			}
		}
		if cursor == 0 {
			break
		}
		prev := strings.LastIndex(text[:cursor], "{")
		if prev == -1 {
			break
		}
		cursor = prev
	}
	return "", false
}

// ParseAgentJSON attempts to parse JSON from text, handling fences and prose wrapping.
func ParseAgentJSON(text string, accepts func(any) bool) any {
	cleaned := StripJsonFences(text)
	if cleaned == "" {
		return nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(cleaned), &parsed); err == nil {
		if accepts == nil || accepts(parsed) {
			return parsed
		}
		return nil
	}
	extracted, ok := ExtractLastJsonObject(cleaned, accepts)
	if !ok {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(extracted), &out); err != nil {
		return nil
	}
	return out
}
