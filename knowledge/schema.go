package knowledge

import (
	"time"
)

const stalenessMonths = 6

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
	ID           string   `yaml:"id"`
	Summary      string   `yaml:"summary"`
	Detail       string   `yaml:"detail"`
	Confidence   string   `yaml:"confidence"`
	Source       string   `yaml:"source"`
	Date         string   `yaml:"date"`
	Supersedes   string   `yaml:"supersedes,omitempty"`
	Alternatives []string `yaml:"alternatives,omitempty"`
	ExpiresAt    string   `yaml:"expires_at,omitempty"`
	LastVerified string   `yaml:"last_verified,omitempty"`
}

// IsStale returns true if the entry has passed its expiry date.
// Returns false if ExpiresAt is not set (backward compatibility).
func (e Entry) IsStale() bool {
	if e.ExpiresAt == "" {
		return false
	}
	today := Today()
	if e.ExpiresAt >= today {
		return false
	}
	if e.LastVerified == "" {
		return true
	}
	return e.LastVerified < e.ExpiresAt
}

// ExpiryDate returns the default expiry date (6 months from now).
func ExpiryDate() string {
	return time.Now().AddDate(0, stalenessMonths, 0).Format("2006-01-02")
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

// IsValidConfidence checks if a confidence level is valid.
func IsValidConfidence(confidence string) bool {
	return confidence == "high" || confidence == "medium" || confidence == "low"
}
