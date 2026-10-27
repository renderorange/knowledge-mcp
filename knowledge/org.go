package knowledge

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Org section errors. Append requires a fresh heading; replace requires an
// existing one; duplicate headings are ambiguous and rejected by both.
var (
	ErrOrgSectionExists    = errors.New("section already exists")
	ErrOrgSectionNotFound  = errors.New("section not found")
	ErrOrgDuplicateSection = errors.New("duplicate section heading")
)

// OrgSectionBody returns the body of the "## heading" section in data,
// excluding the heading line. It returns ErrOrgSectionNotFound when the
// heading is absent and ErrOrgDuplicateSection when it appears twice.
func OrgSectionBody(data, heading string) (string, error) {
	lines := splitOrgLines(data)
	idx := findOrgSections(lines, heading)
	if len(idx) == 0 {
		return "", ErrOrgSectionNotFound
	}
	if len(idx) > 1 {
		return "", ErrOrgDuplicateSection
	}
	end := orgSectionEnd(lines, idx[0])
	return strings.TrimSpace(strings.Join(lines[idx[0]+1:end], "\n")), nil
}

// AppendOrgSection appends a "## heading" section to data. It refuses to
// guess when the heading already exists.
func AppendOrgSection(data, heading, body string) (string, error) {
	idx := findOrgSections(splitOrgLines(data), heading)
	if len(idx) > 1 {
		return "", ErrOrgDuplicateSection
	}
	if len(idx) == 1 {
		return "", ErrOrgSectionExists
	}
	section := renderOrgSection(heading, body)
	if strings.TrimSpace(data) == "" {
		return section, nil
	}
	return strings.TrimRight(data, "\n") + "\n\n" + section, nil
}

// ReplaceOrgSection replaces the body of an existing "## heading" section,
// preserving the heading line and all content outside the section.
func ReplaceOrgSection(data, heading, body string) (string, error) {
	lines := splitOrgLines(data)
	idx := findOrgSections(lines, heading)
	if len(idx) == 0 {
		return "", ErrOrgSectionNotFound
	}
	if len(idx) > 1 {
		return "", ErrOrgDuplicateSection
	}

	start := idx[0]
	end := orgSectionEnd(lines, start)

	var b strings.Builder
	if prefix := strings.Join(lines[:start], "\n"); prefix != "" {
		b.WriteString(strings.TrimRight(prefix, "\n"))
		b.WriteString("\n\n")
	}
	b.WriteString(renderOrgSection(heading, body))
	if rest := lines[end:]; len(rest) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(rest, "\n"))
		b.WriteString("\n")
	}
	return b.String(), nil
}

// SaveText writes a text file atomically (temp file + fsync + rename), the
// same durability model as Save for YAML stores.
func SaveText(filePath, data string) error {
	tmpPath := filePath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := f.WriteString(data); err != nil {
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

func splitOrgLines(data string) []string {
	lines := strings.Split(data, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func findOrgSections(lines []string, heading string) []int {
	var idx []int
	for i, line := range lines {
		rest, ok := strings.CutPrefix(line, "## ")
		if ok && strings.TrimSpace(rest) == heading {
			idx = append(idx, i)
		}
	}
	return idx
}

func orgSectionEnd(lines []string, start int) int {
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			return i
		}
	}
	return len(lines)
}

func renderOrgSection(heading, body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "## " + heading + "\n"
	}
	return "## " + heading + "\n\n" + body + "\n"
}
