---
name: guardrails-onboarding
version: 2.0.0
description: "Create and explore local Konvu Guardrails baselines. Use when a user wants to scan a codebase, inspect its Assets, Controls, or Implementations, compare stored runs, or navigate a baseline in the terminal."
metadata:
  requires:
    bins: ["konvu"]
---
# Guardrails baselines

Konvu Guardrails models the security-relevant Assets in a codebase, the
Controls that apply to them, and the concrete Implementations it found.

## Scan a codebase

```bash
konvu guardrails baseline scan <codebase>
```

The codebase may be any local path; it does not need to be the current working
directory. The deterministic indexing phase prints an estimate before the
model-backed work starts. Let the user review that estimate, or use `--yes`
only when they asked for a non-interactive run.

The model-backed stages require `OPENAI_API_KEY` or `--openai-api-key`. Prefer
the environment variable so the key is not stored in shell history.

Each attempt creates an immutable run under
`~/.konvu/guardrails/baselines/<run-id>/` containing only:

- `baseline.json` — the complete queryable baseline
- `run.log` — stage, retry, usage, cost, and error details

Failed and cancelled attempts remain available for diagnostics.

## Find a run

```bash
konvu guardrails baseline list
konvu guardrails baseline list --repo <name-or-absolute-path>
```

Run queries are independent of the current directory. Use `--run <run-id>` for
an exact historical run. `--repo` selects the latest completed run for an
unambiguous repository name or exact stored path. With neither selector, a
data query succeeds only when exactly one completed run exists.

## Explore baseline data

```bash
konvu guardrails baseline list assets --run <run-id>
konvu guardrails baseline list controls --run <run-id>
konvu guardrails baseline list implementations --run <run-id>
konvu guardrails baseline show <run-id>
konvu guardrails baseline show <record-id> --run <run-id>
konvu guardrails baseline explain <record-id> --run <run-id>
```

Other listable sections include classes, routes, resources, roles, Control
observations, and unresolved observations. Use `--output json` for scripts.
`show <run-id> --output json` returns the exact `baseline.json`. Use `--log` to
read a run's execution log.

`show` returns one stored record. `explain` adds its direct relationships—for
example an Asset's Controls and Implementations, or every Asset using a
Control.

## Use the terminal explorer

```bash
konvu guardrails baseline tui
konvu guardrails baseline tui --run <run-id>
```

The first screen lists historical runs with repository, commit, scan time,
duration, counts, and status. Enter opens a completed run. Failed, cancelled,
running, and invalid runs open diagnostics. Escape returns to the run list.

If no run exists, report the scan command printed by the CLI. Do not look for
or infer data from repository-local files or older Guardrails artifacts.
