---
name: guardrails-onboarding
version: 2.0.0
description: "Create and explore local Konvu Security Context Graphs. Use when a user wants to map a codebase, inspect its Assets, Controls, or Implementations, compare stored runs, or navigate graph history in the terminal."
metadata:
  requires:
    bins: ["konvu"]
---
# Security Context Graphs

Konvu Inventory maps the security-relevant Assets in a codebase, the
Controls that apply to them, and the concrete Implementations it found.

## Map a codebase

```bash
konvu inventory map <codebase>
```

The codebase may be any local path; it does not need to be the current working
directory. The deterministic indexing phase prints an estimate before the
model-backed work starts. Let the user review that estimate, or use `--yes`
only when they asked for a non-interactive run.

The model-backed stages require `OPENAI_API_KEY` or `--openai-api-key`. Prefer
the environment variable so the key is not stored in shell history.

Each attempt is stored outside the repository. Completed maps and failed or
cancelled attempts remain available from any working directory.

## Find a run

```bash
konvu inventory map history
konvu inventory map history <name-or-absolute-path>
```

History is independent of the current directory. Use `--run <run-id>` for an
exact historical run. `--repo` selects every stored run for an unambiguous
repository name or exact stored path, including failed and cancelled runs.

## Inspect and compare maps

```bash
konvu inventory map show <run-id>
konvu inventory map show <run-id> --output json
konvu inventory map diff <base-run> <head-run>
```

`show` prints a static summary by default and exports the complete graph with
`--output json`. Use `--include log` to inspect a run's execution log. `diff`
compares all graph sections by default; use `--collection` to limit it to one.

## Use the terminal explorer

```bash
konvu inventory map history
konvu inventory map history --run <run-id>
```

The first screen lists historical runs with repository, commit, mapped time,
counts, and status. Enter or Right opens a completed run. Failed, cancelled,
running, and invalid runs open diagnostics. Left or Escape returns to the run
list; Q quits.

If no run exists, report the map command printed by the CLI. Do not look for
or infer data from repository-local files or other artifacts.
