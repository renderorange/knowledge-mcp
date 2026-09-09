# knowledge-mcp

An MCP server for persistent agent knowledge. Agents query and write structured knowledge about projects — conventions, subsystem understanding, and design decisions — without re-reading source files every time.

## Quick Start

```bash
make build
```

Or with a specific version:

```bash
make build/1.2.3
```

Then configure your MCP client (see [Agent Configuration](#agent-configuration) below). The client manages the server process — you don't need to run it manually.

Check the version:

```bash
./knowledge-mcp --version
```

## Modes

### Single-project

Targets one project root. Indexes `.agents/` in that project.

```jsonc
// opencode.jsonc
{
  "mcp": {
    "knowledge-mcp": {
      "command": ["knowledge-mcp", "--project", "/path/to/project"],
      "type": "local"
    }
  }
}
```

### Org-wide

Discovers all projects under the org root. Indexes per-project `.agents/` and org-level `.agents/knowledge/`.

```jsonc
// opencode.jsonc
{
  "mcp": {
    "knowledge-mcp": {
      "command": ["knowledge-mcp", "--root", "/path/to/org"],
      "type": "local"
    }
  }
}
```

### Multi-root / multi-project

Repeat `--root` and `--project` as needed; they can be mixed:

```jsonc
// opencode.jsonc
{
  "mcp": {
    "knowledge-mcp": {
      "command": ["knowledge-mcp",
        "--root", "/home/blaine/git",
        "--root", "/home/blaine/work",
        "--project", "/scratch/proto"],
      "type": "local"
    }
  }
}
```

### Shared global store

`--global <path>` adds one knowledge store shared across every project. Querying or listing any project automatically merges the global store's entries alongside the project's own.

```jsonc
// opencode.jsonc
{
  "mcp": {
    "knowledge-mcp": {
      "command": ["knowledge-mcp",
        "--root", "/path/to/org",
        "--global", "/path/to/global-store"],
      "type": "local"
    }
  }
}
```

- The global store is addressed as `_global` and is also queryable directly.
- `list_knowledge` marks merged global sections with a `(global)` suffix so they are never mistaken for the project's own entries; projects with no entries of their own are explicitly reported as having none.
- The global store itself is just a directory with `.agents/<category>.yaml` files — it can live in its own git repo.

General notes:

- Each `--root` discovers its immediate children as projects (one level deep — pass deeper directories as additional flags).
- Duplicate project basenames across roots are addressed as `<root>/<project>` (e.g. `work/api`); `list_projects` shows which names need qualification.
- `query_knowledge` accepts org root names to search that root's `.agents/knowledge/` files. Org-level documents are indexed **per `##` section**, so a query returns the matching section(s), not the entire file.
- Multi-entry configurations store the search index under `$XDG_STATE_HOME/knowledge-mcp/` (default `~/.local/state/knowledge-mcp/`). Single-flag configurations keep the index inside their own `.agents/`.
- `--index <path>` overrides the index location in all modes.

## MCP Tools

| Tool | Description |
|------|-------------|
| `init_knowledge` | Create `.agents/` directory structure for a project |
| `write_knowledge` | Add a new knowledge entry |
| `query_knowledge` | Full-text search with category filters |
| `list_knowledge` | List all entries; entries with rules shown first as constraints |
| `update_knowledge` | Update an existing entry by ID |
| `list_projects` | List all discovered projects (org-wide mode only) |

Queries are plain-text search with no operator syntax — special characters in queries are always treated as literal text.

## Knowledge File Format

Entries are stored in `<project>/.agents/<category>.yaml`:

- `conventions.yaml` — patterns, naming, build commands
- `subsystems.yaml` — how things work
- `decisions.yaml` — why choices were made
- `_meta.yaml` — project metadata

### Rules / Constraints

Entries can include an optional `rule` field — an imperative statement of what's forbidden or required (e.g. `"Never create files outside ./tmp"`). `list_knowledge` surfaces entries with rules in a separate `## constraints` section above regular entries, so agents see hard rules immediately without needing to query detail.

```yaml
- id: conv-001
  summary: Tmp directory rules and session workflow
  detail: "All project docs go in ./tmp/docs/..."
  rule: "NEVER create files outside ./tmp; all docs go in ./tmp/docs/ only"
  source: "user preference"
  date: "2026-09-08"
```

## Agent Configuration

### OpenCode

Add to `opencode.jsonc`:

```jsonc
{
  "mcp": {
    "knowledge-mcp": {
      "command": ["knowledge-mcp", "--root", "/path/to/org"],
      "type": "local"
    }
  }
}
```

### Claude Code

Add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "knowledge-mcp": {
      "command": "knowledge-mcp",
      "args": ["--root", "/path/to/org"]
    }
  }
}
```

## Token Savings

The store saves tokens on **repeat lookups**, not on first write. Typical wins:

| Action | Without the store | With knowledge MCP |
|--------|-------------------|--------------------|
| Session-start rule recall | Re-read instruction files (~1-2K tokens/session) | `list_knowledge` (~300-500 tokens, constraints first) |
| Look up one domain's conventions | Grep + read the relevant docs (~2-5K tokens) | `query_knowledge` with a targeted query (~500-900 tokens) |
| Recall how a subsystem works | Re-read source files (~10-50K tokens) | `query_knowledge` for a written subsystem entry (~500-1K tokens) |

The savings only materialize if the content respects the format:

- **Detail is a summary** (10-20 lines of query-able facts), not a copy of the source files it was distilled from. Copying sources into `detail` doubles the token cost and saves nothing.
- **No duplication with always-loaded instructions.** Rules that also live in an auto-loaded `AGENTS.md` are read at session start anyway; the store earns its keep by holding the *detail* behind those rules, so agents query it only when they actually need the enforcement specifics.
- **Org-level queries get sections, not files.** `.agents/knowledge/*.md` is indexed per `##` heading, so `query_knowledge` on an org root returns only the matching sections.

Anti-patterns that erase the savings:

- Storing whole documents or source code in `detail`.
- Mirroring the same rules in both `AGENTS.md` and the store intentlessly.
- Running `query_knowledge` with no query against org-level docs (returns every section).

## License

MIT — Copyright (c) 2026 Blaine Motsinger. See [LICENSE](LICENSE).
