---
name: recipe-posture
version: 1.0.0
description: "Security posture overview that interprets metrics into a narrative. Use this skill whenever the user wants a security overview, posture summary, dashboard, status report, or asks questions like 'how are we doing on security', 'give me the big picture', 'security summary', 'what's our risk', 'posture check', 'how many vulns do we have', or 'weekly security update'. Also trigger when the user asks about trends, backlog growth, or wants to understand their overall security position — even casually."
metadata:
  requires:
    bins: ["konvu"]
---
# Security Posture

> **PREREQUISITE:** Read `../konvu-shared/SKILL.md` for auth and global flags.

A posture overview that orchestrates multiple Konvu CLI commands and synthesizes the raw numbers into an interpreted narrative. The value is not the data — the user can run the commands themselves. The value is the analysis: what's improving, what's getting worse, where is risk concentrated, and what should they pay attention to.

## Data Gathering

Run all four commands in parallel. Use `-o json` for all of them — the skill does the formatting.

```bash
# Overall metrics with weekly trends, top CVEs, and new vs closed
konvu metrics show --include summary,trends,top_cves,new_vs_closed --interval week -o json

# Severity breakdown with assessment counts
konvu finding counts --group-by severity -o json

# Weekly assessment trend (4 weeks)
konvu finding counts --group-by week -o json

# Exploitable findings by repository
konvu finding list --assessment exploitable --group-by repository -o json --limit 1000
```

If any command fails with exit code 4 (auth), tell the user to run `konvu login` and stop.

## Interpretation

Before presenting the report, analyze the data. This is the skill's core job — turning numbers into insight.

**Compute these derived values from the trends data:**

- **Week-over-week delta** for exploitable count: compare the last two `to_fix` data points. Express as absolute change and percentage.
- **Month-over-month delta**: compare the latest `to_fix` data point to the one ~4 weeks earlier. This is the default comparison frame.
- **Backlog trajectory**: is total_open growing, shrinking, or flat? Look at the slope over the last 4 weeks, not just the last two points (which could be noise).
- **Resolution velocity**: from `new_vs_closed`, compare the most recent week's new vs closed. Are they closing faster than new findings arrive? Compute net change.
- **Risk concentration**: from the repo grouping, compute what percentage of exploitable findings the top 3 repos account for. If one repo dominates, that's a key insight.
- **Severity concentration**: from the severity breakdown, flag if critical+high exploitable findings make up a disproportionate share.

Not all of these will be interesting every time. Only include insights that are actually meaningful — if the backlog is flat, saying "backlog is flat" is less useful than saying nothing. Focus on what's changing, what's surprising, and what needs attention.

## Report Structure

### 1. Executive Summary

A 2-3 sentence narrative lead that captures the overall posture. This should read like a briefing, not a data dump. Adapt the tone to what the data shows.

```
📊 Security Posture — March 12, 2026

You have 461 exploitable findings across 25 repositories, up from 453
last week (+1.8%) and up from 290 a month ago (+59%). The backlog has
been growing steadily — new findings are outpacing closures. juice-shop,
webgoat, and pygoat account for 55% of your exploitable risk.
```

The summary should always include:
- Current exploitable count (this is the number that matters most)
- Direction and magnitude of change (week-over-week and month-over-month)
- The single most important insight (risk concentration, trend acceleration, or a bright spot)

### 2. Trend

Show the weekly progression as a compact table. Include both the backlog totals and the assessment breakdown.

```
📈 Trend (weekly)

| Week       | Total Open | Exploitable | False Positive | New | Closed | Net   |
|------------|------------|-------------|----------------|-----|--------|-------|
| Mar 9      | 8,034      | 461         | 1,926          | 15  | 18     | -3    |
| Mar 2      | 7,950      | 458         | 1,905          | 230 | 16     | +214  |
| Feb 23     | 7,683      | 453         | 1,855          | 311 | 679    | -368  |
| Feb 16     | 7,269      | 443         | 1,806          | 695 | 59     | +636  |
```

After the table, add a one-line interpretation: "Backlog grew 10.5% over the past month. Last week was a good week — more findings closed than opened for the first time in 4 weeks."

Merge `trends` data (backlog, to_fix, to_dismiss) with `new_vs_closed` by matching on date. Show the 4–5 most recent weeks.

### 3. Risk Concentration

Where are the exploitable findings? Show the top repos and what percentage of total exploitable risk they represent.

```
🎯 Risk Concentration

| Repository                  | Exploitable | % of Total |
|-----------------------------|-------------|------------|
| acmekonvu/juice-shop        | 33          | 7.2%       |
| konvudemo/webgoat           | 32          | 6.9%       |
| testpaul1/webgoat           | 32          | 6.9%       |
| acmekonvu/webgoat           | 31          | 6.7%       |
| testpaul1/webgoat2          | 29          | 6.3%       |
| (20 other repos)            | 304         | 65.9%      |

Top 5 repos account for 34.1% of exploitable findings.
```

Show the top 5 repos (or fewer if the concentration is very high). Roll up the rest into an "other" row. Add a one-line interpretation about how concentrated or spread out the risk is.

### 4. Severity Breakdown

Show findings by severity, but focus the narrative on the exploitable ones — that's what the user should act on.

```
⚠️ Severity Breakdown

| Severity | Exploitable | False Positive | Not Assessed | Total |
|----------|-------------|----------------|--------------|-------|
| Critical | 14          | 159            | 603          | 776   |
| High     | 133         | 554            | 1,970        | 2,658 |
| Moderate | 111         | 494            | 1,243        | 1,853 |
| Low      | 12          | 146            | 285          | 445   |

14 critical exploitable findings need immediate attention. 4,101 findings
are not yet assessed — Konvu is still analyzing them.
```

Highlight anything notable: lots of not-assessed (Konvu is still catching up), critical exploitables (urgent), or a high false-positive rate (Konvu is saving you work).

### 5. Top CVEs to Prioritize

From the `top_cves` data. Keep it brief — the investigate skill is for deep dives.

```
🔝 Top CVEs to Prioritize

1. CVE-2025-49794
2. CVE-2020-1747
3. CVE-2025-65482
```

If the user wants to know more about any of these, they can use the investigate skill.

## Next Steps

After the report, offer follow-ups using AskUserQuestion. Pick the most relevant one:

- **Generate a report** — "Want me to generate a shareable report (Markdown or PDF)?" → hand off to the `recipe-posture-report` skill (coming soon)
- **Investigate a top CVE** — "Want to deep-dive into one of the top CVEs?" → hand off to the investigate skill
- **Triage recent findings** — "Want to triage this week's findings?" → hand off to the triage skill
- **Drill into a repo** — If one repo dominates: "acmekonvu/juice-shop has the most exploitable findings. Want to see what's in there?"

Heuristic:
- Backlog growing fast → suggest triage
- High concentration in one repo → suggest drilling into that repo
- User seems executive-level → suggest generating a report
- Default → suggest investigating the top CVE

## Interaction Style

- **Interpret, don't just report.** Every table should have a sentence explaining what it means. "461 exploitable" is data; "461 exploitable, up 59% in a month with no signs of slowing" is insight.
- **Lead with the story.** The executive summary comes first and should be the most carefully crafted part. If the user only reads one paragraph, it should be that one.
- **Keep it copy-paste friendly.** Use markdown tables, no ANSI colors. The report should look good pasted into Slack, a doc, or an email.
- **Numbers in context.** Raw numbers mean nothing without comparison. Always pair a number with its trend or proportion: "14 critical" → "14 critical, up from 10 last month."
- **Be honest about gaps.** If there are lots of not-assessed findings, say so — the numbers will change as Konvu finishes analysis. If new_vs_closed shows a spike, note that it might be a one-time import rather than a trend.
- **One report, not a conversation.** Unlike the triage skill, this is a one-shot output. Present the full report, then offer follow-ups. Don't ask questions before showing the data.
