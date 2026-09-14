package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	mcpMarkerStart  = "// knowledge-mcp:start"
	mcpMarkerEnd    = "// knowledge-mcp:end"
	permMarkerStart = "// knowledge-mcp:permission:start"
	permMarkerEnd   = "// knowledge-mcp:permission:end"
)

var errHandWrittenEntry = errors.New("hand-written knowledge-mcp entry without knowledge-mcp markers")

// MergeMCPEntry adds (or refreshes) the knowledge-mcp MCP entry in opencode
// config text, preserving jsonc comments and unrelated keys.
func MergeMCPEntry(existing []byte, binPath string, flags []string) ([]byte, bool, error) {
	content := string(existing)

	if strings.Contains(content, mcpMarkerStart) && strings.Contains(content, mcpMarkerEnd) {
		return replaceMarkedRegion(content, binPath, flags)
	}

	if handWritten(content) {
		return nil, false, errHandWrittenEntry
	}

	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		entry, _ := renderEntry(binPath, flags, "  ")
		out := "{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"mcp\": {\n" + entry + "  },\n}\n"
		return []byte(out), true, nil
	}

	indent := detectIndent(content)
	entry, _ := renderEntry(binPath, flags, indent)

	mcpRE := regexp.MustCompile(`(^|\n)([ \t]*)"mcp"[ \t]*:[ \t]*\{`)
	if loc := mcpRE.FindStringSubmatchIndex(content); loc != nil {
		// loc is for the whole match; the `{` sits at the end. Append the
		// entry right after the opening brace.
		insertAt := loc[1]
		out := content[:insertAt] + "\n" + entry + content[insertAt:]
		if err := sanityCheck(out); err != nil {
			return nil, false, err
		}
		return []byte(out), true, nil
	}

	// No mcp key: append one before the final closing brace. The preceding
	// key/value needs a comma unless the file ends inside an empty object.
	idx := strings.LastIndex(content, "}")
	if idx < 0 {
		return nil, false, errors.New("no closing brace found and no mcp key; cannot merge")
	}
	before := strings.TrimSpace(content[:idx])
	pfx := ""
	if !strings.HasSuffix(before, ",") && !strings.HasSuffix(before, "{") {
		pfx = ","
	}
	out := strings.TrimRight(content[:idx], " \t\n") + pfx + "\n" + indent + "\"mcp\": {\n" + entry + indent + "},\n" + content[idx:]
	if err := sanityCheck(out); err != nil {
		return nil, false, err
	}
	return []byte(out), true, nil
}

// MergePermissionRule adds (or refreshes) the external_directory permission
// rule for the knowledge-mcp state directory.
func MergePermissionRule(existing []byte, stateDir, homeDir string) ([]byte, bool, error) {
	content := string(existing)
	rel := strings.TrimPrefix(stateDir, homeDir)
	rel = strings.TrimPrefix(rel, "/")
	tildePath := "~/" + rel + "/**"

	if strings.Contains(content, permMarkerStart) && strings.Contains(content, permMarkerEnd) {
		return replacePermMarkedRegion(content, tildePath)
	}

	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		entry := renderPermEntry(tildePath, "  ")
		out := "{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"permission\": {\n" + entry + "  },\n}\n"
		return []byte(out), true, nil
	}

	indent := detectIndent(content)
	entry := renderPermEntry(tildePath, indent)

	permRE := regexp.MustCompile(`(^|\n)([ \t]*)"permission"[ \t]*:[ \t]*\{`)
	if loc := permRE.FindStringSubmatchIndex(content); loc != nil {
		insertAt := loc[1]
		out := content[:insertAt] + "\n" + entry + content[insertAt:]
		if err := sanityCheck(out); err != nil {
			return nil, false, err
		}
		return []byte(out), true, nil
	}

	idx := strings.LastIndex(content, "}")
	if idx < 0 {
		return nil, false, errors.New("no closing brace found and no permission key; cannot merge")
	}
	before := strings.TrimSpace(content[:idx])
	pfx := ""
	if !strings.HasSuffix(before, ",") && !strings.HasSuffix(before, "{") {
		pfx = ","
	}
	out := strings.TrimRight(content[:idx], " \t\n") + pfx + "\n" + indent + "\"permission\": {\n" + entry + indent + "},\n" + content[idx:]
	if err := sanityCheck(out); err != nil {
		return nil, false, err
	}
	return []byte(out), true, nil
}

func replacePermMarkedRegion(content, tildePath string) ([]byte, bool, error) {
	startIdx := strings.Index(content, permMarkerStart)
	endIdx := strings.Index(content, permMarkerEnd)

	markedRegion := content[startIdx : endIdx+len(permMarkerEnd)]
	if strings.Contains(markedRegion, tildePath) {
		return []byte(content), false, nil
	}

	after := content[endIdx:]
	newline := strings.Index(after, "\n")
	replaceEnd := len(content)
	if newline >= 0 {
		replaceEnd = endIdx + newline + 1
	}

	lineStart := strings.LastIndex(content[:startIdx], "\n") + 1
	indent := content[lineStart:startIdx]

	entry := renderPermEntry(tildePath, indent)
	out := content[:lineStart] + entry + content[replaceEnd:]
	if err := sanityCheck(out); err != nil {
		return nil, false, err
	}
	return []byte(out), true, nil
}

func renderPermEntry(tildePath, space string) string {
	inner := space + "  "
	return strings.Join([]string{
		space + permMarkerStart,
		space + "\"external_directory\": {",
		inner + "\"" + tildePath + "\": \"allow\"",
		space + "},",
		space + permMarkerEnd + "\n",
	}, "\n")
}

func mustHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic("resolve home: " + err.Error())
	}
	return home
}

func replaceMarkedRegion(content, binPath string, flags []string) ([]byte, bool, error) {
	startIdx := strings.Index(content, mcpMarkerStart)
	endIdx := strings.Index(content, mcpMarkerEnd)
	after := content[endIdx:]
	newline := strings.Index(after, "\n")
	replaceEnd := len(content)
	if newline >= 0 {
		replaceEnd = endIdx + newline + 1
	}

	lineStart := strings.LastIndex(content[:startIdx], "\n") + 1
	indent := content[lineStart:startIdx]

	entry, _ := renderEntry(binPath, flags, indent)
	out := content[:lineStart] + entry + content[replaceEnd:]
	if err := sanityCheck(out); err != nil {
		return nil, false, err
	}
	return []byte(out), true, nil
}

func handWritten(content string) bool {
	return regexp.MustCompile(`(?m)^[ \t]*"knowledge-mcp"[ \t]*:`).MatchString(content)
}

// renderEntry returns the marker-delimited entry block, indented with
// `space`, plus the indent string itself.
func renderEntry(binPath string, flags []string, space string) (string, string) {
	cmd := append([]string{binPath}, flags...)
	var parts []string
	for _, c := range cmd {
		b, _ := json.Marshal(c)
		parts = append(parts, string(b))
	}
	cmdJSON := "[" + strings.Join(parts, ", ") + "]"
	inner := space + "  "
	lines := []string{
		space + mcpMarkerStart,
		space + "\"knowledge-mcp\": {",
		inner + "\"command\": " + string(cmdJSON) + ",",
		inner + "\"type\": \"local\"",
		space + "},",
		space + mcpMarkerEnd + "\n",
	}
	return strings.Join(lines, "\n"), space
}

func detectIndent(content string) string {
	if m := regexp.MustCompile(`(?m)^([ \t]*)"mcp"`).FindStringSubmatch(content); m != nil {
		return m[1] + "  "
	}
	if m := regexp.MustCompile(`(?m)^([ \t]*)"[^"]+"[ \t]*:`).FindStringSubmatch(content); m != nil {
		return m[1]
	}
	return "  "
}

// sanityCheck rejects edits that would produce unbalanced braces outside
// strings — a proxy for "jsonc still plausibly parses".
func sanityCheck(content string) error {
	inStr, esc := false, false
	depth := 0
	for _, r := range content {
		switch {
		case inStr && esc:
			esc = false
		case inStr && r == '\\':
			esc = true
		case inStr && r == '"':
			inStr = false
		case !inStr && r == '"':
			inStr = true
		case !inStr && r == '{':
			depth++
		case !inStr && r == '}':
			depth--
		}
	}
	if depth != 0 {
		return fmt.Errorf("refusing edit: braces unbalanced (depth %d)", depth)
	}
	return nil
}
