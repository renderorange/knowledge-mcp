# Project Knowledge

This project uses the knowledge layer for persistent agent memory.

## Before starting work

Query knowledge for current context:
- `query_knowledge(project="<name>", category="conventions")` — patterns, build, naming
- `query_knowledge(project="<name>", query="<subsystem>")` — how it works
- `query_knowledge(project="<name>", category="decisions")` — past choices
- `query_knowledge(project="<name>", category="workflows")` — procedures to run

## After learning something significant

Write it back:
- `write_knowledge(project="<name>", category="subsystems", summary="...", detail="...", source="...")`
- `write_knowledge(project="<name>", category="decisions", summary="...", detail="...", source="...")`
- `write_knowledge(project="<name>", category="workflows", summary="...", detail="...", source="...")`
