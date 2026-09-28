# knowledge-mcp

An MCP server for persistent agent knowledge. Agents query and write structured knowledge about projects — conventions, subsystem understanding, and design decisions — without re-reading source files every time.

## Quick Start

One command downloads the binary for your platform and wires everything up
(OpenCode MCP entry, agent instructions block, skill, and the Grep/Glob
augment hook):

```bash
curl -fsSL https://raw.githubusercontent.com/renderorange/knowledge-mcp/main/install.sh -o install.sh
chmod +x install.sh
./install.sh --root /path/to/org --global /path/to/global-store
```

Flags passed to `install.sh` become the server's flags (`--root`,
`--project`, `--global`, `--store`, `--index`). `--version <tag>` pins a
specific release; `--dry-run` previews what would happen.

What install writes:

- the MCP server entry in `~/.config/opencode/opencode.jsonc` (marker-delimited)
- an instruction block in `~/.config/opencode/AGENTS.md`
- the `knowledge-mcp` skill at `~/.config/opencode/skills/knowledge-mcp/SKILL.md`
- the augment plugin at `~/.config/opencode/plugins/knowledge-mcp.ts`
- an install record at `~/.config/knowledge-mcp/install.json`

The plugin intercepts Grep/Glob and appends matching knowledge-store entries
to the results, so agent searches surface stored conventions automatically.

Remove the integration (data-safe — store files are never touched):

```bash
knowledge-mcp uninstall [--remove-binary]
```

The client manages the server process — you don't need to run it manually.

### Build From Source

```bash
make build
```

Or with a specific version:

```bash
make build/1.2.3
```

Check the version:

```bash
./knowledge-mcp --version
```

## Behavior evals

`make eval-mock` runs the deterministic mock tier (no API key); `make eval` runs the
full real-tier agent-behavior suite (needs a model provider). Scenarios live in
`evals/scenarios/*.yaml` with mock contracts in `evals/mock_agents/*.jsonl`.

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

### Central store

`--store <dir>` re-roots every knowledge store under one central directory,
keeping repos clean of `.agents/`:

```jsonc
// opencode.jsonc
{
  "mcp": {
    "knowledge-mcp": {
      "command": ["knowledge-mcp", "--root", "/path/to/org", "--store", "/path/to/knowledge"],
      "type": "local"
    }
  }
}
```

Layout:

```
<store>/
  .index/                          # bleve index (default; --index wins)
  <project>/.agents/               # per-project store (qualified names nest:
                                   #   <root>/<project> -> <store>/<root>/<project>/.agents)
  <org>/.agents/knowledge/         # org-level markdown
```

- In-tree `.agents/` dirs are ignored (startup warnings list them). Migrate
  by moving them: `mkdir -p <store>/<address> && mv <project>/.agents <store>/<address>/.agents`.
- Only `--global` stays where it is; it merges into listings as usual.
- `init_knowledge` initializes the central store for resolvable paths and
  errors for others (add them via `--project`/`--root` first).
- The store directory itself must exist and is never created for you.
- The store is single-writer: its search index and files do not support
  concurrent access, so point only one server at a store.

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
- Multi-entry configurations store the search index under `$XDG_STATE_HOME/knowledge-mcp/` (default `~/.local/state/knowledge-mcp/`). Single-flag configurations keep the index inside their own `.agents/` — unless `--store` is set, in which case the index lives at `<store>/.index`.
- `--index <path>` overrides the index location in all modes.

### Startup Options

`--no-index-on-startup` skips background indexing on startup. The server starts
immediately with stale or empty index data; queries block until indexing completes
if no stale data is available. Useful for large knowledge stores where startup
indexing would delay server readiness.

### Debugging

`knowledge-mcp debug` runs a one-shot startup diagnosis against the same
flags as server mode and exits non-zero when it finds a problem (unresolved
project, stale or missing index, blocked queries):

```bash
knowledge-mcp debug --root /path/to/org
```

The report has three sections — Resolution (projects/stores resolved),
Startup trail (phase timings and a watchdog-bounded index self-query that
flags the cold-index query block), and Environment (version, index
location/source, doc count, freshness, install record) — followed by
`ok`/`warn`/`fail` findings.

For live diagnostics, run the server with `--debug` (optionally
`--log-file <path>` to tee). Lines go to stderr, which MCP clients such as
opencode forward into their own logs. Tool calls are logged with sanitized
arguments — entry bodies are replaced by sizes and never written. Set
`KNM_DEBUG=1` to get the same for the Grep/Glob `hook-augment` plugin hook.

## MCP Tools

| Tool | Description |
|------|-------------|
| `init_knowledge` | Create a project's knowledge directory structure (central store under `--store`) |
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

Category files use a `project:`/`version:`/`entries:` wrapper — a bare YAML
list parses as zero entries. `list_knowledge` marks entries merged from a
shared global store with a `(global)` suffix so they are never mistaken for
the project's own entries.

### Rules / Constraints

Entries can include an optional `rule` field — an imperative statement of what's forbidden or required (e.g. `"Never create files outside ./tmp"`). `list_knowledge` surfaces entries with rules in a separate `## constraints` section above regular entries, so agents see hard rules immediately without needing to query detail.

```yaml
project: my-project
version: 1
entries:
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
