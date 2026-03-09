# Konvu CLI

Command-line interface for [Konvu](https://konvu.com) — manage security vulnerabilities from your terminal.

The Konvu CLI connects to the Konvu API to let you browse, triage, and act on SCA findings without leaving your workflow. It's designed for security engineers who need to answer questions like:

- What are my exploitable critical findings right now?
- Which repositories have the most open vulnerabilities?
- Are we making progress — how does this week compare to last week?
- What's the evidence behind this assessment?

## Installation

```bash
pip install konvu-cli
```

Requires Python 3.11+.

## Quick start

```bash
# Authenticate (opens browser for OAuth)
konvu login

# See your assessment summary
konvu finding counts

# List exploitable findings
konvu finding list --assessment exploitable

# Deep dive into a specific finding
konvu finding get <finding-id> --include evidence
```

## Authentication

Konvu CLI uses OAuth Device Flow. Run `konvu login` and follow the browser prompt. Credentials are stored locally and refreshed automatically.

```bash
konvu login          # authenticate
konvu whoami         # check current user and company
konvu logout         # clear stored credentials
```

## Commands

### `konvu finding list` — Browse findings

List and filter findings across your repositories.

```bash
# This week's exploitable findings
konvu finding list --since 7d --assessment exploitable

# Critical findings with available fixes
konvu finding list --severity critical --has-fix fixed

# Group by repository to see where to focus
konvu finding list --assessment exploitable --group-by repository

# Group by dependency to find the most impactful library to fix
konvu finding list --assessment exploitable --group-by dependency

# Just the count
konvu finding list --assessment not-assessed --count

# Pipe IDs to other commands
konvu finding list --assessment exploitable -q | xargs -I {} konvu finding get {}
```

**Filters:** `--severity`, `--assessment`, `--state`, `--has-fix`, `--repo`, `--cve`, `--dependency`, `--since`, `--until`

**Grouping:** `--group-by repository|dependency|severity|assessment`

**Output:** `--output json|table|csv`, `--quiet` (IDs only), `--count` (total only), `--fields` (select fields)

### `konvu finding get` — Inspect a finding

Get full details on a finding, including AI qualification evidence and recommendation history.

```bash
# Basic detail
konvu finding get <id>

# With exploitability evidence (checklist, proofs, reachability)
konvu finding get <id> --include evidence

# With recommendation decision log
konvu finding get <id> --include logs

# Both, as JSON
konvu finding get <id> --include evidence --include logs --output json
```

### `konvu finding counts` — Assessment metrics

Get accurate counts across your entire dataset, with optional breakdowns.

```bash
# Overall assessment summary
konvu finding counts

# By severity
konvu finding counts --group-by severity

# Weekly trends (last 4 weeks)
konvu finding counts --group-by week

# Monthly trends (last 3 months, or custom range)
konvu finding counts --group-by month --since 180d

# Scoped to a repo
konvu finding counts --repo github:org/repo --group-by severity
```

### `konvu vuln` — Look up a vulnerability

```bash
konvu vuln CVE-2021-44228
konvu vuln GHSA-xxxx-yyyy --output json
```

### `konvu metrics` — Security posture

```bash
konvu metrics --since 30d
konvu metrics --since 90d --interval month --output json
```

### `konvu dismiss` — Bulk dismiss

```bash
# Preview what would be dismissed
konvu dismiss --assessment false-positive --severity low --dry-run

# Dismiss with reason
konvu dismiss --assessment false-positive --severity low --reason "Accepted risk"
```

## Output formats

The CLI auto-detects output format: **table** when running interactively, **JSON** when piped. Override with `--output`:

```bash
konvu finding list --output json    # machine-readable
konvu finding list --output table   # human-readable
konvu finding list --output csv     # spreadsheet-friendly
konvu finding list --quiet          # bare IDs, one per line
```

## Assessment model

Konvu classifies findings into four assessment categories:

| Assessment | Meaning |
|---|---|
| **exploitable** | Confirmed exploitable in your context — fix this |
| **false-positive** | Not exploitable — safe to dismiss |
| **inconclusive** | Needs more information |
| **not-assessed** | Not yet analyzed |

Filter by assessment: `--assessment exploitable`, combine with severity: `--severity critical`.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | Resource not found |
| 4 | Authentication failed |

## Configuration

| Environment variable | Default | Description |
|---|---|---|
| `KONVU_API_URL` | `https://api.konvu.com` | API base URL |
| `KONVU_ZITADEL_DOMAIN` | `https://auth.konvu.com` | OAuth provider |
| `KONVU_ZITADEL_CLIENT_ID` | — | OAuth client ID (required for login) |

## Development

```bash
git clone git@github.com:KonvuTeam/konvu-cli.git
cd konvu-cli
pip install -e ".[dev]"
pytest
```
