# agents_knowledge

An MCP server for persistent agent knowledge. Agents query and write structured knowledge about projects — conventions, subsystem understanding, and design decisions — without re-reading source files every time.

## Quick Start

```bash
go build -o knowledge-mcp .
./knowledge-mcp --project /path/to/your/project
```

## Modes

### Single-project

```bash
./knowledge-mcp --project /path/to/project
```

Indexes `.agents/` in the given project.

### Org-wide

```bash
./knowledge-mcp --root /path/to/org
```

Discovers all projects under the org root. Indexes per-project `.agents/` and org-level `.agents/knowledge/`.

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
