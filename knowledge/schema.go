package knowledge

import (
	"time"
)

// Meta represents the _meta.yaml file header.
type Meta struct {
	Project          string   `yaml:"project"`
	KnowledgeVersion int      `yaml:"knowledge_version"`
	Created          string   `yaml:"created"`
	LastUpdated      string   `yaml:"last_updated"`
	Categories       []string `yaml:"categories"`
}

// Entry represents a single knowledge entry.
type Entry struct {
	ID         string `yaml:"id"`
	Summary    string `yaml:"summary"`
	Detail     string `yaml:"detail"`
	Rule       string `yaml:"rule,omitempty"`
	Source     string `yaml:"source"`
	Date       string `yaml:"date"`
	Supersedes string `yaml:"supersedes,omitempty"`
}

// KnowledgeFile represents a .agents/<category>.yaml file.
type KnowledgeFile struct {
	Project string  `yaml:"project"`
	Version int     `yaml:"version"`
	Entries []Entry `yaml:"entries"`
}

// Today returns the current date in ISO format.
func Today() string {
	return time.Now().Format("2006-01-02")
}

// ValidCategories returns the set of valid category names.
func ValidCategories() []string {
	return []string{"conventions", "subsystems", "decisions"}
}

// CategoryPrefix maps category names to their ID prefixes.
func CategoryPrefix(category string) string {
	switch category {
	case "conventions":
		return "conv"
	case "subsystems":
		return "sub"
	case "decisions":
		return "dec"
	default:
		return ""
	}
}

// IsValidCategory checks if a category name is valid.
func IsValidCategory(category string) bool {
	for _, c := range ValidCategories() {
		if c == category {
			return true
		}
	}
	return false
}
