# Konvu CLI

Command-line interface for [Konvu](https://konvu.com) — manage security vulnerabilities from your terminal.

The Konvu CLI connects to the Konvu API to let you browse, triage, and act on SCA findings without leaving your workflow. It's designed for security engineers who need to answer questions like:

- What are my exploitable critical findings right now?
- Which repositories have the most open vulnerabilities?
- Are we making progress — how does this week compare to last week?
- What's the evidence behind this assessment?
- Do I agree with this recommendation?

## Installation

### Build from source

Requires [Go 1.25+](https://go.dev/dl/) and git access to the repository.

```bash
go install github.com/KonvuInc/konvu-cli/cmd/konvu@latest
```

This places the `konvu` binary in `$GOBIN` (defaults to `~/go/bin`).

Verify it works:

```bash
konvu version
```

> **Note:** Make sure `$GOBIN` is in your `$PATH`.
> For example, add `export PATH="$(go env GOPATH)/bin:$PATH"` to your shell profile.

### Shell script (requires public repo access)

```bash
curl -sSL https://raw.githubusercontent.com/KonvuInc/konvu-cli/main/scripts/install.sh | sh
```

Set a custom install directory with `KONVU_INSTALL_DIR`:

```bash
curl -sSL https://raw.githubusercontent.com/KonvuInc/konvu-cli/main/scripts/install.sh | KONVU_INSTALL_DIR=~/.local/bin sh
```

### Manual download

Download the binary for your platform from [GitHub Releases](https://github.com/KonvuInc/konvu-cli/releases) (requires repository access).

### Docker

```bash
docker run --rm -v konvu-config:/root/.config/konvu ghcr.io/konvuinc/konvu-cli <command>
```

The `konvu-config` volume persists credentials between runs.

```bash
# Interactive login (OAuth — requires -it for TTY, skills prompt installs to host)
docker run --rm -it \
  -v konvu-config:/root/.config/konvu \
  -v ~/.claude/skills:/root/.claude/skills \
  ghcr.io/konvuinc/konvu-cli login

# Non-interactive login with API key
docker run --rm -v konvu-config:/root/.config/konvu ghcr.io/konvuinc/konvu-cli login --api-key api_...

# Subsequent commands reuse the credentials
docker run --rm -v konvu-config:/root/.config/konvu ghcr.io/konvuinc/konvu-cli finding list

# Install skills to your host so Claude Code can find them
docker run --rm -v ~/.claude/skills:/root/.claude/skills ghcr.io/konvuinc/konvu-cli skills install
```

Or pass an API key via environment variable to skip login entirely:

```bash
docker run --rm -e KONVU_ACCESS_TOKEN=api_... ghcr.io/konvuinc/konvu-cli finding list
```

### Homebrew (macOS / Linux) — Coming soon

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

### Guardrails baseline scan

Create a normalized security baseline using public OpenAI. The deterministic
index runs first, then the CLI prints the estimated cost and duration and asks
before starting the model-backed stages:

```bash
export OPENAI_API_KEY=sk-...
konvu guardrails baseline scan path/to/repo
```

The scan always uses `gpt-5.6-luna`. Use `--yes` for non-interactive runs. The
API key is passed only to the Guardrails process and is not stored by Konvu
CLI. Prefer the environment variable so the key is not recorded in shell
history. The downloaded Guardrails runtime is pinned to exact hashes embedded
in this public CLI; cached executables are verified before every run. The child
process receives only the credentials and runtime environment it needs, not
unrelated ambient cloud or developer credentials. It also runs in an OS
filesystem sandbox: the repository is read-only, with writes limited to
the global baseline store and a private temporary directory. Linux requires
`bubblewrap`; `--no-sandbox` explicitly disables isolation. Every attempt gets
an immutable directory under `~/.konvu/guardrails/baselines/<run-id>/` with
exactly `baseline.json` and `run.log`.

### Explore Guardrails baselines

List historical runs and query their Assets, Controls, and Implementations
without rerunning a scan:

```bash
konvu guardrails baseline list
konvu guardrails baseline list assets --run <run-id>
konvu guardrails baseline show <record-id> --run <run-id>
konvu guardrails baseline explain <record-id> --run <run-id>
konvu guardrails baseline tui
```

Queries work from any directory. Select an exact historical run with `--run`,
or use `--repo` to select the latest completed run for a stored repository.
The TUI starts with a runs table; selecting a completed run opens its Asset and
Control workspace, while failed or cancelled runs open diagnostics. Use
`--output json` for lossless scripted access to `baseline.json`.

### Finding sources

Findings come from four scanner categories, each with its own subcommand:

- `konvu finding sca <op>` — dependency (SCA) findings — the historical default
- `konvu finding sast <op>` — application-code (SAST) findings from Semgrep, Arnica, etc.
- `konvu finding container <op>` — container image findings from AWS Inspector and other scanners
- `konvu finding secrets <op>` — leaked-credential findings from repository secret scanning

Common ops are `list`, `get`, and `counts`. `sca`, `sast`, and `secrets` also support `rate`. `sca` alone supports `submit`.

**Backward compatibility**: bare forms (`konvu finding list`, `konvu finding get`, `konvu finding rate`, `konvu finding counts`, `konvu finding submit`) continue to work exactly as before — they're aliases for the `sca` equivalents. Existing scripts, skills, and pipes do not need to change.

**SAST identity note**: `konvu finding sast list` emits the *investigation ID* as the row's `id` field. Both `get` and `rate` expect that investigation ID (not the raw scanner detection ID). The raw detection ID is available as `detection_id` in the list rows. Detections without a Konvu investigation are included in the output with an empty `id` and `triage_status: "pending"`; `konvu finding sast list -q` skips them so `xargs`-style pipes stay safe. To restrict output to triaged rows only, filter with `jq '.[] | select(.id != "")'`.

**Secrets bulk rating**: `konvu finding secrets rate <id> <assessment>` handles single findings. For batches, pipe IDs into `--stdin`:

```bash
konvu finding secrets list --assessment unknown -q \
  | konvu finding secrets rate --stdin applicable
```

The bulk endpoint applies to whole `(provider, secret_hash)` groups, so a rating on one location propagates to every finding of the same secret.

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

**Filters:** `--severity`, `--assessment`, `--state`, `--has-fix`, `--repo`, `--cve`, `--dependency`, `--source`, `--dependabot-alert`, `--since`, `--until`

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

### `konvu finding submit` — Ingest findings from another scanner

Push SCA findings you already have (e.g. a Snyk or Dependabot export) into Konvu
for triage. Reads a JSON array of findings from `--file` (or stdin with `-`) and
submits them against `--repo`. Re-submitting a finding updates it rather than
duplicating; on a Konvu-connected, scanned repo the findings flow into AI triage
automatically.

```bash
# Submit an export against a repo's default branch
konvu finding submit --repo github:acme/web --file snyk-findings.json

# Target a specific branch or tag
konvu finding submit --repo github:acme/web --ref release-2.3 --file findings.json

# Pipe findings in and preview without submitting
cat findings.json | konvu finding submit --repo github:acme/web --file - --dry-run
```

Each finding object accepts `vulnerability_id`, `manifest_location`, and
`dependency_name` (required), plus optional `dependency_version`,
`dependency_ecosystem`, `source`, `state`, and `transitivity`. `source` is kept
verbatim: it comes back as the finding's `scanner` and filters with
`konvu finding list --source <value>`. It is replaced on every update, so a
changed value renames the scanner and an omitted one clears the label — send it
on every submission. Every item is processed independently and reported back as
created / updated / accepted_unmapped / rejected (with a reason); a submission
where every item is rejected exits `1`.

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

### `konvu remediate` — Trigger an auto-fix PR

Asks Konvu to open a remediation pull request for a finding. The job runs
asynchronously inside the on-prem controller (patcheus engine); poll status
with `--wait` or `konvu remediate status`.

Works with both **GitHub** (Konvu Autofix GitHub App) and **GitLab** (Konvu
remediation integration) — the CLI detects which SCM your finding lives in
from the repository URL and points you at the right install flow if a
required integration is missing.

```bash
# Trigger remediation (top-level alias for `konvu remediate run`)
konvu remediate <finding-id>

# Trigger and wait until the job reaches a terminal status
konvu remediate <finding-id> --wait --timeout 15m

# Include a ticket/source URL in the PR description
konvu remediate <finding-id> --source-url https://linear.app/konvu/issue/SEC-42

# Check status without triggering (returns null if no job has run)
konvu remediate status <finding-id>

# Backward-compatible alias
konvu autofix <finding-id>
```

Terminal job statuses: `succeeded`, `failed`, `merged`, `closed`. Non-terminal:
`pending`, `running`.

### `konvu remediate list` — See the plans waiting for you

Lists the remediation plans in your backlog — the same plans the dashboard's
remediation board shows — without needing a plan id. SCA and SAST plans come
back together; use `--kind` to see just one.

```bash
# All plans waiting (SCA + SAST), table when interactive / json when piped
konvu remediate list

# Just SCA (or SAST) plans
konvu remediate list --kind sca
konvu remediate list --kind sast

# Just one repository (slug, full URL, or repo id) — filtered server-side
konvu remediate list --repo github:org/repo

# Only the actionable ones
konvu remediate list --status ready

# Bare plan ids, ready to pipe into `brief`
konvu remediate list --kind sca -q | xargs -n1 konvu remediate brief
```

Flags: `--kind sca|sast|all` (default `all`), `--status` (e.g. `ready`),
`--repo` (one repository: `github:org/repo`, full URL, or repo id),
`--grouping` (`recommended|by_dependency|most_cve_cleared|most_at_risk`),
`--repo-scope` (`tier_1_2|all`), `--limit` (1–50, per kind), `--output/-o`,
`--quiet/-q`.

### `konvu remediate brief` — Brief your own coding agent

Fetches one or more remediation plans and prints a ready-to-use agent prompt:
the packages to upgrade, where they live (repository, branch, manifest), the
findings each bump resolves, and Konvu's assessment of why they matter.

Plan ids come from the Konvu dashboard's remediation board — the
**Copy CLI command** actions put the full command on your clipboard.

```bash
# Print the agent prompt for a plan
konvu remediate brief <plan-id>

# Several plans at once (prompts separated by `---`)
konvu remediate brief <plan-id-1> <plan-id-2>

# Pipe straight into a coding agent
konvu remediate brief <plan-id> | claude -p

# Full structured payload instead of the prompt
konvu remediate brief <plan-id> --output json
```

### `konvu coverage` — Configure where AI Assessment runs

Control which repositories the agents assess and at which severities. Repositories are identified by URL, id, or a unique URL substring.

```bash
# List repositories and their coverage (bare `konvu coverage` also lists)
konvu coverage list

# Enable AI Assessment on a repository (starts on the company default severities)
konvu coverage enable github:org/repo

# Enable scoped to specific severities
konvu coverage enable org/repo --severities CRITICAL,HIGH

# Disable AI Assessment
konvu coverage disable org/repo

# Change the severities assessed for a repository
konvu coverage severities org/repo --set CRITICAL,HIGH
konvu coverage severities org/repo --all          # assess every severity

# Show or set the company-wide default new repositories inherit
konvu coverage default
konvu coverage default --set CRITICAL,HIGH
konvu coverage default --all
```

Severities are `CRITICAL`, `HIGH`, `MEDIUM` (alias for `MODERATE`), `LOW`. `--all` clears the restriction (assess every severity); an empty set is rejected. Use `--dry-run` on `enable`/`disable`/`severities` to preview.

### `konvu inventory` — Explore repositories and their threat profiles

Explore your repositories through Konvu's threat profiles: the production-vs-noise classification, a composite 0–100 threat score and named tier (crown jewel / key asset / standard / peripheral), a one-line summary, and an evidence-bearing attribute map (internet exposure, customer data, cloud credentials, and more). Read-only. Aliased as `konvu inv`. Repositories are identified by URL, id, or a unique URL substring.

```bash
# Org-wide overview: repos profiled, headline attribute counts, tier distribution,
# and the highest-scoring repositories
konvu inventory

# Full threat profile for a single repository — score, tier, classification, and
# every stored attribute with its provenance, confidence, and evidence
konvu inventory show github:org/repo

# Machine-readable output for scripting
konvu inventory -o json
konvu inventory show org/repo -o json

# Bare `repo_id<TAB>tier` for the ranked repos, for piping
konvu inventory -q                       # 0190a1b2-...  crown_jewel
konvu inventory -q | cut -f1 | xargs -n1 konvu inventory show

# Select only the fields you need (top-level keys)
konvu inventory show org/repo --fields threat_profile_score,threat_profile_tier,internet_exposed -o json
```

`show` exits 3 (not found) when a repository has no threat profile yet — Konvu builds them as it analyzes your repositories. The `threat_profile_tier` field is a stable slug (`crown_jewel`, `key_asset`, `standard`, `peripheral`); `threat_profile_tier_label` carries the human-readable version.

### `konvu skills path` — Locate bundled skills

Returns the path to the bundled Claude Code skills directory. Useful for configuring your AI assistant.

```bash
konvu skills path
```

### `konvu version` — Show version

```bash
konvu version
```

### `konvu update` — Update to the latest release

Downloads the latest release for your platform, verifies its checksum, and replaces
the running binary in place.

```bash
# Check whether a newer version is available (no install)
konvu update --check

# Install the latest release
konvu update

# Reinstall even if already up to date (or Homebrew-managed)
konvu update --force
```

If konvu was installed via Homebrew, `update` defers to `brew upgrade konvu` unless
you pass `--force`.

### `konvu --help-all` — Full CLI reference

Prints a complete reference of all commands, flags, and examples in a single view.

```bash
konvu --help-all
```

## Updating

The quickest way to update a released binary is:

```bash
konvu update
```

If you installed via `go install` or Homebrew, use the matching flow instead:

```bash
# go install
go install github.com/KonvuInc/konvu-cli/cmd/konvu@latest

# Homebrew
brew upgrade konvu
```

After updating, refresh the bundled skills:

```bash
konvu skills install
```

The CLI will warn you if bundled skills are newer than what's installed.

## Claude Code integration

Konvu CLI ships with bundled [Claude Code](https://docs.anthropic.com/en/docs/claude-code) skills. After `konvu login`, you'll be prompted to install them. You can also install them manually anytime:

```bash
konvu skills install
```

Skills are installed to `~/.claude/skills/` where Claude Code discovers them automatically — no configuration needed.

To check the skills directory:

```bash
konvu skills path
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
| `KONVU_APP_URL` | `https://app.konvu.com` | Dashboard URL (used in `konvu remediate` install-link suggestions) |
| `KONVU_ZITADEL_DOMAIN` | `https://auth.konvu.com` | OAuth provider |
| `KONVU_ZITADEL_CLIENT_ID` | — | OAuth client ID (required for OAuth login) |

## Development

```bash
go install ./cmd/konvu
go test ./...
go vet ./...
```
