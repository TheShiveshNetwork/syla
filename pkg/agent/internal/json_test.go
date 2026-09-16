package internal

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestStripJsonFences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no fences", "  {\"a\":1}  ", "{\"a\":1}"},
		{"json fence", "```json\n{\"a\":1}\n```", "{\"a\":1}"},
		{"bare fence", "```\n{\"a\":1}\n```", "{\"a\":1}"},
		{"empty", "   ", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripJsonFences(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestTryExtractBalancedObject(t *testing.T) {
	if got, ok := TryExtractBalancedObject(`noise {"a":1} more`, 6); !ok || got != `{"a":1}` {
		t.Fatalf("expected {\"a\":1} got %q ok %v", got, ok)
	}
	if got, ok := TryExtractBalancedObject(`{"a":{"b":2}}`, 0); !ok || got != `{"a":{"b":2}}` {
		t.Fatalf("nested failed %q %v", got, ok)
	}
	if got, ok := TryExtractBalancedObject(`{"a":"{not real}"}`, 0); !ok || got != `{"a":"{not real}"}` {
		t.Fatalf("string brace failed %q %v", got, ok)
	}
	if _, ok := TryExtractBalancedObject(`"a":1`, 0); ok {
		t.Fatalf("should be null when not brace")
	}
	if _, ok := TryExtractBalancedObject(`{"a":1`, 0); ok {
		t.Fatalf("unterminated should be null")
	}
	if _, ok := TryExtractBalancedObject(`{"a":1`, -1); ok {
		t.Fatalf("negative start should be null")
	}
	if _, ok := TryExtractBalancedObject(`{"a":1}`, 10); ok {
		t.Fatalf("out of bounds should be null")
	}
}

func TestExtractLastJsonObject(t *testing.T) {
	if got, ok := ExtractLastJsonObject(`first {"a":1} then {"b":2}`, nil); !ok || got != `{"b":2}` {
		t.Fatalf("rightmost failed %q %v", got, ok)
	}
	if _, ok := ExtractLastJsonObject(`plain prose`, nil); ok {
		t.Fatalf("should be null")
	}
	if got, ok := ExtractLastJsonObject(`good {"a":1} bad {"b":`, nil); !ok || got != `{"a":1}` {
		t.Fatalf("fallback failed %q %v", got, ok)
	}
	text := `final {"success":true,"summary":"mentions {}","key_changes_made":[],"key_learnings":[]} trailing`
	got, ok := ExtractLastJsonObject(text, func(v any) bool {
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		_, hasSuccess := m["success"]
		_, hasSummary := m["summary"]
		return hasSuccess && hasSummary
	})
	if !ok {
		t.Fatalf("accepts scan failed")
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(got), &m)
	if m["success"] != true {
		t.Fatalf("wrong object")
	}
	// prefer rightmost valid
	if got, ok := ExtractLastJsonObject(`{"a":1} {"b":2} {"c":3}`, nil); !ok || got != `{"c":3}` {
		t.Fatalf("prefer rightmost failed %q", got)
	}
}

func TestParseAgentJSON(t *testing.T) {
	check := func(text string, want any) {
		t.Helper()
		got := ParseAgentJSON(text, nil)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("parse %q got %#v want %#v", text, got, want)
		}
	}
	check(`{"success":true}`, map[string]any{"success": true})
	check("```json\n{\"success\":true}\n```", map[string]any{"success": true})
	check("BUILD SUCCESS\n\n{\"success\": true, \"summary\": \"x\", \"key_changes_made\": [], \"key_learnings\": []}", map[string]any{"success": true, "summary": "x", "key_changes_made": []any{}, "key_learnings": []any{}})
	check("{\"success\":true}\n\nDone!", map[string]any{"success": true})
	got := ParseAgentJSON("log: {\"step\":1}\nfinal: {\"step\":2}", nil)
	m := got.(map[string]any)
	if m["step"] != float64(2) {
		t.Fatalf("prefer last failed %v", m)
	}
	if ParseAgentJSON("just prose", nil) != nil {
		t.Fatalf("should be null")
	}
	if ParseAgentJSON("", nil) != nil {
		t.Fatalf("empty should be null")
	}
	text := `{"success":true,"summary":{"success":true,"summary":"nested","key_changes_made":[],"key_learnings":[]},"key_changes_made":[],"key_learnings":[]}`
	got2 := ParseAgentJSON(text, func(v any) bool {
		m2, ok := v.(map[string]any)
		if !ok {
			return false
		}
		s, ok := m2["summary"]
		if !ok {
			return false
		}
		_, isStr := s.(string)
		return isStr
	})
	if got2 != nil {
		t.Fatalf("should be null when top rejected and nested not considered, got %v", got2)
	}
	// with accepts filter
	got3 := ParseAgentJSON(`{"a":1} {"b":2}`, func(v any) bool {
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		_, ok = m["b"]
		return ok
	})
	if got3 == nil {
		t.Fatalf("should find b object")
	}
	m3 := got3.(map[string]any)
	if m3["b"] != float64(2) {
		t.Fatalf("wrong filtered object %v", m3)
	}
}
