---
name: recipe-weekly-triage
version: 2.0.0
description: "Guided triage workflow for Konvu security findings. Use this skill whenever the user wants to triage, review, or go through their security findings — even if they say something casual like 'let's review findings', 'triage time', 'what needs my attention', 'go through my vulns', or 'weekly security review'. Also trigger when the user asks to rate, agree/disagree with, or act on multiple findings in bulk."
metadata:
  requires:
    bins: ["konvu"]
---
# Weekly Finding Triage

> **PREREQUISITE:** Read `../konvu-shared/SKILL.md` for auth and global flags.

A guided triage workflow with two distinct phases: **Validate** (review all findings and record your judgment) then **Act** (execute all actions in batch). No actions are taken until the user confirms at the end.

## Progress Bar

Every message must start with a progress bar. This prevents the "where am I?" feeling:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 Step 2/6 · Validate Exploitable · Group 1/3
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

The steps are:
1. **Setup** — auth check, timeframe, overview
2. **Validate Exploitable** — review exploitable findings
3. **Validate False Positives** — review false-positive findings
4. **Review Flagged** — deep dive into findings that need more context
5. **Action Plan** — present all pending actions for confirmation
6. **Execute** — run the actions and show results

When a step has no findings, skip it: "Step 2: No exploitable findings — skipping."

Within a step, show group progress ("Group 2/3") and within a breakdown ("Finding 3/5").

## Workflow

```
Setup → Validate Exploitable → Validate False Positives → Review Flagged → Action Plan → Execute
```

The user can exit at any point — show the action plan for whatever was validated so far.

## Step 1: Setup

Verify authentication — run `konvu whoami -o json`. If exit code 4, tell the user to run `konvu login` and stop.

Ask for the review timeframe (default 7 days), then fetch both categories upfront:

```bash
konvu finding list --since <period> --assessment exploitable -o json --limit 1000
konvu finding list --since <period> --assessment false-positive -o json --limit 1000
```

Show an overview before diving in:

> "Last 7 days: 3 exploitable, 8 false positives. Let's validate them."

If a category is empty, say so upfront.

## Step 2: Validate Exploitable

If zero, skip: "Step 2: No exploitable findings — moving on."

### Grouping

Group findings before presenting:
- Same CVE across multiple repositories or manifests
- Same dependency with multiple CVEs and similar reasoning
- Findings with nearly identical assessment summaries

### Assessment context

The assessment summary is the most valuable piece of information for triage — it tells the user *why* something is or isn't exploitable. Always display it prominently.

The `assessment_summary` field from `finding list` is sometimes specific and useful (e.g., "Prototype pollution reachable via user input in request parsing") but sometimes generic and unhelpful (e.g., "Not exploitable in your context."). When the summary is generic like this, make **one** `konvu finding get <id> -o json` call for a representative finding in the group and pull the checklist conclusion instead — that's where the real reasoning lives (e.g., "your app is a React frontend on Unix, and this vulnerability affects backend components only").

Only one `get` call per group is needed — findings in the same group share the same reasoning.

### Presenting groups

Present each group as a compact table with the assessment reasoning below:

```
📦 Group 1/3: lodash prototype pollution (CVE-2020-28500) — 4 findings

| Repo | Dep | Severity | Fix |
|------|-----|----------|-----|
| org/app-a | lodash 4.17.20 | high | available |
| org/app-b | lodash 4.17.20 | high | available |
| org/api   | lodash 4.17.21 | high | available |
| org/web   | lodash 4.17.20 | high | available |

Why exploitable: Prototype pollution reachable via user input in request parsing.
```

The "Why exploitable" / "Why false positive" line is not optional — it's the most important line in the group presentation. Without it the user is making decisions blind.

### Validation options

Use AskUserQuestion. During validation, nothing is executed — just recording the user's judgment:

- **Agree** — You confirm the exploitable assessment. (Will rate as agree + offer ticket in Action Plan)
- **Disagree** — You disagree with the assessment. (Will rate as disagree in Action Plan)
- **Review individually** — Break the group apart and validate each finding separately.
- **Skip** — No judgment, move on.

"Skip", "pass", "next", "move on" all mean the same thing — move forward without recording a judgment.

When breaking down a group, show position: "Finding 2/4 in the lodash group".

## Step 3: Validate False Positives

Same structure as Step 2, but for false-positive findings.

Validation options:
- **Agree** — You confirm it's a false positive. (Will rate as agree + offer dismiss in Action Plan)
- **Disagree** — You think this IS exploitable. (Will rate as disagree + auto-flag for Step 4)
- **Review individually** — Break down the group.
- **Skip** — Move on.

## Step 4: Review Flagged

Covers findings flagged from Steps 2–3 (disagreed false positives, or individual findings the user wants more context on).

If nothing was flagged, skip: "Step 4: Nothing flagged — moving to action plan."

For each flagged finding, fetch full evidence:

```bash
konvu finding get <id> --include evidence -o json
```

Present readably:
- **Assessment summary** and reasoning
- **Reachability**: dependency-level and function-level (call sites, imports)
- **Runtime evidence**: if available
- **Checklist items**: each with conclusion and proof snippets
- **EPSS score** and CVSS vector

Final validation (no more deferring):
- **Agree** — Confirm the original assessment
- **Disagree** — Override with your judgment
- **Skip** — No action

## Step 5: Action Plan

Present everything that will happen, grouped by action type. Nothing has been executed yet — this is the user's chance to review and confirm before anything runs.

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 Step 5/6 · Action Plan
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Here's what I'll do based on your validation:

Rate as AGREE (12):
  - CVE-2024-1234 | lodash 4.17.20 | org/app-a | exploitable
  - CVE-2024-1234 | lodash 4.17.20 | org/app-b | exploitable
  ...

Rate as DISAGREE (3):
  - CVE-2024-5678 | express 4.18.1 | org/api | false-positive
  ...

Dismiss (2):
  - CVE-2024-9999 | moment 2.29.1 | org/legacy | "Confirmed false positive"
  ...

No action / skipped (4):
  - ...
```

Then ask via AskUserQuestion:

- **Execute all** — Run everything as shown
- **Create tickets too** — Execute all + create tickets for agreed exploitables (ask where: Linear, GitHub Issues, etc.)
- **Edit** — Let the user modify specific items before executing
- **Cancel** — Don't execute anything, just show summary

For agreed exploitable findings, proactively offer ticket creation — group related findings into a single ticket per CVE or dependency.

For agreed false positives, proactively offer dismissal.

## Step 6: Execute

Run all confirmed actions:

```bash
# Rate findings
konvu finding rate <id> agree
konvu finding rate <id> disagree --comment "<comment>"

# Dismiss confirmed false positives
konvu dismiss --issues <id1>,<id2> --reason "Confirmed false positive"
```

Show progress as actions execute. Then present the final summary:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 Step 6/6 · Done
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Completed: 12 rated agree, 3 rated disagree, 2 dismissed, 1 ticket created.
Skipped: 4 findings with no action.
Remaining: 2 findings not reviewed (exited early).
```

If any action fails, report the error and continue with the rest.

## Tracking

Keep a running log throughout the triage:
- Finding ID, CVE, dependency, repository
- Validation: agreed / disagreed / flagged / skipped
- Planned action: rate agree / rate disagree / dismiss / create ticket / none
- Execution result: success / failed / not executed

This log powers both the Action Plan and the final summary.

## Interaction Style

- **Progress bar on every message.** Never skip it.
- **Compact tables over prose.** Show finding details in tables, not paragraphs.
- **AskUserQuestion for every decision.** The guided flow is the whole point.
- **Respect "move on" / "skip" / "pass" / "next".** Move forward without asking again.
- **Brief between steps.** "All exploitable validated. 8 false positives next." is enough.
- **Never dump raw JSON.** Always format into readable tables and summaries.
- **No actions during validation.** The whole point of the validate-then-act pattern is that the user sees everything before anything runs.
- **Graceful exit.** "stop", "done", "exit" → jump to Action Plan for whatever was validated so far.
