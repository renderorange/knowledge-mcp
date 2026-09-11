package install

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Record struct {
	Version   string     `json:"version"`
	BinPath   string     `json:"bin_path"`
	Roots     []string   `json:"roots"`
	Projects  []string   `json:"projects"`
	Global    string     `json:"global"`
	Store     string     `json:"store"`
	Index     string     `json:"index"`
	Artifacts []Artifact `json:"artifacts"`
}

type Artifact struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // "agents_md_block" | "mcp_entry_block" | "skill" | "plugin"
}

func recordPath() string {
	return filepath.Join(ConfigDir(), "install.json")
}

// SaveRecord writes the record via temp + rename so a crashed install never
// leaves a half-written JSON file behind.
func SaveRecord(record Record) error {
	if err := os.MkdirAll(ConfigDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	tmp := recordPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, recordPath())
}

// LoadRecord reads the record; ok is false when absent or unparseable, in
// which case callers treat the install as absent.
func LoadRecord() (Record, bool) {
	data, err := os.ReadFile(recordPath())
	if err != nil {
		return Record{}, false
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, false
	}
	return rec, true
}