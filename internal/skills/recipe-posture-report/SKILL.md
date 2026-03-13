---
name: recipe-posture-report
version: 1.0.0
description: "Generate an audit-ready security posture report as Markdown (or PDF if possible). Use this skill when the user wants to generate, export, or share a security report — phrases like 'generate a report', 'security report', 'audit report', 'board deck', 'export posture', 'share security status', 'write up our security posture', or when the posture skill offers a report follow-up. Also trigger when the user asks for something they can hand to an auditor, share with leadership, or attach to a compliance document."
metadata:
  requires:
    bins: ["konvu"]
---
# Security Posture Report

> **PREREQUISITE:** Read `../konvu-shared/SKILL.md` for auth and global flags.

Generates an audit-ready security posture report. The skill's value is formatting and narrative — turning CLI output into something you hand to an auditor, drop in a board deck, or attach to a compliance submission.

If the posture skill (`recipe-posture`) was already run in this conversation, use its data as the foundation and fetch only the additional data needed for the report (dismissed findings, fixed findings). If called standalone, run the posture skill's data gathering first.

## Data Gathering

### From the posture skill (or re-fetch if needed)

These are the same commands the posture skill runs. If the data is already in context, skip them.

```bash
konvu metrics show --include summary,trends,top_cves,new_vs_closed --interval week -o json
konvu finding counts --group-by severity -o json
konvu finding list --assessment exploitable --group-by repository -o json --limit 1000
```

### Additional data for the report

These commands fetch the audit trail — actions taken and their justifications. Run in parallel.

```bash
# Dismissed findings with justifications (last 30 days)
konvu finding list --state dismissed --since 30d -o json --limit 1000

# Fixed findings (last 30 days)
konvu finding list --state fixed --since 30d -o json --limit 1000

# Dismissed findings by repo (for the actions summary)
konvu finding list --state dismissed --since 30d --group-by repository -o json --limit 1000

# Total dismissed count (all time, for context)
konvu finding list --state dismissed --count -o json
```

If any command fails with exit code 4 (auth), tell the user to run `konvu login` and stop.

## Report Generation

### Ask the user

Before generating, ask via AskUserQuestion:
- **Report period** — Default: last 30 days. "What time period should the report cover?"
- **Format** — "Markdown file, or PDF if you have a PDF tool available?" Default to Markdown.
- **File name** — Suggest a sensible default like `security-posture-report-2026-03-12.md`

### Output format

Write the report as a Markdown file. If the user wants PDF and `pandoc` is available, convert:

```bash
pandoc report.md -o report.pdf --pdf-engine=xelatex
```

If `pandoc` isn't available, write the Markdown file and let the user know they can convert it themselves or use any Markdown-to-PDF tool.

## Report Template

The report should follow this structure. Adapt the content based on what the data shows — these sections are the skeleton, but the narrative should be specific and data-driven.

```markdown
# Security Posture Report

**Organization:** [from konvu whoami if available]
**Period:** [start date] — [end date]
**Generated:** [today's date]

---

## Executive Summary

[2-3 paragraph narrative summarizing the security posture. Lead with the
headline number (exploitable findings), the trend (improving/worsening),
and the single most important insight. This should stand on its own —
if someone reads only this section, they should understand the situation.]

Key metrics:
- **Total open findings:** X
- **Exploitable:** X (Y% of total)
- **False positives identified:** X
- **Findings resolved this period:** X fixed, X dismissed
- **Net change:** +/-X findings

---

## Open Findings by Severity

| Severity | Exploitable | False Positive | Not Assessed | Total |
|----------|-------------|----------------|--------------|-------|
| Critical | ...         | ...            | ...          | ...   |
| High     | ...         | ...            | ...          | ...   |
| Moderate | ...         | ...            | ...          | ...   |
| Low      | ...         | ...            | ...          | ...   |
| **Total**| ...         | ...            | ...          | ...   |

[1-2 sentences interpreting: where is the critical risk, what proportion
is actually exploitable vs noise]

---

## Trend

| Week       | Total Open | Exploitable | New  | Closed | Net    |
|------------|------------|-------------|------|--------|--------|
| ...        | ...        | ...         | ...  | ...    | ...    |

[Interpretation: is the backlog growing or shrinking? Are we closing
faster than new findings arrive?]

---

## Risk Concentration

| Repository              | Exploitable | % of Total |
|-------------------------|-------------|------------|
| ...                     | ...         | ...        |

[Which repos carry the most risk and what that implies for remediation
prioritization]

---

## Actions Taken

### Findings Resolved

- **Fixed:** X findings remediated through code changes or dependency upgrades
- **Dismissed:** X findings dismissed after review

### Dismissed Findings

Dismissed findings with assessment justification, grouped by repository.
This section provides the audit trail for why findings were closed without
a code fix.

[For each repo with dismissed findings, show a table:]

#### [repo name] — X dismissed

| CVE | Dependency | Severity | Justification |
|-----|------------|----------|---------------|
| ... | ...        | ...      | ...           |

[The justification comes from the assessment_summary field. For generic
summaries like "Not exploitable in your context", fetch the full finding
via `konvu finding get <id> --include evidence -o json` and use the
checklist conclusion instead — the same approach the triage skill uses.]

---

## Top Vulnerabilities to Prioritize

| # | CVE | Recommendation |
|---|-----|----------------|
| 1 | ... | ...            |
| 2 | ... | ...            |
| 3 | ... | ...            |

[Brief note on why these are prioritized]

---

## Appendix: Not Assessed Findings

[If there's a large number of not-assessed findings, note it here with
context: Konvu is still analyzing them, and the numbers above may shift
as analysis completes.]
```

## Writing the Report

When filling in the template:

- **Executive summary is the most important section.** Spend the most effort here. It should tell a story, not list bullet points. "Over the past 30 days, the total backlog grew from 5,335 to 8,034 findings (+50.6%), driven primarily by onboarding new repositories in February. However, exploitable findings grew more slowly (from 290 to 461, +59%), and Konvu identified 1,926 false positives — meaning 24% of the backlog has been automatically deprioritized."

- **Dismissed findings need real justifications.** This is an audit document. "Not exploitable in your context" is not a sufficient justification for an auditor. When you encounter generic assessment summaries, fetch the full evidence for a representative finding per group (same CVE + same repo) to get the actual reasoning. Group dismissed findings by repo to make the audit trail scannable.

- **Numbers always in context.** Never just state a number — pair it with a comparison, proportion, or trend. "14 critical exploitable" → "14 critical exploitable findings, representing 3% of the exploitable backlog."

- **Be honest about limitations.** If there are many not-assessed findings, note that the picture is incomplete. If the reporting period includes a big onboarding event (spike in new findings), call it out so the reader doesn't misinterpret the trend.

- **Keep it factual.** This is going to auditors and leadership. No speculation, no hedging language like "might" or "could potentially." State what the data shows.

## After Generation

Tell the user where the file was saved and its size. Offer follow-ups:

- **Review and edit** — "Want to review it before sharing? I can adjust any section."
- **Investigate a top CVE** — "Want to deep-dive into any of the top CVEs before finalizing?"
- **Triage open findings** — "There are X not-yet-triaged findings. Want to triage before the next report?"
