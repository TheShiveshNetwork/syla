package taskfile

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type TaskFile struct {
	Frontmatter Frontmatter `yaml:",inline"`
	Body        string
	Path        string
}

type Frontmatter struct {
	Agent          string  `yaml:"agent"`
	Mode           string  `yaml:"mode"`
	Workspace      string  `yaml:"workspace"`
	MaxIterations  *int    `yaml:"max_iterations"`
	MaxDuration    string  `yaml:"max_duration"`
	BudgetUSD      *float64 `yaml:"budget_usd"`
	MaxTokens      *int    `yaml:"max_tokens"`
	StopOn         string  `yaml:"stop_on"`
	StopWhen       string  `yaml:"stop_when"`
	Resume         string  `yaml:"resume"`
	KeepAwake      *bool   `yaml:"keep_awake"`
	PreventSleep   *bool   `yaml:"prevent_sleep"`
	Model          string  `yaml:"model"`
	MaxRateLimitWait string `yaml:"max_rate_limit_wait"`
}

func Parse(path string) (*TaskFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseBytes(path, data)
}

func ParseBytes(path string, data []byte) (*TaskFile, error) {
	text := string(data)
	if !strings.HasPrefix(strings.TrimSpace(text), "---") {
		return &TaskFile{Body: text, Path: path}, nil
	}
	parts := strings.SplitN(text, "---", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid frontmatter: missing closing ---")
	}
	fmRaw := parts[1]
	body := strings.TrimSpace(parts[2])
	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(fmRaw), &fm); err != nil {
		return nil, fmt.Errorf("frontmatter yaml: %w", err)
	}
	// normalize aliases
	if fm.StopWhen == "" && fm.StopOn != "" {
		fm.StopWhen = fm.StopOn
	}
	if fm.PreventSleep == nil && fm.KeepAwake != nil {
		fm.PreventSleep = fm.KeepAwake
	}
	return &TaskFile{Frontmatter: fm, Body: body, Path: path}, nil
}

func (f *Frontmatter) GetMaxDuration() (time.Duration, error) {
	if f.MaxDuration == "" {
		return 0, nil
	}
	return time.ParseDuration(f.MaxDuration)
}

func (f *Frontmatter) GetMaxRateLimitWait() (time.Duration, error) {
	if f.MaxRateLimitWait == "" {
		return 24 * time.Hour, nil
	}
	return time.ParseDuration(f.MaxRateLimitWait)
}

func (f *Frontmatter) GetKeepAwake() bool {
	if f.PreventSleep != nil {
		return *f.PreventSleep
	}
	if f.KeepAwake != nil {
		return *f.KeepAwake
	}
	return true
}
