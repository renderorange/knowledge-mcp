# Project Knowledge

This project uses the knowledge layer for persistent agent memory.

## Before starting work

Query knowledge for current context:
- `query_knowledge(project="<name>", category="conventions")` — patterns, build, naming
- `query_knowledge(project="<name>", query="<subsystem>")` — how it works
- `query_knowledge(project="<name>", category="decisions")` — past choices

## After learning something significant

Write it back:
- `write_knowledge(project="<name>", category="subsystems", summary="...", detail="...", confidence="...", source="...")`
- `write_knowledge(project="<name>", category="decisions", summary="...", detail="...", confidence="...", source="...")`
