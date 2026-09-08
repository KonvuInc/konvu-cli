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

Each attempt creates an immutable run under
`~/.konvu/guardrails/baselines/<run-id>/` containing only:

- `baseline.json` — the complete queryable Security Context Graph
- `run.log` — stage, retry, usage, cost, and error details

Failed and cancelled attempts remain available for diagnostics.

## Find a run

```bash
konvu inventory map history
konvu inventory map history <name-or-absolute-path>
```

Run queries are independent of the current directory. Use `--run <run-id>` for
an exact historical run. `--repo` selects the latest completed run for an
unambiguous repository name or exact stored path. With neither selector, a
data query succeeds only when exactly one completed run exists. `map history
--repo` returns every stored run for that unambiguous repository, including
failed and cancelled runs.

## Explore graph data

```bash
konvu inventory map records list --run <run-id> --collection assets
konvu inventory map records list --run <run-id> --collection controls
konvu inventory map records list --run <run-id> --collection implementations
konvu inventory map show <run-id>
konvu inventory map records get <record-id> --run <run-id>
konvu inventory map records get <record-id> --collection <collection> --run <run-id>
konvu inventory map records explain <record-id> --run <run-id>
```

Other listable sections include Asset observations, classes, routes, resources,
roles, Control observations, and unresolved observations. Use `--collection`
with `records get` or `records explain` to address an exact section when IDs overlap. Use
`--output json` for scripts.
`show <run-id> --output json` returns the exact `baseline.json`. Use
`--include log` to read a run's execution log.

`records get` returns one stored record. `records explain` adds its direct relationships—for
example an Asset's Controls and Implementations, or every Asset using a
Control.

## Use the terminal explorer

```bash
konvu inventory map history
konvu inventory map history --run <run-id>
```

The first screen lists historical runs with repository, commit, mapped time,
duration, counts, and status. Enter opens a completed run. Failed, cancelled,
running, and invalid runs open diagnostics. Escape returns to the run list.

If no run exists, report the map command printed by the CLI. Do not look for
or infer data from repository-local files or other artifacts.
