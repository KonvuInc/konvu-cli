---
name: guardrails-onboarding
version: 1.0.0
description: "Set up Konvu Guardrails on a repository so its pull requests are checked for access-control changes. Use this skill whenever the user wants to start checking who can do what in their code — even if they say it casually like 'set up guardrails', 'onboard this repo', 'check my pull requests for auth changes', 'protect my routes', 'stop people breaking permissions', or 'why did guardrails flag my PR'. Also use it when a user asks what access rules a repository has, or wants to approve, review or turn checking off."
metadata:
  requires:
    bins: ["konvu"]
---
# Guardrails Onboarding

> **PREREQUISITE:** Read `../konvu-shared/SKILL.md` for auth and global flags.

Guardrails reads the access control your code already enforces — who may do what — turns it into rules, and once the user approves them, checks every pull request against them.

Five steps, in order. Nothing is checked until all five are done.

```
Connect → Scan → Review → Approve → Switch on
```

## Progress Bar

Every message starts with the pipeline, with the active step in brackets.

```
✓ Connect → ✓ Scan → [Review] → Approve → Switch on
                        ↳ 36 rules drafted · reading them with the user
```

## Step 1: Connect

Once per GitHub organization.

```bash
konvu guardrails connect
```

The organization defaults to the owner of the `origin` remote; pass `--org acme` from outside a checkout.

Three outcomes:
- **Not installed** — prints an install link. Tell the user to open it, then run the command again.
- **Connected, repo not visible** — prints the repository-selection page and waits, polling until GitHub reports the repository. Tell the user to tick the repository and save; the command notices by itself.
- **Connected** — move on.

Connecting an organization does **not** start checking anything. Say so if the user assumes it does.

## Step 2: Scan

```bash
konvu guardrails scan --wait              # the repo you are in
konvu guardrails scan ../web --wait       # another checkout
konvu guardrails scan --remote acme/web   # no checkout needed
```

This reads the code with a model and takes minutes. Say that before starting so the wait is expected.

Report what comes back — routes analyzed, how many restrict access, rules drafted — and any notes. A note naming routes that bind no object key is worth repeating to the user: those routes are seen but cannot earn credit for restricting *which* record they touch.

If a repository comes back with nothing to check, it has no access control the tool can see. That is an answer, not a failure. Stop and say so.

## Step 3: Review the rules with the user

**Do not skip this step, and do not approve on the user's behalf without showing them the rules first.**

```bash
konvu guardrails show acme/web
```

The rules describe **what the code does today**. If a route is missing a permission check, the drafted rule says that access is intended. Approving it makes that the standard, and a later change removing more checks would then look fine.

So present the rules as a question, not a formality:

> "These are the rules read from your code. Anything here that should *not* be allowed is a bug worth fixing before approving — approving says this is what you intend."

Walk through them in groups by resource. Draw attention to any rule where the condition column is empty, permissive, or grants access to an anonymous or unauthenticated role — those are the ones worth a second look.

## Step 4: Approve

Approving says "these rules are what I intend", which is what lets a later change be flagged for breaking one.

```bash
konvu guardrails approve acme/web                     # all of them
konvu guardrails approve acme/web --rule "<key>"      # one at a time
```

Rule keys come from `show`, and must be copied exactly:

```bash
konvu guardrails show acme/web -o json | jq -r '.policy[].key'
```

Never invent or reconstruct a key — a key that matches nothing is refused (exit 2), which is the good case. Never pass a shell variable you have not checked is non-empty.

**Checks stay paused until every rule is approved.** A rule nobody has approved counts as access the user forbids, so starting earlier would fail pull requests over rules still being read. `approve` reports how many are still drafted; keep going until it says checks are running.

Use `--rule` when the list is long enough to work through in sessions, and tell the user where they got to.

## Step 5: Switch checking on

```bash
konvu guardrails enable acme/web    # or bare, for every approved repository
```

A repository without approved rules is refused by name — that means step 4 is not finished.

Two things to tell the user once this succeeds, because both surprise people:

- **Open pull requests are checked on their next push.** Nothing is checked retroactively.
- **A check never blocks a merge.** Whether a failing check stops a merge is the repository's branch protection setting, which Konvu does not change. If the user wants it blocking, they add it there themselves.

## After onboarding

### Where things stand

```bash
konvu guardrails list
```

`Approved` and `Checking` are different columns and different questions — rules can be approved on a repository whose pull requests nothing is looking at yet.

Anything the table cannot show is printed underneath: repositories with no access control, ones that failed to scan, and ones with no scan that are still switched on.

### A pull request got flagged

```bash
konvu guardrails explain <token>     # token is in the check's comment on the PR
```

Findings come in two kinds and the output groups them:

- **BREAKS A RULE YOU APPROVED** — the code no longer does what the approved rules say. Fix the code, or take the rule back if it was wrong.
- **NEW ACCESS — NEEDS YOUR APPROVAL** — the code does something no rule covers. Approve it, or change the code.

For each finding the output names the route, what it checks now, the file and line, and what similar routes do. Use that last one: matching the app's own bar is usually the right fix, rather than inventing a new check.

To record what a route *should* enforce, in the user's own words:

```bash
konvu guardrails explain <token> --intent "only the document's owner may read it"
```

Nothing is applied to the rules by that. The check re-runs on the next push and clears itself if the code matches.

To decide on new access:

```bash
konvu guardrails review acme/web --pr 412
konvu guardrails review acme/web --pr 412 --allow "<key>"
konvu guardrails review acme/web --pr 412 --deny  "<key>"
```

Keys are printed by `review` itself and must be passed back exactly. Decisions apply when the pull request merges, and `--clear` removes one beforehand.

### Undoing

```bash
konvu guardrails unapprove acme/web --rule "<key>"   # one rule
konvu guardrails unapprove acme/web                  # all → back to a draft
konvu guardrails enable --off acme/web               # stop checking
konvu guardrails delete acme/web --all-branches      # remove the scan
```

Taking back one rule leaves checking on; that rule becomes access the user no longer intends, so a pull request relying on it is flagged. Taking back all of them pauses checks.

`delete` does **not** switch the repository off. It says so, and `list` says so afterwards — a repository with no scan that is still switched on gets a check on every pull request that can only report it has nothing to judge against. Offer `enable --off` alongside.

## When something goes wrong

| Exit | Meaning | What to do |
|---|---|---|
| 2 | bad arguments | Usually a rule key that matches nothing. Re-read it from `show`. |
| 3 | not found | The repository or branch has no scan. `list` shows what exists. |
| 4 | auth | Tell the user to run `konvu login`. Do not retry. |

A scan can fail or come back with far fewer routes than a previous run on the same code. Report the number you got rather than the number you expected, and offer to scan again — do not present a low count as the repository's true size.

## Interaction Style

- **Progress bar on every message.** Never skip it.
- **Never approve without showing the rules first.** Approving is the user's statement of intent, not a setup step to get past.
- **Copy rule keys, never reconstruct them.**
- **Say the wait is coming** before a scan, and report real numbers after.
- **Say what a step does not do**: connecting does not start checking, approving does not block merges, deleting does not switch off.
- **Plain words.** Say "rules", "access", "who may do what" — not policy, baseline, or capability.
- **Never dump raw JSON.** Format it.
- **Stop on exit code 4.** Auth failures are for the user to fix.
