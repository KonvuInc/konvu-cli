---
name: recipe-investigate
version: 1.0.0
description: "Deep-dive investigation of a single CVE, GHSA, or finding. Use this skill whenever the user wants to investigate, research, look into, or understand a specific vulnerability or finding — even casual phrases like 'what's the deal with CVE-2024-1234', 'tell me about this finding', 'is this exploitable', 'deep dive on GHSA-xxxx', or 'explain this vuln'. Also trigger when the user pastes a CVE/GHSA ID or finding ID and asks what it means or whether they should care."
metadata:
  requires:
    bins: ["konvu"]
---
# Investigate Finding

> **PREREQUISITE:** Read `../konvu-shared/SKILL.md` for auth and global flags.

A single-vulnerability deep dive that gathers full context from the Konvu API and synthesizes it into a clear narrative: what is this vulnerability, how does each finding of it fare in your environment, where does it show up, and what's the fix path.

A CVE describes a theoretical weakness in a package. A *finding* is a concrete instance of that CVE in a specific repository and dependency context. Each finding is assessed independently — the same CVE can be exploitable in one repo and a false positive in another, depending on how the dependency is actually used. This skill makes that distinction clear.

## Input Detection

The user provides one of:
- **CVE ID** — matches `CVE-\d{4}-\d+` (e.g., `CVE-2024-1234`)
- **GHSA ID** — matches `GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}` (e.g., `GHSA-abcd-efgh-ijkl`)
- **Finding ID** — anything else (typically a UUID or short hash)

If the user's message is ambiguous, ask which finding or vulnerability they mean before proceeding.

## Data Gathering

The investigation needs three things: the vulnerability context, the detailed finding evidence, and the blast radius. The commands to run depend on what the user gave you.

### Starting from a CVE or GHSA ID

Run these two commands in parallel:

```bash
# Vulnerability context
konvu vuln get <ID> --include summary,technical,exploitability,remediation,references,affected -o json

# All findings for this vulnerability
konvu finding list --cve <ID> -o json --limit 1000
```

Then fetch full evidence for findings. If all findings share the same assessment, one representative is enough. If findings have **different assessments**, fetch evidence for one representative from each assessment category — the reasoning will differ and the user needs to see why.

```bash
konvu finding get <finding-id> --include evidence --include logs -o json
```

### Starting from a Finding ID

Fetch the finding first, then fan out:

```bash
konvu finding get <finding-id> --include evidence --include logs -o json
```

Extract the CVE or GHSA ID from the response (look in `vulnerability.id` or `vulnerability.aliases`). Then run both in parallel:

```bash
konvu vuln get <CVE-or-GHSA> --include summary,technical,exploitability,remediation,references,affected -o json
konvu finding list --cve <CVE-or-GHSA> -o json --limit 1000
```

If other findings have different assessments from the one the user asked about, fetch a representative from each different assessment to show the contrast.

### Error handling

- **Exit code 4 (auth):** Tell the user to run `konvu login` and stop.
- **Exit code 3 (not found):** Tell the user the ID wasn't found. Suggest checking the ID or running `konvu finding list` to browse.
- **No CVE/GHSA on the finding:** Skip the `vuln get` and `finding list --cve` calls. Note in the report that there's no linked vulnerability advisory.

## Report Structure

Present the investigation as four sections. Use compact tables (like the triage skill) over prose where possible. Never dump raw JSON.

### 1. The Vulnerability

What the CVE/GHSA describes — purely the vulnerability itself, independent of any finding's exploitability assessment. This section is about the theoretical risk.

```
🔍 CVE-2024-1234 — lodash prototype pollution

| Field    | Value                                          |
|----------|------------------------------------------------|
| Severity | HIGH                                           |
| CVSS     | 7.5                                            |
| EPSS     | 0.42 (82nd percentile)                         |
| Package  | lodash < 4.17.21                               |
| Aliases  | GHSA-abcd-efgh-ijkl                            |

Prototype pollution vulnerability in lodash before 4.17.21. The merge,
mergeWith, and defaultsDeep functions allow attackers to modify Object
prototype properties via crafted input.
```

Include CVSS and EPSS when available — they help gauge urgency. If technical details are available from the `--include technical` response, include a brief explanation of the attack mechanism.

### 2. Your Findings

This is the core value. Each finding has its own assessment based on its specific context — how the dependency is used in that repo, what code paths are reachable, what runtime protections exist.

Present findings grouped by assessment. For each group, show the conclusion from the representative finding's checklist and evidence.

**When you started from a specific finding ID**, lead with that finding's analysis, then show others if they exist.

**Checklist presentation:** Each checklist item has a description (the question Konvu investigated) and a conclusion (what was found). Show the conclusion — that's what matters. If evidence includes proof snippets (file paths, code), include them.

**When all findings share the same assessment:**

```
📋 All 4 findings assessed as EXPLOITABLE

Konvu's analysis (based on org/app-a, representative of all 4):

  Vulnerability applicable to dependency stack
  → lodash 4.17.20 is a direct dependency, affected version range confirmed.

  Exploitable code path exists
  → lodash.merge() called in request middleware with unsanitized user objects.
    src/middleware/parse.js:42
    `const merged = _.merge({}, defaults, req.body)`

  Runtime protections
  → No Object.freeze() or input sanitization before merge call.
```

**When findings have different assessments:**

```
📋 4 findings — 3 exploitable, 1 false-positive

Assessments differ because each finding is evaluated based on how the
dependency is actually used in its repo.

── Exploitable: org/app-a, org/app-b, org/web ──

Konvu's analysis (based on org/app-a):

  Vulnerability applicable to dependency stack
  → lodash 4.17.20 is a direct dependency, affected version range confirmed.

  Exploitable code path exists
  → src/middleware/parse.js:42 — _.merge({}, defaults, req.body)

  Runtime protections
  → No Object.freeze() or input sanitization before merge call.

── False positive: org/api ──

Konvu's analysis:

  Vulnerability applicable to dependency stack
  → lodash 4.17.21 is a direct dependency, affected version range confirmed.

  Exploitable code path exists
  → No calls to merge, mergeWith, or defaultsDeep found in codebase.
    Only _.get() and _.isEmpty() are used.
```

The key is showing *why* the same CVE leads to different conclusions in different contexts. This is the most valuable insight Konvu provides.

**Assessment history** — If recommendation history is available from `--include logs`, add a brief note:

```
Assessment history: Initially inconclusive (2024-11-15), upgraded to
exploitable (2024-11-18) after reachability analysis confirmed code path.
```

### 3. Blast Radius

Summary table of all findings from `finding list --cve`.

```
💥 Blast Radius: 4 findings across 3 repositories

| Repo       | Dependency     | Assessment   | Fix       |
|------------|----------------|--------------|-----------|
| org/app-a  | lodash 4.17.20 | exploitable  | available |
| org/app-b  | lodash 4.17.20 | exploitable  | available |
| org/api    | lodash 4.17.21 | false-pos    | available |
| org/web    | lodash 4.17.20 | exploitable  | available |
```

If there's only one finding, say so — "This vulnerability appears in only one repository."

### 4. Fix Path

Remediation from `vuln get --include remediation` and the finding details.

```
🔧 Fix Path

Fix available: upgrade lodash to >= 4.17.21

Needs attention (exploitable):
  - org/app-a: package.json
  - org/app-b: package-lock.json
  - org/web: yarn.lock

Can deprioritize (false-positive):
  - org/api: package.json
```

Split remediation by assessment so the user knows what's urgent vs. what can wait. If no fix is available, say so explicitly and suggest workarounds if the vulnerability context provides any.

## Next Steps

After the report, offer actionable follow-ups using AskUserQuestion. Pick the most natural one based on the situation — one question, not a menu:

- **Rate findings** — "Do you agree with Konvu's assessment on these findings?" → `konvu finding rate <id> agree/disagree` for each finding
- **Dismiss false positives** — If there are false positives: "Want to dismiss the false-positive findings?" → `konvu dismiss --issues <ids>`
- **Drill into a specific finding** — When assessments differ: "Want to see the full evidence for a specific finding?"
- **Investigate another** — "Want to look into another vulnerability?"

Heuristic for which to offer:
- All exploitable → suggest rating
- Mixed assessments → offer to drill into a specific finding or rate in bulk
- False positives → suggest dismissing
- User started from a specific finding → suggest rating that finding

## Interaction Style

- **Never dump raw JSON.** Always synthesize into the report structure above.
- **Assessment reasoning is mandatory.** The "Your Findings" section is the whole point — never skip it.
- **Assessments belong to findings, not CVEs.** Always make it clear which finding(s) an assessment applies to and why.
- **Compact tables over prose.** Match the triage skill's table-first approach.
- **One vulnerability at a time.** This skill investigates one CVE/finding deeply. If the user wants to review many findings, point them to the triage skill instead.
- **Be direct about gaps.** If data is missing (no EPSS, no fix, no evidence), say so rather than omitting the section.
