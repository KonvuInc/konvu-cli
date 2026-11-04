The direction is: **{direction}**, in {repo} ({repo_path}).

First run `konvu invariants guide` (read-only). It prints Konvu's guide to a good security rule: what one is, the right level of detail and how to rank them. Konvu's own suggestions follow the same text. It needs no login: without one it prints the copy bundled with the CLI. If it fails anyway, read that copy at `{skill_dir}/references/guide.md` and go on: writing the rule never needs an account, so don't ask the user to log in here.

Then start your message with one line: you're going through that part of the code, a couple of minutes. Then go deep on this direction only: follow the real flow end to end in the code (entry points, checks, where data or actions leave), read what the team said about it, and look outside the repo if the flow continues there, read-only.

Before drafting, run `konvu invariants list -o json` (read-only) and note each security rule's `statement` and `status`. If it fails, go on without it. Otherwise, check every version of the rule in this step against that list, yours or one the user rewords, picks or writes:
- A `validated` one is already active for the company: never use it again, reworded, narrowed or broadened. If a version the user brings matches one, tell them which in a line and offer the closest runner-up.
- A `proposed` one is a suggestion Konvu already made: if the rule says the same thing, use that suggestion's exact wording, so the upload accepts it instead of adding a near-copy. If the user wants other words, tell them in a line that theirs will be added next to Konvu's suggestion.

Draft 4 to 6 candidates within this direction, each one a security rule as the guide defines it, at the level of detail it sets (the outcome, not the mechanism: see its checks). Rank them the way the guide says, using what you found: who the customers are, what they trust the company with, the worst outcomes in `context.json` and what this repo does. Then read `{skill_dir}/references/rules.md` and keep **the single most important one**. Save it as the only entry in `{dir}/invariants.json` (format in rules.md, under Fields). Keep the runners-up in mind in case they want to swap.

If active security rules already cover what matters in this direction and only minor candidates are left, don't save one to fill the slot. Send this instead: the progress bar from `{show} progress 2`, pasted unchanged with its code fences; a bold title on its own line, **This part is already covered**; one line naming the active security rules that cover it; the other directions you offered before, one line each; then, in bold, **pick one, or tell me where to dig.** Go deep on their pick the same way, still on this step, and carry on below with the security rule you find there, naming that area instead of {direction}.

Your message, in this order, short and structured:
1. the progress bar from `{show} progress 2`, pasted unchanged with its code fences
2. a bold title on its own line, **Your first security rule**
3. one line, along the lines of: "I'm back from going through {direction}. Here's the one I believe matters most:"
4. the security rule: `{show} rules {dir_q}` prints it in a box (recommended: paste its output unchanged, code fences included)
5. one line on why this one: what it prevents, for whom
6. then, as two short lines:
   - Want to tweak it? Reword it, make it stricter or broader, or tell me where to dig.
   - We'll stick to this one for now; you can write as many as you want later.
7. the action in bold, in your own words for the moment, e.g. **When it feels right, tell me and I'll save it to Konvu.**

Nothing else: no summary of the audit, no list of other candidates unless they ask.

This is a conversation: let the user shape it as long as they want; you stay on this step. For each change, update `invariants.json` and show the security rule again the same way, with one line on what changed. Things they may ask for:
- reword it, or make it stricter or broader
- a different one: offer the runner-ups in one line each and swap if they pick one, or write theirs (check it against the guide; if it's really a code check, offer the security rule it serves)
- why it matters: two lines from what you found
- anything else about where to look or what matters: go and look, read-only, then show the sharpened one
