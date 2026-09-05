# agents_knowledge

An MCP server for persistent agent knowledge. Agents query and write structured knowledge about projects — conventions, subsystem understanding, and design decisions — without re-reading source files every time.

## Quick Start

```bash
go build -o knowledge-mcp .
```

Then configure your MCP client (see [Agent Configuration](#agent-configuration) below). The client manages the server process — you don't need to run it manually.

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

- Each `--root` discovers its immediate children as projects (one level deep — pass deeper directories as additional flags).
- Duplicate project basenames across roots are addressed as `<root>/<project>` (e.g. `work/api`); `list_projects` shows which names need qualification.
- `query_knowledge` accepts org root names to search that root's `.agents/knowledge/` files.
- Multi-entry configurations store the search index under `$XDG_STATE_HOME/knowledge-mcp/` (default `~/.local/state/knowledge-mcp/`). Single-flag configurations keep the index inside their own `.agents/`.
- `--index <path>` overrides the index location in all modes.

## MCP Tools

| Tool | Description |
|------|-------------|
| `init_knowledge` | Create `.agents/` directory structure for a project |
| `write_knowledge` | Add a new knowledge entry |
| `query_knowledge` | Full-text search with category/confidence filters |
| `list_knowledge` | List all entries (summaries only) |
| `update_knowledge` | Update an existing entry by ID |

## Knowledge File Format

Entries are stored in `<project>/.agents/<category>.yaml`:

- `conventions.yaml` — patterns, naming, build commands
- `subsystems.yaml` — how things work
- `decisions.yaml` — why choices were made
- `_meta.yaml` — project metadata

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

| Action | File read | Knowledge MCP |
|--------|-----------|---------------|
| Get conventions | ~5K tokens | ~500 tokens |
| Understand subsystem | ~50-100K tokens | ~1K tokens |
| Check past decisions | ~3K tokens | ~300 tokens |
