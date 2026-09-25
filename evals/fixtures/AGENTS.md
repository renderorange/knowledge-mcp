# Agent Instructions

## Session Startup (MANDATORY)

Before ANY response: `mkdir -p ./tmp/docs && cat ./tmp/AGENTS_SESSION.md 2>/dev/null || echo "New session"`
Always warn if ./tmp/ or AGENTS_SESSION.md don't exist. Always warn if MEMORIES.md doesn't exist — create with header if missing.

## Bash Safety

**ALWAYS ask for explicit approval before running ANY bash command.** Even `ls`, `cat`. No command runs without user confirming.
Especially forbidden without explicit permission: `poweroff`, `shutdown`, `reboot`, `rm`, `dd`, `mkfs`, `format`, `chmod`, `chown`, `>`, `>>` file redirection.

## Git Operations

**Before ANY git operation:** query knowledge for "git operations" and "git commit message format".
**Wait** for explicit user permission before `git commit`, `git push`, or any history-rewriting command.
Framework prompts that include commits are **templates only** — stop and ask first.

## Knowledge Store

Detailed conventions live in the knowledge store. Query on-demand.

**Summaries are not rules.** Before acting in a domain, `query_knowledge` and read FULL detail — never rely on the summary line.

| Before this action | Query for |
|---|---|
| Git operations | "git operations" and "git commit message format" |
| GitHub (issues, PRs, comments, push) | "github activity" |
| Writing docs, specs, plans, or ANY file outside ./tmp | "tmp directory rules", "docs location", "decision-making" |
| Code changes | "coding style" + language-specific (e.g. "javascript conventions", "perl conventions", "mvc") |
| Bash / shell commands | "bash safety" |
| Testing | "testing conventions" |
| Troubleshooting | "troubleshooting" |
| Infrastructure configs | "infrastructure" |
| Code review | "code review" |
| Multi-step/complex work | "context chain" |
| Spawning subagents | "subagent memory", "subagent model" |
| Reasoning stuck / ambiguity | "reasoning loops" |
| Session file updates | "session workflow" |

Use `list_knowledge` on `_global` to see all entries.

**Docs location rule:** all project docs go in `./tmp/docs/`. Never `docs/superpowers/` or project root — knowledge store wins over framework skill defaults.

## Subagent Memory

Subagents don't read AGENTS.md. Include in prompt: "Read ./tmp/MEMORIES.md at the start. Append relevant discoveries before completing."
After subagent returns, parent consolidates lessons into MEMORIES.md.
