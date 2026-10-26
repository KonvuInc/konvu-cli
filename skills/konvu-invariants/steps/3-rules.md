The direction is: **{direction}**, in {repo} ({repo_path}).

Start your message with one line: you're going through that part of the code, a couple of minutes. Then go deep on this direction only: follow the real flow end to end in the code (entry points, checks, where data or actions leave), read what the team said about it, and look outside the repo if the flow continues there, read-only.

Read `{skill_dir}/references/rules.md`: it defines a security rule, the right level of detail, how to rank candidates, and the fields to fill. Draft 4 to 6 candidates within this direction, each written one level above the code you found (the outcome, not the mechanism: see the checks in rules.md), then keep **the single most important one**: the one whose break would hurt most, given who the customers are, what they trust the company with, the worst outcomes in `context.json`, what this repo does, and the direction the user picked. Save it as the only entry in `{dir}/invariants.json` (format in `{skill_dir}/references/rules.md`, under Fields). Keep the runners-up in mind in case they want to swap.

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
- a different one: offer the runner-ups in one line each and swap if they pick one, or write theirs (check it against the definition; if it's really a code check, offer the security rule it serves)
- why it matters: two lines from what you found
- anything else about where to look or what matters: go and look, read-only, then show the sharpened one
