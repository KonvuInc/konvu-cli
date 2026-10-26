---
name: konvu-upload-invariants
version: 1.0.0
description: "Save security invariants (security rules) the user wrote to Konvu with the konvu CLI: checks login, validates on the server with a dry run, then adds them. Use when the user wants to upload, save or push invariants or security rules to Konvu, or when konvu-invariants hands off."
metadata:
  requires:
    bins: ["konvu"]
---

# Konvu upload invariants

Adds the user's security rules to their company in Konvu, with `konvu invariants push`. The server checks everything and attributes them to the logged-in user. Konvu then maps each one to the controls in their code.

1. **The CLI.** `konvu version`. If it's missing, tell the user to install it (`curl -sSL https://raw.githubusercontent.com/KonvuInc/konvu-cli/main/scripts/install.sh | sh`, see its README) and stop.
2. **Login.** `konvu whoami`. If it fails, run `konvu login` and tell the user in one line to confirm the code in the browser window it opens; with an API key, they can run `konvu login --api-key <key>` themselves instead, so the key stays out of the chat. Never print a token.
3. **The file.** Write a temporary JSON file `{"invariants": [...]}`, 1 to 10 of them per push (split longer lists), keeping only the fields Konvu takes: `statement` (at most 200 characters), `why_it_matters` (at most 500), `property` (`authentication`, `authorization`, `confidentiality`, `integrity`, `availability` or `non_repudiation`), and when present `protected_assets` `[{"kind", "name"}]`, `rank` (1 or more) and `repository_id`. Leave out anything else, like notes or evidence.
4. **Dry run.** `konvu invariants push --file <file> --dry-run -o json`. It writes nothing. On an error, follow its `suggestion`: fix what you can (a statement too long, a missing property) and ask the user only for what you can't.
5. **Confirm.** If konvu-invariants handed off, the user already agreed: go on. Otherwise show them, one line each, the rank and statement that will be added, and ask before writing.
6. **Push.** `konvu invariants push --file <file> -o json`. A 502 "not yet mapped" means saved: run the same push once more. Report one short line per rule: added (`created`), now validated (`validated`, it was a Konvu proposal) or already there (`unchanged`), with its rank. Then delete the temporary file.
