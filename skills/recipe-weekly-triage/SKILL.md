---
name: recipe-weekly-triage
version: 1.0.0
description: "Guided triage workflow for Konvu security findings. Use this skill whenever the user wants to triage, review, or go through their security findings — even if they say something casual like 'let's review findings', 'triage time', 'what needs my attention', 'go through my vulns', or 'weekly security review'. Also trigger when the user asks to rate, agree/disagree with, or act on multiple findings in bulk."
metadata:
  requires:
    bins: ["konvu"]
---
# Weekly Finding Triage

> **PREREQUISITE:** Read `../konvu-shared/SKILL.md` for auth and global flags.

A guided workflow that walks you through recent security findings, groups them intelligently, and lets you act on each one — agree, disagree, flag for deeper review, create tickets, or dismiss. The goal is to replace a series of manual CLI commands with an opinionated conversational loop.

## Workflow Overview

```
Start → Choose timeframe
  → Phase 1: Exploitable findings (grouped)
    → Per group/finding: Agree (+ offer ticket) | Disagree (+ offer dismiss) | Flag for review | Exit
  → Phase 2: False positive findings (grouped)
    → Per group/finding: Agree (+ offer dismiss) | Disagree (→ auto-flag for review) | Flag for review | Exit
  → Phase 3: Deep review of flagged findings (with full evidence)
    → Per finding: Agree | Disagree | Skip | Exit
  → Summary
```

The user can exit at any point — the summary shows everything done so far.

## Phase 0: Setup

Ask the user for the review timeframe using AskUserQuestion. Default to 7 days.

Example:
> "Ready to triage your recent findings. Review the last 7 days?"
> Options: "Last 7 days" / "Last 14 days" / "Last 30 days" / "Custom"

If they pick custom, ask for a specific period.

Verify authentication first — run `konvu whoami -o json`. If it fails with exit code 4, tell the user to run `konvu login` and stop.

## Phase 1: Exploitable Findings

Fetch all exploitable findings for the period:

```bash
konvu finding list --since <period> --assessment exploitable -o json --limit 1000
```

If zero results, tell the user and move to Phase 2.

### Grouping

Before presenting findings one-by-one, analyze the batch and group findings that can reasonably be acted on together. Use your judgment — common grouping patterns:

- Same CVE across multiple repositories or manifests
- Same dependency with multiple CVEs that share similar exploitability reasoning
- Findings with nearly identical assessment summaries
- Same vulnerability class in the same repository

Present each group with a summary of what the findings have in common and how many there are. Use AskUserQuestion:

> "I found 4 exploitable findings related to `lodash` prototype pollution (CVE-2020-28500) across 3 repos — all with similar assessment reasoning. Act on these as a group, or break down individually?"
> Options: "Act as group" / "Break down"

Findings that don't fit any group get presented individually.

### Per finding (or group) actions

Present the finding's key details: CVE, severity, dependency, repository, and the **assessment summary** (not full evidence — that's for Phase 3). Use AskUserQuestion with these options:

- **Agree** — Run `konvu finding rate <id> agree`. Then ask if they want to create a ticket. If yes, ask where (based on available tools — Linear, GitHub Issues, etc.). Prefill the ticket with finding details (CVE, severity, dependency, repo, assessment summary).
- **Disagree** — Run `konvu finding rate <id> disagree`. Ask for an optional comment. Then offer to dismiss: `konvu dismiss --issues <id> --reason "<comment>"`.
- **Flag for deeper review** — Save the finding ID and context for Phase 3.
- **Exit triage** — Jump straight to the Summary.

When acting on a group, apply the same action to all findings in the group. For tickets, create one ticket that references all findings in the group.

When breaking down a group, present each finding individually but keep the group context visible (e.g., "Finding 2 of 4 in the lodash prototype pollution group").

### Tracking

Keep a running log of every action taken:
- Finding ID, CVE, dependency, repository
- Action: agreed / disagreed / flagged / dismissed / ticket created
- Any comments provided

## Phase 2: False Positive Findings

Fetch all false positive findings for the same period:

```bash
konvu finding list --since <period> --assessment false-positive -o json --limit 1000
```

If zero results, tell the user and move to Phase 3.

Apply the same grouping logic as Phase 1. Then per finding/group, use AskUserQuestion:

- **Agree** — Run `konvu finding rate <id> agree`. Then offer to dismiss: `konvu dismiss --issues <id> --reason "Confirmed false positive"`.
- **Disagree** — Run `konvu finding rate <id> disagree`. Ask for an optional comment. Automatically flag for deeper review in Phase 3 (the user thinks this is actually exploitable, so it needs evidence review).
- **Flag for deeper review** — Save for Phase 3.
- **Exit triage** — Jump to Summary.

## Phase 3: Deep Review

This phase covers all findings flagged during Phases 1 and 2. If nothing was flagged, skip to Summary.

Tell the user how many findings are queued for deep review. For each one, fetch full evidence:

```bash
konvu finding get <id> --include evidence -o json
```

Present the full evidence: assessment summary, checklist items with conclusions, proofs (code snippets, file paths, line numbers), and reachability analysis. Give enough context for the user to make an informed call.

Use AskUserQuestion — no more deferring:

- **Agree** — Rate and follow the same action logic as the finding's original phase (offer ticket for exploitables, offer dismiss for false positives).
- **Disagree** — Rate with comment, offer dismiss for exploitables, flag for deeper review is not available here.
- **Skip** — Move to next finding, no action taken.
- **Exit triage** — Jump to Summary.

## Summary

Always show the summary when the triage ends, whether completed or exited early.

Present two things:

**1. Counts:**
A quick overview — e.g., "Reviewed 18 findings: 12 agreed, 3 disagreed, 2 dismissed, 1 ticket created, 4 skipped, 2 remaining."

**2. Action log:**
List each finding that had an action, grouped by action type:

```
Agreed (12):
  - CVE-2024-1234 | lodash 4.17.20 | github:org/repo-a | exploitable
  - CVE-2024-1234 | lodash 4.17.20 | github:org/repo-b | exploitable
  ...

Disagreed (3):
  - CVE-2024-5678 | express 4.18.1 | github:org/api | false-positive → flagged for review
  ...

Dismissed (2):
  - CVE-2024-9999 | moment 2.29.1 | github:org/legacy | "Not used in production"
  ...

Tickets created (1):
  - CVE-2024-1234 | lodash prototype pollution | LINEAR-123
  ...

Skipped (4):
  - ...
```

If there are remaining unreviewed findings (because the user exited early), mention how many are left.

## Interaction Style

- Use **AskUserQuestion for every decision point**. The whole value of this skill is the guided, conversational flow.
- Keep finding presentations concise — CVE, severity, dependency, repo, and the one-liner assessment summary. Don't dump raw JSON at the user.
- When showing evidence in Phase 3, format it readably — use markdown headers, code blocks for proof snippets, and clear labels.
- Be conversational between phases: "That's all the exploitable findings. Moving on to false positives — there are 6 to review."
- Respect early exit gracefully — don't guilt the user, just show the summary.
