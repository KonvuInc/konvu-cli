# Konvu CLI

Command-line interface for [Konvu](https://konvu.com) — manage security vulnerabilities from your terminal.

The Konvu CLI connects to the Konvu API to let you browse, triage, and act on SCA findings without leaving your workflow. It's designed for security engineers who need to answer questions like:

- What are my exploitable critical findings right now?
- Which repositories have the most open vulnerabilities?
- Are we making progress — how does this week compare to last week?
- What's the evidence behind this assessment?
- Do I agree with this recommendation?

## Installation

### Homebrew (macOS / Linux)

```bash
brew install KonvuTeam/tap/konvu
```

### Shell script

```bash
curl -sSL https://raw.githubusercontent.com/KonvuTeam/konvu-cli/main/scripts/install.sh | sh
```

Set a custom install directory with `KONVU_INSTALL_DIR`:

```bash
curl -sSL https://raw.githubusercontent.com/KonvuTeam/konvu-cli/main/scripts/install.sh | KONVU_INSTALL_DIR=~/.local/bin sh
```

### Manual download

Download the binary for your platform from [GitHub Releases](https://github.com/KonvuTeam/konvu-cli/releases).

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

Konvu CLI supports two authentication methods:

1. **Browser login (OAuth)** — interactive, opens your browser
2. **API key** — non-interactive, ideal for CI/CD and scripts

```bash
konvu login                      # interactive picker (OAuth or API key)
konvu login --api-key            # prompt for API key (masked input)
konvu login --api-key api_...    # pass API key directly
konvu whoami                     # check current user and company
konvu logout                     # clear stored credentials
```

Create an API key at: https://app.konvu.com/configuration/api_keys

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

# Filter by scanner source
konvu finding list --source snyk --assessment exploitable

# Pipe IDs to other commands
konvu finding list --assessment exploitable -q | xargs -I {} konvu finding get {}
```

**Filters:** `--severity`, `--assessment`, `--state`, `--has-fix`, `--repo`, `--cve`, `--dependency`, `--source`, `--source-id`, `--since`, `--until`

**Grouping:** `--group-by repository|dependency|severity|assessment`

**Output:** `--output json|table|csv`, `--quiet` (IDs only), `--count` (total only), `--fields` (select fields)

### `konvu finding get` — Inspect a finding

Get full details on a finding, structured into three sections: **Assessment** (Konvu's analysis), **Finding** (this specific instance), and **Vulnerability** (CVE details).

```bash
# Basic detail
konvu finding get <id>

# Verbose — full evidence for each checklist item
konvu finding get <id> --verbose

# With exploitability evidence (checklist, proofs, reachability)
konvu finding get <id> --include evidence

# With recommendation decision log
konvu finding get <id> --include logs

# Both, as JSON
konvu finding get <id> --include evidence --include logs --output json
```

### `konvu finding rate` — Rate an assessment

Provide feedback on Konvu's assessment to improve future recommendations.

```bash
# Agree with the assessment
konvu finding rate <id> agree

# Disagree with a comment
konvu finding rate <id> disagree --comment "Only used in test environment"

# Skip the extra API call if you already have the recommendation ID
konvu finding rate <id> agree --recommendation-id <rec-id>
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

Shows vulnerability details and Konvu's assessment across your repositories, with color-coded exploitability status.

```bash
konvu vuln CVE-2021-44228
konvu vuln GHSA-xxxx-yyyy --include remediation --output json
```

### `konvu metrics` — Security posture

Dashboard-style overview with summary, trends, top CVEs, and new vs closed counts.

```bash
konvu metrics
konvu metrics --include top_cves,new_vs_closed
konvu metrics --since 90d --interval month --output json
```

### `konvu dismiss` — Bulk dismiss

```bash
# Preview what would be dismissed
konvu dismiss --assessment false-positive --severity low --dry-run

# Dismiss with reason
konvu dismiss --assessment false-positive --severity low --reason "Accepted risk"
```

### `konvu skills path` — Locate bundled skills

Returns the path to the bundled Claude Code skills directory. Useful for configuring your AI assistant.

```bash
konvu skills path
```

### `konvu version` — Show version

```bash
konvu version
```

### `konvu --help-all` — Full CLI reference

Prints a complete reference of all commands, flags, and examples in a single view.

```bash
konvu --help-all
```

## Claude Code integration

Konvu CLI ships with bundled [Claude Code](https://docs.anthropic.com/en/docs/claude-code) skills that teach AI agents how to use the CLI effectively. To make them available, add the skills path to your `.claude/settings.json`:

```json
{
  "permissions": {
    "additionalDirectories": ["<output of konvu skills path>"]
  }
}
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
| `KONVU_ACCESS_TOKEN` | — | API key or access token (alternative to `konvu login`) |
| `KONVU_API_URL` | `https://api.konvu.com` | API base URL |
| `KONVU_ZITADEL_DOMAIN` | `https://auth.konvu.com` | OAuth provider |
| `KONVU_ZITADEL_CLIENT_ID` | — | OAuth client ID (required for OAuth login) |

## Development

```bash
git clone git@github.com:KonvuTeam/konvu-cli.git
cd konvu-cli
go build -o konvu .
go test ./...
go vet ./...
```
