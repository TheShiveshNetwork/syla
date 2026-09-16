package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/TheShiveshNetwork/syla/pkg/agent/internal"
)

// AgentOutputSchema describes the JSON schema the agent must conform to.
type AgentOutputSchema struct {
	Type                 string                    `json:"type"`
	AdditionalProperties bool                      `json:"additionalProperties"`
	Properties           map[string]SchemaProperty `json:"properties"`
	Required             []string                  `json:"required"`
}

// SchemaProperty is a single field in the output schema.
type SchemaProperty struct {
	Type  string   `json:"type"`
	Items *Items   `json:"items,omitempty"`
	Enum  []string `json:"enum,omitempty"`
}

// Items describes array element type.
type Items struct {
	Type string `json:"type"`
}

// CommitField is an extra commit-message field to inject into the schema.
type CommitField struct {
	Name    string
	Allowed []string
}

// BuildAgentOutputSchema builds the schema given stop/commit options.
func BuildAgentOutputSchema(includeStopField bool, commitFields []CommitField) AgentOutputSchema {
	props := map[string]SchemaProperty{
		"success":          {Type: "boolean"},
		"summary":          {Type: "string"},
		"key_changes_made": {Type: "array", Items: &Items{Type: "string"}},
		"key_learnings":    {Type: "array", Items: &Items{Type: "string"}},
	}
	required := []string{"success", "summary", "key_changes_made", "key_learnings"}
	for _, f := range commitFields {
		p := SchemaProperty{Type: "string"}
		if f.Allowed != nil {
			p.Enum = f.Allowed
		}
		props[f.Name] = p
		required = append(required, f.Name)
	}
	if includeStopField {
		props["should_fully_stop"] = SchemaProperty{Type: "boolean"}
		required = append(required, "should_fully_stop")
	}
	return AgentOutputSchema{
		Type:                 "object",
		AdditionalProperties: false,
		Properties:           props,
		Required:             required,
	}
}

// ValidateAgentOutput validates value against schema and returns typed output.
func ValidateAgentOutput(value map[string]any, schema AgentOutputSchema) (AgentOutput, error) {
	if value == nil {
		return AgentOutput{}, fmt.Errorf("expected an object")
	}
	if !schema.AdditionalProperties {
		allowed := make(map[string]bool, len(schema.Properties))
		for k := range schema.Properties {
			allowed[k] = true
		}
		for k := range value {
			if !allowed[k] {
				return AgentOutput{}, fmt.Errorf("unexpected property %s", k)
			}
		}
	}
	for _, name := range schema.Required {
		if _, ok := value[name]; !ok {
			return AgentOutput{}, fmt.Errorf("%s is required", name)
		}
	}
	for name, prop := range schema.Properties {
		raw, ok := value[name]
		if !ok {
			continue
		}
		if prop.Type == "array" {
			arr, ok := raw.([]any)
			if !ok {
				return AgentOutput{}, fmt.Errorf("%s must be an array of strings", name)
			}
			if prop.Items != nil && prop.Items.Type == "string" {
				for _, item := range arr {
					if _, ok := item.(string); !ok {
						return AgentOutput{}, fmt.Errorf("%s must be an array of strings", name)
					}
				}
			}
			continue
		}
		switch prop.Type {
		case "string":
			s, ok := raw.(string)
			if !ok {
				article := "a"
				if strings.HasPrefix(prop.Type, "a") || strings.HasPrefix(prop.Type, "e") || strings.HasPrefix(prop.Type, "i") || strings.HasPrefix(prop.Type, "o") || strings.HasPrefix(prop.Type, "u") {
					// simplified check - reuse helper
					if isVowel(prop.Type[0]) {
						article = "an"
					}
				}
				return AgentOutput{}, fmt.Errorf("%s must be %s %s", name, article, prop.Type)
			}
			if len(prop.Enum) > 0 {
				found := false
				for _, e := range prop.Enum {
					if s == e {
						found = true
						break
					}
				}
				if !found {
					quoted := make([]string, len(prop.Enum))
					for i, e := range prop.Enum {
						q, _ := json.Marshal(e)
						quoted[i] = string(q)
					}
					return AgentOutput{}, fmt.Errorf("%s must be one of %s", name, strings.Join(quoted, ", "))
				}
			}
		case "boolean":
			if _, ok := raw.(bool); !ok {
				article := "a"
				if isVowel(prop.Type[0]) {
					article = "an"
				}
				return AgentOutput{}, fmt.Errorf("%s must be %s %s", name, article, prop.Type)
			}
		default:
			// generic type check via stringified type
			actual := fmt.Sprintf("%T", raw)
			_ = actual
		}
	}

	// Marshal back to typed struct
	b, _ := json.Marshal(value)
	var out AgentOutput
	if err := json.Unmarshal(b, &out); err != nil {
		return AgentOutput{}, err
	}
	// capture extras
	out.Extras = make(map[string]string)
	for k, v := range value {
		if _, known := schema.Properties[k]; known {
			// known but maybe commit field
			if k != "success" && k != "summary" && k != "key_changes_made" && k != "key_learnings" && k != "should_fully_stop" {
				if s, ok := v.(string); ok {
					out.Extras[k] = s
				}
			}
			continue
		}
	}
	// also capture commit fields if present
	for _, f := range schema.Required {
		if f == "success" || f == "summary" || f == "key_changes_made" || f == "key_learnings" || f == "should_fully_stop" {
			continue
		}
		if v, ok := value[f]; ok {
			if s, ok := v.(string); ok {
				out.Extras[f] = s
			}
		}
	}
	return out, nil
}

func isVowel(c byte) bool {
	switch c {
	case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
		return true
	}
	return false
}

// ParseAgentOutput parses text containing JSON per schema, using json-extract helpers.
func ParseAgentOutput(text string, schema AgentOutputSchema, agentLabel string) (AgentOutput, error) {
	parsed := internal.ParseAgentJSON(text, func(v any) bool {
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		_, err := ValidateAgentOutput(m, schema)
		return err == nil
	})
	if parsed != nil {
		if m, ok := parsed.(map[string]any); ok {
			return ValidateAgentOutput(m, schema)
		}
	}
	fallback := internal.ParseAgentJSON(text, nil)
	if fallback != nil {
		if m, ok := fallback.(map[string]any); ok {
			return ValidateAgentOutput(m, schema)
		}
	}
	return AgentOutput{}, fmt.Errorf("%s output did not contain a parseable JSON object", agentLabel)
}
