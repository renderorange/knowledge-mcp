package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type FileFixture struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
}

type Setup struct {
	Files []FileFixture `yaml:"files"`
}

type TranscriptAssert struct {
	Pattern string `yaml:"pattern"`
	Must    bool   `yaml:"must"`
}

type SideEffect struct {
	Type  string   `yaml:"type"`
	Path  string   `yaml:"path"`
	Allow []string `yaml:"allow"`
}

type ToolCallAssert struct {
	Tool string `yaml:"tool"`
	Must bool   `yaml:"must"`
}

type AssertBlock struct {
	Transcript  []TranscriptAssert `yaml:"transcript"`
	SideEffects []SideEffect       `yaml:"side_effects"`
	ToolCalls   []ToolCallAssert   `yaml:"tool_calls"`
}

type Judge struct {
	Rubric string `yaml:"rubric"`
	Scale  string `yaml:"scale"`
	Min    int    `yaml:"min"`
}

type Scenario struct {
	ID         string      `yaml:"id"`
	Name       string      `yaml:"name"`
	Tier       string      `yaml:"tier"`
	Tags       []string    `yaml:"tags"`
	Prompt     string      `yaml:"prompt"`
	Setup      Setup       `yaml:"setup"`
	Assert     AssertBlock `yaml:"assert"`
	Judge      *Judge      `yaml:"judge"`
	Retry      int         `yaml:"retry"`
	TimeoutSec int         `yaml:"timeout_sec"`
}

var validTiers = map[string]bool{"both": true, "real": true, "mock": true}
var validSideEffects = map[string]bool{
	"path_exists": true, "path_absent": true,
	"git_rev_count_unchanged": true, "forbidden_new_paths": true,
}

func LoadScenario(path string) (*Scenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var sc Scenario
	if err := dec.Decode(&sc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := sc.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &sc, nil
}

func LoadDir(dir string) ([]*Scenario, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	out := make([]*Scenario, 0, len(paths))
	for _, p := range paths {
		sc, err := LoadScenario(p)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, nil
}

func (s *Scenario) Validate() error {
	if s.ID == "" || s.Name == "" || strings.TrimSpace(s.Prompt) == "" {
		return fmt.Errorf("id, name, and prompt are required")
	}
	if !validTiers[s.Tier] {
		return fmt.Errorf("tier %q must be both, real, or mock", s.Tier)
	}
	for _, se := range s.Assert.SideEffects {
		if !validSideEffects[se.Type] {
			return fmt.Errorf("unknown side_effects type %q", se.Type)
		}
	}
	if s.Judge != nil && strings.TrimSpace(s.Judge.Rubric) == "" {
		return fmt.Errorf("judge.rubric is required when judge is present")
	}
	if s.TimeoutSec < 0 || s.Retry < 0 {
		return fmt.Errorf("retry and timeout_sec must be >= 0")
	}
	return nil
}
