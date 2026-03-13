---
name: recipe-weekly-triage
version: 3.0.0
description: "Guided triage workflow for Konvu security findings. Use this skill whenever the user wants to triage, review, or go through their security findings — even if they say something casual like 'let's review findings', 'triage time', 'what needs my attention', 'go through my vulns', or 'weekly security review'. Also trigger when the user asks to rate, agree/disagree with, or act on multiple findings in bulk."
metadata:
  requires:
    bins: ["konvu"]
---
# Weekly Finding Triage

> **PREREQUISITE:** Read `../konvu-shared/SKILL.md` for auth and global flags.

A guided triage workflow where **rating happens inline** as the user validates each group, and **bulk actions** (dismiss, create tickets) are collected and executed at the end after confirmation.

Rating an assessment as agree/disagree is lightweight feedback — it should happen the moment the user makes the call, not be deferred. Heavier actions that change state (dismissing findings, creating tickets) are batched so the user can review them before they run.

## Progress Bar

Every message must start with a two-line progress bar. The first line shows the full pipeline so the user always knows where they are. The second line uses breadcrumb nesting to show exactly what's happening right now.

```
✓ Setup → [Validate] → Actions → Execute
             ↳ Exploitable — Group 2/3 · lodash prototype pollution · rated 4 so far
```

- `✓` = completed step, `[brackets]` = active step, plain text = upcoming step
- The `↳` detail line is indented to align under the active step, showing it as a child
- The pipeline line updates as steps complete: `✓ Setup → ✓ Validate → [Actions] → Execute`
- Skipped steps show as `– Validate` instead of `✓`

The steps are:
1. **Setup** — timeframe, fetch findings, overview
2. **Validate** — review all assessments (exploitable first, then false positives), rate inline
3. **Actions** — define and confirm bulk actions (dismiss, create tickets) based on validation
4. **Execute** — run the bulk actions and show results

Within a step, show group progress ("Group 2/3") and within a breakdown ("Finding 3/5").

## Workflow

```
Setup → Validate (exploitable, then false positives) → Actions → Execute
```

The user can exit at any point — show the action plan for whatever was validated so far.

## Step 1: Setup

Start with a single sentence explaining what's about to happen — before any CLI calls:

> "Konvu has investigated your recent vulnerabilities and assessed which ones are actually exploitable in your environment. Let's review those assessments together."

Ask for the review timeframe (default 7 days), then fetch both categories:

```bash
konvu finding list --since <period> --assessment exploitable -o json --limit 1000
konvu finding list --since <period> --assessment false-positive -o json --limit 1000
```

If either command fails with exit code 4 (auth error), tell the user to run `konvu login` and stop.

Show the counts before diving in:

> "Last 7 days: 3 exploitable, 8 false positives."

If a category is empty, say so upfront.

## Step 2: Validate Assessments

This step covers all assessments: exploitable findings first, then false positives. Both follow the same structure.

If a category has zero findings, skip it: "No exploitable findings — moving to false positives."

### Grouping

Group findings before presenting:
- Same CVE across multiple repositories or manifests
- Same dependency with multiple CVEs and similar reasoning
- Findings with nearly identical assessment summaries

### Assessment summary — always show it

The assessment summary is the most valuable piece of information for triage. It tells the user *why* Konvu thinks something is exploitable or a false positive. Without it, the user is making decisions blind.

Every group and every individual finding presentation must include the assessment summary. This is not optional — it's the reason this tool exists.

The `assessment_summary` field from `finding list` is sometimes specific and useful (e.g., "Prototype pollution reachable via user input in request parsing") but sometimes generic and unhelpful (e.g., "Not exploitable in your context."). When the summary is generic, make **one** `konvu finding get <id> -o json` call for a representative finding in the group and pull the checklist conclusion instead — that's where the real reasoning lives (e.g., "your app is a React frontend on Unix, and this vulnerability affects backend components only").

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

Why exploitable: Prototype pollution reachable via user input in request
parsing. The lodash.merge() call in request middleware passes unsanitized
user objects directly.
```

### Validation options

Use AskUserQuestion. When the user agrees or disagrees, **rate immediately** — don't defer it:

**For exploitable findings:**
- **Agree** — Rate as helpful right now (`konvu finding rate <id> agree` for each finding in the group). Then move on.
- **Disagree** — Rate as disagree right now (`konvu finding rate <id> disagree`). Ask for an optional comment. Then move on.
- **Need more context** — Flag for deeper review after all groups are done.
- **Skip** — No judgment, move on.

**For false positive findings:**
- **Agree** — Rate as helpful right now. Then move on.
- **Disagree** — Rate as disagree right now. You think this IS exploitable — flag for deeper review.
- **Need more context** — Flag for deeper review.
- **Skip** — Move on.

"Skip", "pass", "next", "move on" all mean the same thing — move forward without rating.

When breaking down a group, show position: "Finding 2/4 in the lodash group".

### Flagged findings: deeper review

After all groups (both exploitable and false positive) are done, review any flagged findings. For each, fetch full evidence:

```bash
konvu finding get <id> --include evidence -o json
```

Present readably:
- **Assessment summary** and reasoning
- **Reachability**: dependency-level and function-level (call sites, imports)
- **Runtime evidence**: if available
- **Checklist items**: each with conclusion and proof snippets
- **EPSS score** and CVSS vector

Then ask for a final call — no more deferring:
- **Agree** — Rate immediately
- **Disagree** — Rate immediately with comment
- **Skip** — No action

## Step 3: Define Actions

Ratings have already been submitted during validation. Now propose bulk actions based on what was validated — dismissals for agreed false positives, tickets for agreed exploitables.

```
✓ Setup → ✓ Validate → [Actions] → Execute
                          ↳ Proposing actions based on validation

Rated during validation: 12 agree, 3 disagree.

Pending bulk actions:

Dismiss (2):
  - CVE-2024-9999 | moment 2.29.1 | org/legacy | "Confirmed false positive"
  ...

Create ticket (1):
  - CVE-2024-1234 | lodash prototype pollution | 4 findings across 3 repos
  ...

Skipped (4 findings, no action taken).
```

If there are no actions to propose, skip straight to the summary.

Then ask via AskUserQuestion:
- **Execute all** — Run everything as shown
- **Edit** — Let the user modify specific items before executing
- **Cancel** — Don't execute, just show summary

## Step 4: Execute

Run all confirmed bulk actions:

```bash
# Dismiss confirmed false positives
konvu dismiss --issues <id1>,<id2> --reason "Confirmed false positive"
```

For ticket creation, ask where (Linear, GitHub Issues, etc.) and group related findings into a single ticket per CVE or dependency.

Show progress as actions execute. Then present the final summary:

```
✓ Setup → ✓ Validate → ✓ Actions → ✓ Execute
                                      ↳ Triage complete

Rated: 12 agree, 3 disagree.
Dismissed: 2 findings.
Tickets: 1 created.
Skipped: 4 findings with no action.
```

If any action fails, report the error and continue with the rest.

## Tracking

Keep a running log throughout the triage:
- Finding ID, CVE, dependency, repository, assessment summary
- User judgment: agreed / disagreed / flagged / skipped
- Rating result: submitted / failed
- Pending bulk action: dismiss / create ticket / none
- Execution result: success / failed / not executed

This log powers both the Action Plan and the final summary.

## Interaction Style

- **Progress bar on every message.** Never skip it.
- **Assessment summary on every finding/group.** This is what helps users decide. Never present a finding without explaining *why* it was assessed the way it was.
- **Rate immediately on agree/disagree.** Don't make the user wait — rating is lightweight feedback, not a destructive action.
- **Compact tables over prose.** Show finding details in tables, not paragraphs.
- **AskUserQuestion for every decision.** The guided flow is the whole point.
- **Respect "move on" / "skip" / "pass" / "next".** Move forward without asking again.
- **Brief between steps.** "All exploitable validated. 8 false positives next." is enough.
- **Never dump raw JSON.** Always format into readable tables and summaries.
- **Graceful exit.** "stop", "done", "exit" → jump to Action Plan for whatever was validated so far.
