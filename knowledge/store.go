package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load reads a YAML file and returns a KnowledgeFile.
func Load(filePath string) (*KnowledgeFile, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var kf KnowledgeFile
	if err := yaml.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}

	return &kf, nil
}

// Save writes a KnowledgeFile to a YAML file atomically (temp file + fsync + rename).
func Save(filePath string, kf *KnowledgeFile) error {
	data, err := yaml.Marshal(kf)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}

	tmpPath := filePath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// NextID generates the next sequential ID for a category.
// Given existing entries with prefix "conv" and IDs like conv-001, conv-002,
// it returns "conv-003".
func NextID(entries []Entry, prefix string) string {
	maxNum := 0
	for _, e := range entries {
		if strings.HasPrefix(e.ID, prefix+"-") {
			numStr := strings.TrimPrefix(e.ID, prefix+"-")
			num, err := strconv.Atoi(numStr)
			if err == nil && num > maxNum {
				maxNum = num
			}
		}
	}
	return fmt.Sprintf("%s-%03d", prefix, maxNum+1)
}

// CategoryFilePath returns the path to a category YAML file within .agents/.
func CategoryFilePath(agentsDir, category string) string {
	return filepath.Join(agentsDir, category+".yaml")
}

// MetaFilePath returns the path to _meta.yaml within .agents/.
func MetaFilePath(agentsDir string) string {
	return filepath.Join(agentsDir, "_meta.yaml")
}

// EnsureDir creates the .agents/ directory if it doesn't exist.
func EnsureDir(agentsDir string) error {
	return os.MkdirAll(agentsDir, 0755)
}

// FileExists checks if a file exists.
func FileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

// LoadOrCreate loads a KnowledgeFile or creates an empty one.
// Callers must hold the file lock to prevent TOCTOU races.
func LoadOrCreate(filePath, project string) (*KnowledgeFile, error) {
	kf, err := Load(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &KnowledgeFile{
				Project: project,
				Version: 1,
				Entries: []Entry{},
			}, nil
		}
		return nil, err
	}
	return kf, nil
}
