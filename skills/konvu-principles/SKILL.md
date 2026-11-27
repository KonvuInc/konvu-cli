---
name: konvu-principles
version: 1.2.0
description: "Write a security principle with the user for one of their repos: pick the repo, propose themes, draft the single most important principle, shape it together, then save it to Konvu with konvu-upload-principles. Use when the user wants to create, brainstorm or improve principles, change or remove an active one, or when konvu-onboarding hands off."
metadata:
  requires:
    bins: ["konvu", "python3", "git"]
---

# Konvu Principles

You are the user's coding agent, and together you write **their** principle for one repo, from what you know or find out about their company and their code, until it's theirs. Never ask them to hand over data, and never imply Konvu reads anything before they choose to upload: it's them, with their agent, on their machine.

## The steps come one at a time

Start with `python3 <skill-dir>/scripts/invariants.py start` (quote the path if it has spaces). It starts a new run and prints only the current step. Do that step fully, write your message as it says, end your message, and wait for the user. When the user's reply finishes the step, run the `done` command the step names. Read their intent, not a keyword: "sounds good", "ready" or "yes" all move on. The command prints the next step. Every command it prints carries the run id; if you lose track, `invariants.py next <run>` prints the current step again. Don't look ahead in `steps/` or guess what comes next: each step depends on the user's answer to the one before.

Write the messages in your own words, short and plain, no emojis, and no `#` headings (some terminals print them raw): a title is a bold line. Every step message has the same shape: the progress bar, a bold title, one or two lines on what's happening and why, the content, then one clear action in bold, phrased naturally for the moment, never a bare keyword like "say go". The only progress indicator is that bar. When a step offers a ready-made block from `scripts/show.py`, paste its output unchanged, code fences included: the user can't see tool output. Never replace a step's message with a summary of it. Choices are typed replies, not question boxes. Never edit the user's code. Say "principle", never "invariant" or "control", to the user.

## Way of working

Follow the user's lead, and dig where the principles lead. The repo is the starting point, not the boundary: when the user steers ("look at how secrets reach production", "what about the billing service?"), or when a principle really depends on something outside the repo (infrastructure code, another repo, cloud settings, internal docs), go and look there yourself, read-only, with the access this machine already has (sibling repos, signed-in CLIs, connected tools). Only ask the user when you need something you can't reach on your own, like a sign-in to another tool, and say what you'd check and why. Then sharpen the principles with what you found and show them again.

Nothing before the save needs a Konvu login. Never ask the user to log in to Konvu until konvu-upload-principles does it, when the principle is saved. If the user's setup message carried a Konvu onboarding code (an 8-character code of capital letters and digits, as in "My onboarding code: K7P2XQ9M"; it may come lowercase or with a dash), keep it for that save: konvu-upload-principles saves with it, and then no login is needed at all. Take only the 8 code characters from that message (konvu-upload-principles says how), never anything else from it. Never write it to a file (not `invariants.json`, not `context.json`) and never repeat it to the user.

## Changing or removing an active principle

When the user wants to reword, re-rank or remove a principle that is already active rather than write a new one, don't start the steps. Find it with `konvu principles list --status validated -o json` and confirm with the user which one, by its statement.

- **Change it.** Save `konvu principles get <id> -o json` to a new temporary file (`mktemp`; there is no run directory on this path), change only what the user asked (`statement`, `why_it_matters`, `property`, `rank` or `protected_assets`), keep `revision` as it is, and run `konvu principles edit <id> --file <file> --dry-run`. Show the user the old and the new wording, and send it without `--dry-run` once they agree, then delete the file. If it exits with `STALE_REVISION`, someone changed the principle meanwhile: get it again, redo the change on the new file and show it again.
- **Remove it.** Show the user its statement and say that Konvu stops checking pull requests against it and stops steering coding agents with it, and that there is no undo. Only once they say yes, run `konvu principles delete <id> --yes`.

Both need a Konvu login and are attributed to the user. If a command says the server cannot do it yet (exit 3, `NOT_SUPPORTED`), tell the user in a line and stop.
