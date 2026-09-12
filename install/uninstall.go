package install

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RunUninstall parses uninstall flags and detaches the client integration.
func RunUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	removeBinary := fs.Bool("remove-binary", false, "also delete the recorded knowledge-mcp binary")
	_ = fs.Parse(args)

	reports, err := runUninstall(*removeBinary)
	for _, r := range reports {
		fmt.Println(r)
	}
	return err
}

// runUninstall removes exactly what install wrote. It never touches
// .agents/ dirs, stores, or indexes.
func runUninstall(removeBinary bool) ([]string, error) {
	rec, ok := LoadRecord()
	if !ok {
		return nil, errors.New("no install record found at " + recordPath() + "; nothing to uninstall (knowledge data is never touched)")
	}

	var reports []string

	// Strip the AGENTS.md block (marker-delimited only).
	if err := stripFileBlock(filepath.Join(OpencodeDir(), "AGENTS.md"), agentsStartMark, agentsEndMark, &reports); err != nil {
		return reports, err
	}

	// Strip the opencode config marker block.
	if cfgPath, err := OpencodeConfigPath(); err == nil {
		if serr := stripFileBlock(cfgPath, mcpMarkerStart, mcpMarkerEnd, &reports); serr != nil {
			return reports, serr
		}
	}

	// Delete generated full-file artifacts.
	for _, art := range rec.Artifacts {
		switch art.Kind {
		case "skill", "plugin":
			if err := os.Remove(art.Path); err != nil && !os.IsNotExist(err) {
				reports = append(reports, "skip "+art.Path+": "+err.Error())
				continue
			}
			reports = append(reports, "removed "+art.Path)
		}
	}
	// Remove the skill dir if we made it and it is now empty.
	skillDir := filepath.Join(OpencodeDir(), "skills", "knowledge-mcp")
	_ = os.Remove(skillDir) // rmdir semantics: fails silently if non-empty
	reports = append(reports, "removed "+skillDir)

	if err := os.Remove(recordPath()); err != nil && !os.IsNotExist(err) {
		return reports, fmt.Errorf("remove record: %w", err)
	}
	reports = append(reports, "removed "+recordPath())

	if removeBinary && rec.BinPath != "" {
		if err := os.Remove(rec.BinPath); err != nil && !os.IsNotExist(err) {
			reports = append(reports, "skip binary "+rec.BinPath+": "+err.Error())
		} else {
			reports = append(reports, "removed binary "+rec.BinPath)
		}
	}

	return reports, nil
}

// stripFileBlock strips a marker-delimited region (markers inclusive) from
// file at path. Missing file or missing markers -> skip report, no error.
func stripFileBlock(path, startMark, endMark string, reports *[]string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		*reports = append(*reports, "skip "+path+": not found")
		return nil
	}
	stripped := stripBlock(string(b), startMark, endMark)
	if stripped == string(b) {
		*reports = append(*reports, "skip "+path+": markers not found; left untouched")
		return nil
	}
	if err := os.WriteFile(path, []byte(stripped), 0644); err != nil {
		return fmt.Errorf("strip %s: %w", path, err)
	}
	*reports = append(*reports, "stripped "+path)
	return nil
}

// stripBlock removes a marker-delimited region (markers inclusive).
func stripBlock(content, startMark, endMark string) string {
	start := strings.Index(content, startMark)
	end := strings.Index(content, endMark)
	if start < 0 || end <= start {
		return content
	}
	endLine := strings.Index(content[end:], "\n")
	if endLine < 0 {
		return content[:start] + content[len(content):]
	}
	return content[:start] + content[end+endLine+1:]
}
