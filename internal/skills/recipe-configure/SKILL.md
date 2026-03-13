---
name: recipe-configure
version: 1.0.0
description: "Configure Konvu skill policies — repo scope, triage preferences, integrations, and report settings. Use this skill when the user wants to set up, configure, tune, or change their Konvu workflow — phrases like 'configure konvu', 'set up my preferences', 'change triage settings', 'which repos should I monitor', 'set up ticketing', 'configure policies', 'update my config', or 'konvu setup'. Also trigger on first-use signals like 'I just installed konvu' or 'getting started'. If a user asks a question that a policy would answer ('which repos are in scope?', 'what severity do I triage?'), check if the config exists and offer to set it up if not."
metadata:
  requires:
    bins: ["konvu"]
---
# Configure Policies

> **PREREQUISITE:** Read `../konvu-shared/SKILL.md` for auth and global flags.

An interactive setup wizard that creates a local config file defining how the other Konvu skills behave. Without this, every skill has to ask the same filtering questions each time. With it, skills read the config and adjust their behavior automatically.

The config lives at `~/.config/konvu/policies.json` (Linux), `~/Library/Application Support/konvu/policies.json` (macOS), or `%APPDATA%\konvu\policies.json` (Windows) — the same directory as `credentials.json`. To find the exact path, check where `konvu` stores credentials:

```bash
# The config dir is the same as where credentials live
konvu whoami -o json
```

## Config Schema

```json
{
  "version": 1,
  "organization": "Acme Corp",
  "repos": {
    "mode": "all",
    "include": [],
    "exclude": []
  },
  "triage": {
    "severity": ["critical", "high", "moderate", "low"],
    "default_period": "7d"
  },
  "integrations": {
    "ticketing": null,
    "compliance": null
  }
}
```

**Fields:**

- `version` — Schema version. Always `1` for now.
- `organization` — Display name for reports.
- `repos.mode` — `"all"` (everything, minus excludes), `"include"` (only listed repos), or `"exclude"` (everything except listed repos). Default: `"all"`.
- `repos.include` / `repos.exclude` — Repo identifiers (e.g., `"github:org/repo"`).
- `triage.severity` — Which severities the triage skill should include. Default: all four.
- `triage.default_period` — Default lookback period for triage. Default: `"7d"`.
- `integrations.ticketing` — Ticketing configuration (see Integrations section).
- `integrations.compliance` — Compliance platform configuration (see Integrations section).

## Wizard Flow

The wizard walks through each section conversationally. Every section is optional — the user can skip any of them and come back later. Use AskUserQuestion for each decision point.

On first run, introduce the wizard:

> "Let's set up your Konvu policies. These configure how triage, posture reports, and other skills work — things like which repos to include, what severity to focus on, and where to create tickets. Everything is optional and you can change it anytime."

If a config already exists, read it first, show a summary of current settings, and ask what the user wants to change. Don't re-run the whole wizard — just jump to the relevant section.

### 1. Organization Name

> "What's your organization or team name? This shows up in report headers."

Simple text input. Skip if the user doesn't care.

### 2. Repository Scope

This is the most important section and the one where onboarding needs to be smooth. The goal is to help users quickly exclude repos they don't care about (test repos, forks, archived projects) without having to enumerate every repo they *do* care about.

**Step 1: Fetch the full repo list with finding counts.**

```bash
konvu finding list --group-by repository -o json --limit 1000
```

**Step 2: Present repos grouped by org/owner**, sorted by finding count descending. Show a compact summary:

```
Here are your repositories with open findings:

  acmekonvu (7 repos, 135 findings)
    juice-shop          33 findings
    webgoat             31 findings
    pygoat              22 findings
    ghost               15 findings
    redash              12 findings
    discourse            7 findings
    nuxeo                3 findings

  konvudemo (1 repo, 32 findings)
    webgoat             32 findings

  testpaul1 (2 repos, 61 findings)
    webgoat             32 findings
    webgoat2            29 findings

  zetaben (2 repos, 18 findings)
    arxiv-opds          10 findings
    gumroad              8 findings
```

**Step 3: Ask what to exclude.** The default should be "include everything" — most users just need to exclude a few noisy repos. Offer several ways to exclude:

- **By org** — "Exclude all repos from `testpaul1`?" (useful for test/personal accounts)
- **By name pattern** — "Exclude repos with 'webgoat' in the name?" (useful for demo/test repos)
- **By specific repo** — "Exclude `acmekonvu/juice-shop`?"
- **Keep all** — "Looks good, include everything."

Make it conversational, not a checklist. If the data suggests obvious candidates for exclusion (repos named "test", "demo", "webgoat", "juice-shop", or repos belonging to orgs that look personal), proactively suggest them:

> "I see some repos that might be test/demo environments — juice-shop, webgoat, pygoat. Want to exclude those from triage and reports?"

The user can accept the suggestion, modify it, or keep everything. Store the result as either an include list or exclude list depending on what's simpler.

### 3. Triage Preferences

> "What severity levels should triage focus on? Default is all levels (critical, high, moderate, low). Some teams only triage critical and high."

Present the options:
- **All severities** (default)
- **Critical and high only**
- **Critical only**
- **Custom** — let the user specify

Also ask about the default review period:

> "How far back should triage look by default? (default: 7 days)"

### 4. Integrations

Integrations are about connecting Konvu workflows to external systems. The config stores what's available — the skills use this to know where to create tickets or sync dismissals.

#### Ticketing

> "Do you use a ticketing system for tracking security remediation? (Linear, GitHub Issues, Jira, or other)"

If yes, store the provider and any relevant project/label info. The triage skill uses this to know where to create tickets when the user says "create a ticket for this."

For now, ticketing works through MCP integrations (Linear MCP, GitHub MCP) rather than the Konvu CLI directly. The config records the user's preference so skills can create tickets with the right context:

```json
"ticketing": {
  "provider": "linear",
  "team": "Security",
  "label": "vulnerability"
}
```

or:

```json
"ticketing": {
  "provider": "github",
  "repo": "org/security-issues"
}
```

If the user mentions a provider, check if the corresponding MCP is available. If not, note it:

> "I don't see a Linear MCP connection right now. You can add one later and the config will be ready for it."

#### Compliance

> "Do you use a compliance platform like Vanta? If so, dismissed findings can be synced there."

Same approach — store the provider, check for the MCP, note if it's not connected yet:

```json
"compliance": {
  "provider": "vanta"
}
```

If the user doesn't use any of these, skip — `null` is the default.

### 5. Summary and Save

After the wizard, show a summary of everything configured:

```
📋 Konvu Policies

Organization: Acme Corp

Repos: All repositories, excluding:
  - testpaul1/* (2 repos)
  - acmekonvu/juice-shop
  - acmekonvu/webgoat

Triage: Critical and High severity, 7-day lookback

Ticketing: Linear → Security team, "vulnerability" label
Compliance: Vanta (MCP not connected yet)
```

Ask for confirmation, then write the config file.

## How Other Skills Use the Config

This section defines the contract between the config and the other skills. Skills should read `policies.json` at the start and adjust their behavior. The AI assistant is responsible for reading the config and modifying the CLI commands accordingly — the config doesn't change how the CLI binary works, it changes how the skills call the CLI.

### Reading the config

At the start of any Konvu skill workflow, check if `policies.json` exists. Read it to understand the user's preferences. The config dir path can be determined by platform:
- macOS: `~/Library/Application Support/konvu/policies.json`
- Linux: `~/.config/konvu/policies.json`
- Windows: `%APPDATA%\konvu\policies.json`

If the config doesn't exist, skills work with defaults (all repos, all severities, no integrations). Don't prompt the user to configure unless they ask.

### Repo filtering

When `repos.mode` is `"exclude"`, skills add `--repo` filters to exclude listed repos from queries. In practice, this means running the query without repo filters and post-filtering the results, since the CLI's `--repo` flag filters *to* a repo rather than *excluding* one.

When `repos.mode` is `"include"`, skills run separate queries per repo or use the `--repo` flag directly.

When `repos.mode` is `"all"` with excludes, treat it like `"exclude"` mode.

### Triage preferences

The triage skill uses `triage.severity` to set the `--severity` flag and `triage.default_period` for the `--since` flag. If the user overrides these in conversation ("triage critical findings from the last month"), the override takes precedence for that session.

### Integrations

When the triage skill reaches the "create ticket" action:
- If `ticketing` is configured, use the provider's MCP to create the ticket with the configured team/label/repo.
- If not configured, ask the user where they want the ticket.

When dismiss actions are executed:
- If `compliance` is configured, note in the output that the dismissal should be synced. (The actual sync depends on the compliance MCP being available.)

### Report settings

The report skill uses `organization` for the report header. It uses the repo scope to determine which findings appear in the report.

## Updating the Config

When the user wants to change a setting, don't re-run the whole wizard. Read the current config, show the relevant section, and update just that part:

> User: "Change my triage severity to critical only"
> → Read config, update `triage.severity` to `["critical"]`, write config, confirm.

> User: "Add repo X to the exclude list"
> → Read config, append to `repos.exclude`, write config, confirm.

> User: "Show my config"
> → Read and display the full config summary.

## Interaction Style

- **Conversational, not form-like.** The wizard should feel like a conversation, not a series of form fields. Use AskUserQuestion, respond to what the user says, and adapt.
- **Smart defaults.** Everything has a sensible default. The user should be able to press enter through the whole wizard and have a working config.
- **Proactive suggestions for repos.** Don't just list repos and ask "which ones?" — analyze the data and suggest what to exclude based on naming patterns (test, demo, fork) and context.
- **Skip-friendly.** "Skip" or "next" or "don't care" all mean accept defaults and move on.
- **Show the impact.** After excluding repos, show how many findings that removes: "Excluding those 5 repos removes 157 findings from triage scope (34% of total)."
