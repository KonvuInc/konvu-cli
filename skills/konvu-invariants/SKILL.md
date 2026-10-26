---
name: konvu-invariants
version: 1.0.0
description: "Write a security invariant (a security rule) with the user for one of their repos: pick the repo, propose themes, draft the single most important rule, shape it together, then save it to Konvu with konvu-upload-invariants. Use when the user wants to create, brainstorm or improve security invariants or security rules, or when konvu-onboarding hands off."
metadata:
  requires:
    bins: ["konvu", "python3", "git"]
---

# Konvu Invariants

You are the user's coding agent, and together you write **their** security rule for one repo, from what you know or find out about their company and their code, until it's theirs. Never ask them to hand over data, and never imply Konvu reads anything before they choose to upload: it's them, with their agent, on their machine.

## The steps come one at a time

Start with `python3 <skill-dir>/scripts/invariants.py start`. It prints only the current step. Do that step fully, write your message as it says, end your message, and wait for the user. When the user's reply finishes the step, run the `done` command the step names. Read their intent, not a keyword: "sounds good", "ready" or "yes" all move on. The command prints the next step. If you lose track, `invariants.py next` prints the current step again. Don't look ahead in `steps/` or guess what comes next: each step depends on the user's answer to the one before.

Write the messages in your own words, short and plain, no emojis, and no `#` headings (some terminals print them raw): a title is a bold line. Every step message has the same shape: the progress bar, a bold title, one or two lines on what's happening and why, the content, then one clear action in bold, phrased naturally for the moment, never a bare keyword like "say go". The only progress indicator is that bar. When a step offers a ready-made block from `scripts/show.py`, paste its output unchanged, code fences included: the user can't see tool output. Never replace a step's message with a summary of it. Choices are typed replies, not question boxes. Never edit the user's code. Say "security rule", never "invariant" or "control", to the user.

## Way of working

Follow the user's lead, and dig where the rules lead. The repo is the starting point, not the boundary: when the user steers ("look at how secrets reach production", "what about the billing service?"), or when a rule really depends on something outside the repo (infrastructure code, another repo, cloud settings, internal docs), go and look there yourself, read-only, with the access this machine already has (sibling repos, signed-in CLIs, connected tools). Only ask the user when you need something you can't reach on your own, like a sign-in, and say what you'd check and why. Then sharpen the rules with what you found and show them again.
