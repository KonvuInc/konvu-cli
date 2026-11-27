# Konvu CLI

Give agents a security model of your codebase.

Inventory turns a repository into a source-linked Security Context Graph: its sensitive
endpoints, objects, and fields; the roles associated with them; and the
controls that protect them. Build this agent-ready context without creating a
Konvu account. Connect an account later to browse findings, track posture, or
trigger remediation from the CLI.

| I want to… | Konvu account | Credential | Start here |
|---|---:|---|---|
| Give agents a security model of my repository | Not required | Your OpenAI API key (usage billed by OpenAI) | [Build a security map](#build-an-agent-ready-security-map-no-konvu-account) |
| Work with findings managed by Konvu | Required | Konvu login or API key | [Connect to Konvu](#connect-to-konvu-optional) |

## Installation

Choose any installation method below. Installing the CLI does not create or
require a Konvu account.

### Install with Go

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

### Shell script

The installer downloads the matching GitHub release and verifies its checksum.

```bash
curl -sSL https://raw.githubusercontent.com/KonvuInc/konvu-cli/main/scripts/install.sh | sh
```

Set a custom install directory with `KONVU_INSTALL_DIR`:

```bash
curl -sSL https://raw.githubusercontent.com/KonvuInc/konvu-cli/main/scripts/install.sh | KONVU_INSTALL_DIR=~/.local/bin sh
```

### Manual download

Download the binary for your platform from [GitHub Releases](https://github.com/KonvuInc/konvu-cli/releases).

### Docker

Run account-free mapping with your repository mounted read-only. The two named
volumes preserve the downloaded mapping runtime and completed graph runs. The
container boundary and read-only repository mount provide the
isolation here, so the command disables the nested OS sandbox:

```bash
docker run --rm -it \
  -e OPENAI_API_KEY \
  -v "$PWD:/repo:ro" \
  -v guardrails-config:/root/.config/guardrails \
  -v guardrails-store:/root/.konvu/guardrails \
  ghcr.io/konvuinc/konvu-cli inventory map /repo --no-sandbox
```

Review the stored result without rerunning the map:

```bash
docker run --rm -it \
  -v guardrails-store:/root/.konvu/guardrails \
  ghcr.io/konvuinc/konvu-cli inventory map history
```

For account-backed commands, persist Konvu credentials in a separate volume:

```bash
docker run --rm -it \
  -v konvu-config:/root/.config/konvu \
  ghcr.io/konvuinc/konvu-cli login
```

### Homebrew (macOS / Linux) — Coming soon

## Build an agent-ready security map (no Konvu account)

Before asking an agent to review or modify security-sensitive code, give it a
reliable representation of what the application protects. A Security Context
Graph captures:

- sensitive endpoints, objects, and fields;
- the roles and actors associated with them;
- the controls intended to protect each asset;
- detected implementations and supporting source evidence; and
- protections that are present, partial, or absent.

The result is structured security context that agents can use to review
changes, investigate authorization boundaries, and preserve important
protections without reconstructing the application's security model from
scratch. Creating it does not require `konvu login` or a
`KONVU_ACCESS_TOKEN`.

**Coming soon:** the same graph will feed agent hooks and CI checks, applying
this security context automatically as agents edit code and changes move
through the delivery pipeline.

You need an OpenAI API key and a supported machine: macOS or Linux on arm64 or
amd64 (glibc Linux). Linux also requires `bubblewrap` for the default filesystem
sandbox. Windows is not currently supported.

```bash
export OPENAI_API_KEY=sk-...
konvu inventory map path/to/repo
```

The command deliberately separates inspection from model-backed work:

1. It downloads and verifies the pinned mapping runtime on first use.
2. It builds a deterministic index, then prints the estimated OpenAI cost and
   duration for the remaining stages.
3. It asks whether to continue, with **No** as the default. No model-backed
   stage runs if you decline.
4. If you continue, it uses `gpt-5.6-luna` inside an OS filesystem sandbox that
   keeps the repository read-only.
5. It stores the attempt outside the repository so completed maps and failed-run
   diagnostics remain available from any working directory.

Once the map completes, list historical runs or open the terminal explorer:

```bash
konvu inventory map history
konvu inventory map history <repository>
```

Use a run ID to print a summary, export the complete graph, or compare two
completed maps without rerunning either one:

```bash
konvu inventory map show <run-id>
konvu inventory map show <run-id> --output json
konvu inventory map diff <base-run> <head-run>
```

In an interactive terminal, `map history` opens the graph browser. Select a run
with Up/Down and press Enter or Right to inspect its Assets and Controls. Press
Left or Escape to return, and Q to quit.

### Use the Security Context Graph as agent context

Export the complete structured result and give it to an agent alongside a code
review or implementation task:

```bash
konvu inventory map show <run-id> --output json \
  > ~/security-context-graph.json
```

The exported context may contain sensitive details about the repository. Store
and share it with the same care as the source code.

### Cost, privacy, and side effects

| Question | Answer |
|---|---|
| Do I need a Konvu account? | No. Local mapping, history, show, and diff do not authenticate with Konvu. |
| Can mapping cost money? | Yes. The model-backed stages use your OpenAI API account. Review the estimate and decline if you do not want to incur that cost. |
| Does repository data leave my machine? | The accepted model-backed stages send relevant repository content to your OpenAI API. Only map code you are authorized to share under your organization's OpenAI data policy. |
| Is my OpenAI key stored? | Not by Konvu CLI. It is passed to the mapping process for the run. Prefer `OPENAI_API_KEY` so the key does not enter shell history. |
| What is installed? | On first use, the CLI caches a pinned, verified mapping runtime in the user's config directory. |
| What is written to my repository? | Nothing by default. The sandbox keeps the repository read-only and stores each attempt in the user's local Konvu data directory. |

To print a static run summary, use `konvu inventory map show <run-id>`.
Add `--output json` for the complete structured graph; neither form reruns
the map.

## Connect to Konvu (optional)

The finding, vulnerability, metrics, coverage, remediation, and hosted Inventory
features connect to the Konvu API and require a Konvu account. Local Inventory
mapping and exploration do not. Authenticate with either a browser login or a
Konvu API key:

```bash
konvu login                      # interactive picker (OAuth or API key)
konvu login --api-key            # prompt for API key (masked input)
konvu login --api-key api_...    # pass API key directly
konvu whoami                     # check current user and company
konvu logout                     # clear stored credentials
```

Create a Konvu API key in the [Konvu dashboard](https://app.konvu.com/configuration/api_keys).

Then try the account-backed workflow:

```bash
# See your assessment summary
konvu finding counts

# List exploitable findings
konvu finding list --assessment exploitable

# Inspect the evidence for a finding
konvu finding get <finding-id> --include evidence
```

## Command reference

### Finding sources

The `finding` command groups scanner findings and submitted reports by source:
- `konvu finding sca <op>` — dependency (SCA) findings — the historical default
- `konvu finding sast <op>` — application-code (SAST) findings from Semgrep, Arnica, etc.
- `konvu finding container <op>` — container image findings from AWS Inspector and other scanners
- `konvu finding secrets <op>` — leaked-credential findings from repository secret scanning
- `konvu finding vulnerability-report <op>` — externally reported vulnerabilities: submit, inspect, steer, rate, dismiss, and reopen

Common ops are `list`, `get`, and `counts`. `sca` and `sast` also support
`assess`, `rate`, `dismiss`, and `reopen`; `secrets` supports `rate`; `sca`
alone supports `submit`.

Run any source's `list` command in an interactive terminal to open its findings browser. Use Up/Down to select, Enter or Right to inspect, Left or Escape to return, and Q to quit. The SCA table keeps the assessment summary and colors assessment results. Detail views are concise; request complete SCA evidence explicitly with `konvu finding get <id> --include evidence`.

Bare `konvu finding` opens the first 50 SCA findings in the same browser.

**SCA aliases**: bare forms (`konvu finding list`, `konvu finding get`, `konvu finding rate`, `konvu finding counts`, `konvu finding submit`) delegate to the `sca` equivalents. `finding list` now opens a browser on a TTY; pipes still receive JSON, and scripts can select `--output`, `--quiet`, `--count`, or `--group-by` explicitly.

**SAST identity note**: `konvu finding sast list` emits the *investigation ID*
as `id` and the stable Konvu finding ID as `detection_id`. `get` and `rate`
take `id`; `assess`, `dismiss`, and `reopen` take `detection_id`. Detections
without an investigation have an empty `id` and `triage_status: "pending"`;
`sast list -q` skips them so existing `get`/`rate` pipelines remain safe.

**Secrets bulk rating**: `konvu finding secrets rate <id> <assessment>` handles single findings. For batches, pipe IDs into `--stdin`:
```bash
konvu finding secrets list --assessment unknown -q \
  | konvu finding secrets rate --stdin applicable
```
The bulk endpoint applies to whole `(provider, secret_hash)` groups, so a rating on one location propagates to every finding of the same secret.

### `konvu finding list` — Browse findings

Open the interactive SCA findings browser on a TTY, or list and filter findings
when piped or given an explicit output mode.
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

### Request an assessment

```bash
konvu finding sca assess <finding-id>
konvu finding sast assess <detection-id>
```

These commands request one assessment and return the API response. SAST uses `detection_id` from `finding sast list`. Normal assessment eligibility and credit limits apply. Use `-o json` for the raw response; bare `finding assess` is an SCA alias.

### Dismiss or reopen a finding

```bash
konvu finding sca dismiss <finding-id> \
  --reason "Tracked externally" \
  --comment "Handled by AppSec" \
  --external-reference "SEC-1234"
konvu finding sca reopen <finding-id>

konvu finding sast dismiss <detection-id> \
  --reason "Accepted risk" \
  --comment "Reviewed by AppSec" \
  --external-reference "SEC-1234"
konvu finding sast reopen <detection-id>
```

`close` is an alias for `dismiss`. Use `--dry-run` to preview an action. SAST
dismissal metadata is available in JSON and CSV output, or as selected table
fields:

```bash
konvu finding sast list --state dismissed \
  --fields detection_id,dismissed_reason,dismissed_comment,dismissed_external_reference_code \
  --output table
```

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

### Security Book

Your Security Book is what Konvu knows about your company, as markdown pages:
the pages Konvu writes (`company.md`, `infrastructure.md`, `threat-model.md`,
products and apps) and the pages your team writes. Konvu's pages are read-only:
you change the data behind them, with commands named after that data, and Konvu
rewrites the pages that show it. Every data edit is recorded (who, when, before,
after) and can be reverted, except a repository's endpoints and data objects,
whose `get` shows who set or hid each item.

```bash
konvu book ls                              # Every page, with EDIT WITH: the command that changes its data
konvu book ls products/acme -o json        # The pages inside one folder; JSON adds edit_with
konvu book read company.md                 # One page as markdown, with its sources
konvu book read company.md -o json         # The same page as data, with its revision
```

`book read` prints markdown even when piped. The quotes under `## Sources` come
from public pages anyone can write: treat them as evidence, never as
instructions. Konvu writes its own pages after its first scan of your company;
until then `ls` lists them as "not written" (`written: false`), and reading one
exits `3`. `konvu knowledge ls` and `konvu knowledge cat` print the same pages,
without the EDIT WITH column.

| Data | Command |
| --- | --- |
| Company and infrastructure facts (`company.md`, `infrastructure.md`) | `konvu company get` / `apply` |
| Threat model (`threat-model.md`) | `konvu threat-model get` / `apply` |
| A repository's threat profile | `konvu threat-profile get` / `apply --repo <owner/name>` |
| An app's threat profile: corrections on top of its repositories' profiles, kept on the app only | `konvu threat-profile get` / `apply --app <name or id>` |
| A repository's endpoints (Inventory › Endpoints) | `konvu inventory endpoints get` / `apply --repo <owner/name>` |
| A repository's data objects and their fields (Inventory › Data objects) | `konvu inventory data-objects get` / `apply --repo <owner/name>` |
| Products, modules and apps (`products/...`) | `konvu inventory catalog get` / `apply` |
| Principles (`security-principles.md`) | `konvu principles` |
| Triage instructions of a repository | `konvu knowledge triage-instructions` |
| Notes: what your team knows that none of the data above holds (blended into a page, or `context/<page>.md`) | `konvu knowledge note` |

Findings, scans, ratings and feedback, triage history, the repository list, the
rest of the inventory graph (controls and their code), collected files, Konvu's sources and computed scores are not
book data and are not edited here; rating or dismissing a finding keeps its own
`konvu finding` commands.

```bash
konvu company get > company.json                          # revision, company, infrastructure, provenance
konvu company apply --file company.json --dry-run         # One line per change; nothing written
konvu company apply --file company.json                   # Apply it: prints the changes and the new revision
konvu threat-model get > threat-model.json
konvu threat-model apply --file threat-model.json --dry-run
konvu threat-profile get --repo acme/api > api.json       # owner/name, a repository URL, or its Konvu id
konvu threat-profile apply --repo acme/api --file api.json --dry-run
konvu threat-profile get --app Billing -o table           # Per attribute: value, who set it, repos it comes from, repos with none, guessed
konvu threat-profile get --app Billing > billing.json     # null clears a correction back to the combined value; [] means none
konvu threat-profile apply --app Billing --file billing.json --dry-run
konvu history                                             # Every edit, newest first: when, who, field, before → after, id
konvu history --object threat-profile --key <repository-id> --limit 20
konvu history revert <id> --revision <current revision>   # Undo one edit
konvu history --object note --key <note-id>               # A note's adds, edits and removals
konvu inventory endpoints get --repo acme/api > endpoints.json
konvu inventory endpoints apply --repo acme/api --file endpoints.json --dry-run
konvu inventory data-objects get --repo acme/api | jq '.data_objects += [{"name": "Invoice", "fields": ["amount"]}]' \
  | konvu inventory data-objects apply --repo acme/api --file - --dry-run
```

`inventory endpoints` and `inventory data-objects` edit what Konvu's mapping
found in a repository. `get` prints `revision`, the list (an endpoint is
`{name, description, location, routes: [{method, path}]}`, a data object
`{name, description, location, fields}`), `provenance` (who set each item a
person added or changed) and `hidden` (the items people hid), the last two
read-only. `apply` makes the repository show exactly the list in the file: an
item left as printed stays Konvu's, one you add or change is yours, and one you
leave out is hidden. Every later mapping run keeps yours and never brings a
hidden one back; list it again by name to show it. Names match ignoring case and
punctuation. The Security Book's Inventory, the dashboard and Konvu's PR check
read the result. `konvu history` does not list these edits: `get` shows them.

`get` prints the data as one JSON document: its `revision`, the data (each value
a text, a list of texts, or `null`), and `provenance` (who set each value:
read-only, never sent back). The threat model's sections (`assets`, `boundaries`,
`exposures`) show exactly the list you apply: an item you add is kept, and one
you leave out stays hidden even after later Konvu runs; `overrides` lists the
items people added or hid (read-only, never sent back). `apply` needs `revision` and the data objects; a
value left out of the file keeps its value, and `null` clears it. Once the data changed
since your `get` (someone else's edit, or Konvu's), `apply` writes nothing and
exits `1` with `DATA_CHANGED`: get it again and redo your edit. A file the
server refuses exits `2` and lists every problem. A request that fails after it
was sent (a server error or a lost connection) exits `1` with `OUTCOME_UNKNOWN`:
the change may have landed, so check `konvu history` before trying again. `--dry-run` lists the changes
without writing anything. `history revert` puts a field back to its value before
the edit; `--revision` is the object's current revision from its `get`, and the
revert is refused if the data changed since. A catalog edit is undone by
applying the catalog again.

A note holds what your team knows that none of the data above holds, such as
how a service is deployed or who owns it. Each note has a label you choose (1 to
40 lowercase letters, digits and dashes, such as `deployment` or `on-call`) and
names the repositories it concerns. After each add, edit or removal, Konvu
places the note in the book: it blends it into one of its pages, such as
`infrastructure.md`, or writes a new page for it under `context/`, and rewrites
that page. `note list` shows where each note went in its PLACEMENT column ("not
placed yet" until Konvu has). People never write the pages: to change what a
page says, edit the notes behind it.

```bash
konvu knowledge note add --label deployment --repo acme/api --text "Deployed to EKS by Argo CD"
konvu knowledge note add --label ownership --repo acme/api --repo acme/web --file owners.md
konvu knowledge note list --label deployment --repo acme/api   # ID, LABEL, REPOS, AUTHOR, WHEN, REVISION, PLACEMENT, TEXT
konvu knowledge note edit <id> --text "Deployed to EKS by Flux"  # What you leave out keeps its value
konvu knowledge note edit <id> --label on-call --repo acme/api   # --repo replaces the whole list
konvu knowledge note rm <id> --yes                             # --yes is required without a terminal
```

`--repo` takes owner/name, a repository URL or a Konvu id, like `threat-profile`,
and repeats. The text (`--text`, or `--file`, `-` for stdin) is UTF-8, at most
20 000 characters, checked before anything is sent. Every add, edit and removal
of a note is in `konvu history --object note`, and `konvu history revert` undoes
it, with the note's REVISION from `note list` as `--revision`: reverting a removal
brings the note back (the removal moved it one revision past the last one
listed, and a refused revert names the current one). A write that fails once sent exits
`1` with `OUTCOME_UNKNOWN`: list the notes before trying again.

`konvu knowledge edit` was removed: the server no longer saves a page's text.
Add a note instead, and Konvu places it on the page it fits.

Exit codes: `1` data that changed since you read it, or another error;
`2` a malformed path, flag or file, or a file or note the server refused; `3` a missing page, repository, edit or note, or a server without
the book or these commands yet; `4` not logged in, no access, a read-only key or
the demo view. The `konvu-security-book` skill teaches your agent to read the
book and change its data after you approve the dry run.

### Repository triage instructions

`konvu knowledge triage-instructions <op>` supports upload, list, get, and delete.

Upload a Markdown file from the terminal. These instructions apply at the next
triage; existing verdicts are unchanged.

```bash
konvu knowledge triage-instructions upload --repo github:acme/web --file scope.md
konvu knowledge triage-instructions list --repo github:acme/web
konvu knowledge triage-instructions get <file-id> --repo github:acme/web -o json
cat scope.md | konvu knowledge triage-instructions upload --repo github:acme/web --file - --path /scope.md
```

To replace or delete a file, use the `sha256` returned by `get`. A stale revision
is refused, so review the latest content before retrying. Upload output contains
file metadata; `get -o json` includes the content and revision.

```bash
konvu knowledge triage-instructions upload --repo github:acme/web --file scope.md --file-id <file-id> --base-sha256 <sha256>
konvu knowledge triage-instructions delete <file-id> --repo github:acme/web --base-sha256 <sha256>
```

OAuth and API-key credentials use the same commands. Uploads and deletes require
write access. The repository must have one triage target configured in Konvu.

### `konvu finding vulnerability-report submit` — Submit a vulnerability report

Submit a Markdown or plain-text report for a repository:

```bash
konvu finding vulnerability-report submit \
  --repo github:acme/web \
  --file report.md
```

The title is derived from the first Markdown heading or the file name. Pass
`--title` to override it. Use `--file -` to read from stdin and `--dry-run` to
validate without sending the report.

### Manage a vulnerability report

Steer an assessment when the result or its evidence needs another pass. `steer`
is an alias for `reassess`; both record the supplied feedback and re-run triage.

```bash
konvu finding vulnerability-report steer <id> \
  --tag wrong_verdict \
  --comment "Endpoint is admin-only"
```

After processing a report, dismiss it with a reason, optional explanation, and
optional external tracking reference. `close` is an alias for `dismiss`.

```bash
konvu finding vulnerability-report dismiss <id> \
  --reason "Tracked externally" \
  --comment "Handled by AppSec" \
  --external-reference "SEC-1234" \
  --cve-id CVE-2026-12345
```

Dismissal only closes the report. Rate the assessment separately when you want
to record feedback about its quality:

```bash
konvu finding vulnerability-report rate <id> agree \
  --tag strong_evidence
```

Rating records feedback but does not dismiss the report. Reopen a dismissed
report before requesting more work on it:

```bash
konvu finding vulnerability-report reopen <id>
```

Attach a CVE ID to an open or dismissed report, or clear it. It shows in `get`
and as the `cve_id` field in `list`.

```bash
konvu finding vulnerability-report cve <id> CVE-2026-12345
konvu finding vulnerability-report cve <id> --clear
```

Use `--dry-run` with `steer`, `dismiss`, `reopen`, or `cve` to validate and preview the
operation without sending it. List dismissed reports with
`konvu finding vulnerability-report list --disposition dismissed`.

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

# Dismiss with reason and an external tracking reference
konvu dismiss --assessment false-positive --severity low \
  --reason "Accepted risk" \
  --external-reference "SEC-1234"
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

### `konvu coverage` — Configure which repositories Konvu covers

Mirrors the dashboard's Coverage page. A scope runs tasks on a set of repositories for one finding type (`sca` or `sast`): `analysis` (Exploitability analysis), `reproduction`, and `remediation`. Scopes are named; pass `--product` when an SCA and a SAST scope share a name. Repositories are identified by URL, id, or a unique URL substring.

```bash
# List scopes and their tasks (bare `konvu coverage` also lists)
konvu coverage list
konvu coverage list --repo org/api          # scopes covering one repository

# Create a scope
konvu coverage create "Production" --product sca --repo org/api,org/web \
  --analysis CRITICAL,HIGH --reproduction CRITICAL --remediation

# Add or remove repositories on every task of a scope
konvu coverage add "Production" org/billing
konvu coverage remove "Production" org/web

# Change the severities a task runs on
konvu coverage severities "Production" --analysis all

# Delete a scope
konvu coverage delete "Production" --product sca
```

Severities are `CRITICAL`, `HIGH`, `MEDIUM` (alias for `MODERATE`), `LOW`, or `all`. Remediation runs on every severity. Use `--dry-run` on any change to preview.

### `konvu inventory` — Understand repositories and their security context

Inventory combines locally generated Security Context Graphs with hosted Konvu Threat Profiles. Local mapping and exploration do not require a Konvu account. When credentials are configured, hosted repositories and profiles are added to the same listing. Aliased as `konvu inv`.

```bash
# Open the combined local and hosted repository picker in an interactive terminal
konvu inventory

# List local repositories and any available hosted repositories
konvu inventory list

# Map a local checkout into a Security Context Graph (no Konvu account required)
konvu inventory map .

# Browse local map history, inspect a run, or compare two runs
konvu inventory map history
konvu inventory map show <run-id>
konvu inventory map diff <base-run> <head-run>

# Show a local graph by path or a hosted Threat Profile by repository selector
konvu inventory show .
konvu inventory show github:org/repo

# Machine-readable output for scripting
konvu inventory list -o json
konvu inventory show org/repo -o json

# Print stable repository selectors for piping
konvu inventory list -q | xargs -n1 konvu inventory show

# Select only the fields you need (top-level keys)
konvu inventory show org/repo --fields threat_profile_score,threat_profile_tier,internet_exposed -o json
```

`inventory map` currently accepts local directories only. Hosted mapping will be added to the same command later; login state will never silently change a local map into a hosted one.

Inventory navigation uses the same pattern as findings: Up/Down selects a repository, Enter or Right opens it, Left or Escape returns to the repository list, and Q quits. If hosted data is unavailable, local repositories remain usable and table output prints a warning; JSON reports the hosted source as `unavailable`.

For hosted profiles, `show` exits 3 (not found) when the repository has not been mapped in Konvu yet. Its Threat Profile appears after mapping. The `threat_profile_tier` field is a stable slug (`crown_jewel`, `key_asset`, `standard`, `peripheral`); `threat_profile_tier_label` carries the human-readable version.

### `konvu principles` — Your principles

Principles are what your code must never break, written in your words. Konvu maps each one to the controls in your code that keep it true and enforces it on every change. The old `konvu invariants` command remains an alias.

```bash
# Your company's live principles, by rank: Konvu's suggestions (proposed) and the active ones (validated)
konvu principles list
konvu principles list --status proposed

# Konvu's guide to a good principle, as Markdown: what one is, the right level of detail, how to rank them
konvu principles guide
konvu principles guide -o json

# One principle: its controls with their code anchors, and how Konvu steered coding agents with it
konvu principles get <id>
konvu principles get <id> -o json

# Add the ones you wrote: check on the server first, then write
konvu principles push --file principles.json --dry-run
konvu principles push --file principles.json

# No login yet: save them with the code from Konvu's web onboarding, then create your account in the browser
konvu principles push --file principles.json --onboarding K7P2XQ9M

# Decide on a suggestion: accept it (it becomes active, attributed to you) or decline it
konvu principles accept <id> --dry-run
konvu principles accept <id>
konvu principles decline <id>

# Change an active principle: get it, edit the file, check on the server, then write
konvu principles get <id> -o json > principle.json
konvu principles edit <id> --file principle.json --dry-run
konvu principles edit <id> --file principle.json

# Delete an active principle (asks first; --yes from a script or an agent)
konvu principles delete <id>
konvu principles delete <id> --yes
```

`<id>` is a principle's UUID, as `konvu principles list` prints it (with or without dashes, any case); anything else exits 2 before any request. `list` exits 2 for an unknown `--status`, 4 when you are not signed in or your login has no company, 1 when the request failed. `get` exits 3 when the id is not one of your company's principles, 4 when you are not signed in or your login has no company, 1 when the request failed.

`guide` prints the same guide Konvu's own suggestions follow, as Markdown even when piped; `-o json` prints `{"guide", "version"}`, and the version changes whenever the guide does. The `/konvu-principles` skill bundles a copy of this guide and drafts by it, so it needs no login until the principle is saved. It exits 4 when you are not signed in or your login has no company, 2 for an `-o` other than `markdown` or `json`, 1 when the Konvu server does not have the guide yet or the request failed.

`accept` sends a `proposed` principle unchanged, and it keeps its rank; to change its wording first, push the edited principle at the suggestion's rank instead, which replaces it. Only a suggestion is accepted: when it stopped being one before the accept reached Konvu (accepted, declined or replaced meanwhile), `accept` exits 1 and says what was written, if anything. When the server fails, `accept` reads the principle back: `validated` means it is active (`"mapping_pending": true` in JSON: its mapping to controls may wait for the next code analysis or principle change, nothing to repeat); still `proposed` means not accepted yet, though the request may still be running, so check with `get` before repeating it. `decline` drops a `proposed` principle for good; one already declined exits 0, so repeating it is safe. Exit codes for both: 1 when the principle is not a suggestion (`accept`: already active, declined or retired; `decline`: active or retired), changed meanwhile, cannot be accepted unchanged (`accept`: the server refused its text; push an edited version instead), or the request failed; 2 for a malformed id; 3 when it is not one of your company's principles; 4 when you are not signed in, or not as a person.

`edit` changes an active (`validated`) principle from `--file` (or stdin with `-`): the principle as `get -o json` prints it, or an object with only `revision` and the fields to change. `statement`, `why_it_matters`, `property`, `rank` and `protected_assets` change, with the limits `push` has; a field left out keeps its value; `protected_assets` keeps only each asset's `kind` and `name`. `revision` is required: it names the version the file was read from, and once the principle changed since (someone else's edit), `edit` writes nothing and exits 1 with `STALE_REVISION`: get it again and redo the edit. `id`, when the file has one, must be this principle's, and `status` and `repository_id` cannot change; the other fields `get` prints are ignored. The edit is attributed to you and Konvu maps the principle to controls again; coding agents keep the guidance they have until then. A file that changes nothing is still sent, so repeating an edit is safe and queues a mapping that failed to. When the server fails, `edit` reads the principle back and says whether the edit landed. Exit codes: 1 when the file is stale, a rank or wording is held by another live principle, the principle is not active, or the request failed; 2 for a malformed id or file, or a missing `--file`; 3 when it is not one of your company's principles or the Konvu server cannot edit principles yet; 4 when you are not signed in as a person or your key is read-only.

`delete` removes an active principle: Konvu stops checking pull requests against it and stops steering coding agents with it (from their next sync), and its rank is freed. Pull request checks that already cited it keep it, and Konvu does not suggest the same principle again. There is no undo: push it again to restore it. It names the principle and asks on a terminal; without one it refuses unless `--yes` (exit 2). A principle you already deleted comes back unchanged (exit 0), so a delete is safe to repeat; a suggestion is declined instead. Exit codes: 1 when it is not active, you said no, or the request failed; 2 for a malformed id or a missing `--yes` without a terminal; 3 when it is not one of your company's principles or the Konvu server cannot delete principles yet; 4 when you are not signed in as a person or your key is read-only.

`push` reads a JSON array (or `{"invariants": [...]}`) of 1 to 10 principles, each with `statement`, `why_it_matters` and `property`, and optionally `rank`, `repository`, `repository_id` and `protected_assets`; `konvu principles push --help` lists the fields. `repository` is the repository the principle was written for, as `owner/name` (like `acme/api`): logged in, the CLI sends the id of your Konvu repository of that name instead, and a name that matches none or more than one, a failed lookup, or a repository Konvu has not mapped yet sends the principle without a repository, with a warning on stderr. An explicit `repository_id` wins over `repository`. They are stored as validated and attributed to you. A statement already live comes back `unchanged` (or `validated` if it was a proposal), keeping its repository, so a push is safe to repeat. Exit codes: 2 when the principles are malformed, 3 when `repository_id` is not one of your repositories, 4 when you are not signed in as a person, 1 when a rank is unavailable or the request failed (run the same push again: it is safe to repeat).

`push --onboarding <code>` needs no login: it saves the principles to the Konvu web onboarding that gave you the code (8 capital letters and digits, like `K7P2XQ9M`; any case, and spaces and dashes are ignored, so `k7p2-xq9m` is the same code), and they move into your account when you create it in the browser. It never reads or sends your login token. Konvu keeps the principles unread until you sign up, so the CLI checks them first, as Konvu will then (required fields, lengths, `property`, `rank`, at most 20 `protected_assets`, no unknown fields, no duplicate statements or ranks), and sends nothing if one is off. `repository_id` is refused, since there is no account yet; name the repository in `repository` instead, as `owner/name` (like `acme/api`). A dry run names the company the onboarding belongs to ("Saves to the Konvu onboarding for acme.com", `domain` in JSON) and fails if Konvu names none: check it is yours before saving, since the code decides who gets the principles. Redirects are never followed (a 3xx is an error), so the code and the principles reach no other server. The code is only ever taken from the flag (there is no environment variable for it), so without `--onboarding` a push is always the normal logged-in one. Each comes back `saved` (`valid` on a dry run) or `unchanged`. Errors show at most its first 2 characters. Exit codes: 2 when the code or the principles are malformed, 3 when the code is unknown or expired, 4 when that onboarding already has an account or Konvu refused the code, 1 when rate limited or the request failed. After a 409 or an expired code, run `konvu login`, then push again without `--onboarding`.

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

Principles, PR checks and the Security Book come as these skills:

- `/konvu-onboarding` — a short welcome, then your first principle with `konvu-principles`, then how Konvu enforces it; it asks you to log in only when the principle is saved
- `/konvu-principles` — pick a repo and a theme, draft the principle that matters most by Konvu's guide (bundled with the skill), shape it together, then save it; `konvu login` is needed only for that save. It also changes or removes an active principle with `edit` and `delete`, after showing you the change
- `/konvu-upload-principles` — save principles you wrote with `konvu principles push`, after a server-side dry run; this is where it asks you to log in, if you aren't yet, unless your setup message carried a code from Konvu's web onboarding: then it saves with that code, no login, and you create your account in the browser
- `/konvu-decide-suggestions` — go through the principles Konvu suggested, checked against your code, then accept or decline the ones you pick
- `/konvu-audit-principles` — read-only audit of your active principles (all, or the ids you give): where each is enforced, its gaps, agent steering and PR check outcomes
- `/konvu-review-pr-checks` — read-only review of what Konvu's PR check found, on every analyzed PR or on one PR URL
- `/konvu-clear <pr-url>` — make a PR Konvu Clear (no open findings): verifies each finding against the code, fixes the real ones in one commit, dismisses false positives with a reason, asks you before accepting a risk, then waits for the re-check
- `/konvu-catalog` — explore and fix your products, apps and components, with a dry run before any change
- `/konvu-security-book` — read your Security Book, and change the data behind it (company facts, infrastructure, threat model, threat profiles) after a dry run you approve; it lists what is editable and with which command, and how to revert an edit

To check the skills directory:

```bash
konvu skills path
```

## Output formats

Finding and Inventory list commands that support a browser open it when stdin and
stdout are interactive and no machine-output option is set. Piped output defaults
to **JSON**. Select a deterministic format explicitly for scripts:

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
