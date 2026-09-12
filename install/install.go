package install

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/renderorange/knowledge-mcp/projects"
	"github.com/renderorange/knowledge-mcp/tools"
)

type strSlice []string

func (s *strSlice) String() string { return fmt.Sprintf("%v", []string(*s)) }
func (s *strSlice) Set(v string) error {
	if v == "" {
		return errors.New("empty path")
	}
	*s = append(*s, v)
	return nil
}

// installConfig is the all-flags input to an install run. runInstall is the
// pure core; Run parses argv and fills it in.
type installConfig struct {
	Roots    []string
	Projects []string
	Global   string
	Store    string
	Index    string
	Version  string
	BinPath  string
}

// Run parses install flags and performs an install.
func Run(args []string, version string) error {
	var roots, projects strSlice
	var globalPath, storeDir, indexOverride string
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	fs.Var(&roots, "root", "Org root whose immediate children are projects (repeatable)")
	fs.Var(&projects, "project", "Single project root (repeatable)")
	fs.StringVar(&globalPath, "global", "", "Path to a global knowledge store shared across all projects")
	fs.StringVar(&storeDir, "store", "", "Central directory for all knowledge stores; in-tree .agents/ is ignored when set")
	fs.StringVar(&indexOverride, "index", "", "Override the search index location")
	_ = fs.Parse(args)

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	reports, err := runInstall(installConfig{
		Roots: roots, Projects: projects, Global: globalPath,
		Store: storeDir, Index: indexOverride, Version: version, BinPath: binPath,
	})
	for _, r := range reports {
		fmt.Println(r)
	}
	return err
}

func runInstall(cfg installConfig) ([]string, error) {
	if len(cfg.Roots) == 0 && len(cfg.Projects) == 0 && cfg.Global == "" {
		return nil, errors.New("error: at least one --project, --root, or --global is required")
	}

	// Same resolution semantics as the server.
	if _, _, err := projects.BuildWithStore(cfg.Roots, cfg.Projects, cfg.Global, cfg.Store); err != nil {
		return nil, fmt.Errorf("error: %w", err)
	}

	flags := commandFlags(cfg)

	// Merge the opencode config first — the riskiest step. On failure print
	// the paste-able snippet and abort before writing anything else.
	configPath, err := OpencodeConfigPath()
	if err != nil {
		return nil, fmt.Errorf("opencode config: %w", err)
	}
	var existingCfg []byte
	if b, rerr := os.ReadFile(configPath); rerr == nil {
		existingCfg = b
	}
	merged, changed, mergeErr := MergeMCPEntry(existingCfg, cfg.BinPath, flags)
	if mergeErr != nil {
		entry, _ := renderEntry(cfg.BinPath, flags, "  ")
		fmt.Fprintf(os.Stderr, "error: could not merge the MCP entry into %s: %v\n", configPath, mergeErr)
		fmt.Fprintf(os.Stderr, "Add the following to the \"mcp\" section of %s manually:\n%s", configPath, entry)
		return nil, mergeErr
	}
	if changed {
		if err := os.WriteFile(configPath, merged, 0644); err != nil {
			return nil, fmt.Errorf("write %s: %w", configPath, err)
		}
	}

	artifacts := []Artifact{{Path: configPath, Kind: "mcp_entry_block"}}
	var reports []string

	skillDir := filepath.Join(OpencodeDir(), "skills", "knowledge-mcp")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return nil, fmt.Errorf("skill dir: %w", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, RenderSkillMD(tools.Registry), 0644); err != nil {
		return nil, fmt.Errorf("skill: %w", err)
	}
	reports = append(reports, "installed skill "+skillPath)
	artifacts = append(artifacts, Artifact{Path: skillPath, Kind: "skill"})

	pluginPath := filepath.Join(OpencodeDir(), "plugins", "knowledge-mcp.ts")
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0755); err != nil {
		return nil, fmt.Errorf("plugin dir: %w", err)
	}
	if err := os.WriteFile(pluginPath, RenderPlugin(cfg.BinPath), 0644); err != nil {
		return nil, fmt.Errorf("plugin: %w", err)
	}
	reports = append(reports, "installed plugin "+pluginPath)
	artifacts = append(artifacts, Artifact{Path: pluginPath, Kind: "plugin"})

	agentsPath := filepath.Join(OpencodeDir(), "AGENTS.md")
	var agentsContent []byte
	if b, rerr := os.ReadFile(agentsPath); rerr == nil {
		agentsContent = b
	}
	upserted := UpsertBlock(agentsContent, RenderAgentsBlock(tools.Registry), agentsStartMark, agentsEndMark)
	if err := os.WriteFile(agentsPath, upserted, 0644); err != nil {
		return nil, fmt.Errorf("AGENTS.md: %w", err)
	}
	reports = append(reports, "installed AGENTS.md block "+agentsPath)
	artifacts = append(artifacts, Artifact{Path: agentsPath, Kind: "agents_md_block"})

	rec := Record{
		Version: cfg.Version, BinPath: cfg.BinPath,
		Roots: cfg.Roots, Projects: cfg.Projects, Global: cfg.Global,
		Store: cfg.Store, Index: cfg.Index, Artifacts: artifacts,
	}
	if err := SaveRecord(rec); err != nil {
		return nil, fmt.Errorf("record: %w", err)
	}
	reports = append(reports, "installed record "+recordPath())
	reports = append(reports, "note: restart opencode to load the new MCP server and plugin")
	return reports, nil
}

// commandFlags expands cfg into the server command array recorded in the
// opencode mcp entry.
func commandFlags(cfg installConfig) []string {
	var flags []string
	for _, r := range cfg.Roots {
		flags = append(flags, "--root", r)
	}
	for _, p := range cfg.Projects {
		flags = append(flags, "--project", p)
	}
	if cfg.Global != "" {
		flags = append(flags, "--global", cfg.Global)
	}
	if cfg.Store != "" {
		flags = append(flags, "--store", cfg.Store)
	}
	if cfg.Index != "" {
		flags = append(flags, "--index", cfg.Index)
	}
	return flags
}
