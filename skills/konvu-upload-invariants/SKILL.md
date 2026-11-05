---
name: konvu-upload-invariants
version: 1.1.0
description: "Save security invariants (security rules) the user wrote to Konvu with the konvu CLI: checks login (or, with an onboarding code from Konvu's web onboarding, saves without one), validates on the server with a dry run, then adds them. Use when the user wants to upload, save or push invariants or security rules to Konvu, or when konvu-invariants hands off."
metadata:
  requires:
    bins: ["konvu"]
---

# Konvu upload invariants

Adds the user's security rules to their company in Konvu, with `konvu invariants push`. The server checks everything and attributes them to the logged-in user. Konvu then maps each one to the controls in their code.

**Onboarding code.** If the user's setup message carried a Konvu onboarding code (an 8-character code of capital letters and digits, as in "My onboarding code: K7P2XQ9M"; it may come lowercase or with a dash), save with it: no Konvu login, the rule waits in the user's Konvu tab, and it moves into their account when they create it there. The code goes only into the `--onboarding` flag of the two push commands below, the one place the CLI reads it (never an environment variable): never write it to a file (not the invariants file, notes or memory), and never repeat it to the user; if you must refer to it, its first two characters are enough. Pass it as the user gave it, quoted: the CLI ignores case, spaces and dashes. No code: everything below runs as written, without `--onboarding`.

1. **The CLI.** `konvu version`. If it's missing, tell the user to install it (`curl -sSL https://raw.githubusercontent.com/KonvuInc/konvu-cli/main/scripts/install.sh | sh`, see its README) and stop.
2. **Login.** With an onboarding code, skip this step: no `konvu whoami`, no `konvu login`. Otherwise `konvu whoami`. If it fails, run `konvu login` and tell the user in one line to confirm the code in the browser window it opens; with an API key, they can run `konvu login --api-key <key>` themselves instead, so the key stays out of the chat. Never print a token.
3. **The file.** Write a temporary JSON file in exactly the push format, `{"invariants": [...]}`, 1 to 10 of them per push (split longer lists), keeping only the fields Konvu takes:
   - `statement` (required, at most 200 characters), `why_it_matters` (required, at most 500: what breaking it costs the business), `property` (required: `authentication`, `authorization`, `confidentiality`, `integrity`, `availability` or `non_repudiation`)
   - when known, `protected_assets` (at most 20 `{"kind", "name"}`, kind at most 64 characters, name at most 200: what it protects, in plain words) and `rank` (1 or more)
   - with an onboarding code, `repository`: the repo the rule is about, as `owner/name` from its git remote (`git -C <repo> remote get-url origin`; `git@github.com:acme/api.git` or `https://github.com/acme/api` gives `acme/api`), left out if it has no remote. Never `repository_id` with a code: there is no account yet.
   - without one, `repository_id` when you have it.

   Derive `why_it_matters`, `property` and `protected_assets` from the conversation and the code, and ask the user only for what you can't. Leave out anything else, like notes or evidence. With a code, Konvu keeps the rules unread until the user signs up, so the CLI checks every field first and sends nothing if one is off.
4. **Dry run.** `konvu invariants push --file <file> --dry-run -o json`, plus `--onboarding <code>` when you have one. It writes nothing. On an error, follow its `suggestion`: fix what you can (a statement too long, a missing property, each problem is named by invariant number and field) and ask the user only for what you can't. With a code, see "Onboarding code errors" below first.
5. **Confirm.** If konvu-invariants handed off, the user already agreed: go on. Otherwise show them, one line each, the rank and statement that will be added, and ask before writing.
6. **Push.** `konvu invariants push --file <file> -o json`, plus `--onboarding <code>` when you have one. Once the rules are saved, delete the temporary file.
   - With a code, each rule comes back `saved` or `unchanged` (already there). Tell the user in one line, exactly: "Saved. Go back to your Konvu tab: your rule is there, create your account to keep it."
   - Without one, a 502 "not yet mapped" means saved: run the same push once more. Report one short line per rule: added (`created`), now validated (`validated`, it was a Konvu proposal) or already there (`unchanged`), with its rank.

**Onboarding code errors** (dry run or push):
- "this onboarding already has a Konvu account": the user already created their account in the browser. Say so in one line, drop the code, run `konvu login` (step 2), then do the dry run and push again without `--onboarding`, and report as without a code.
- "unknown or has expired": say so in one line, and ask whether they want to paste a new code from their Konvu tab, or log in instead: run `konvu login` (step 2), then do the dry run and push again without `--onboarding`.
- `--onboarding` is an unknown flag: their CLI is older than these skills. Ask the user before running `konvu update`, then push again.
- Anything else: follow the error's suggestion, as in step 4.
