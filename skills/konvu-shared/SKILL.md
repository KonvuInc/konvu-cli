---
name: konvu-shared
version: 1.5.0
description: "Konvu CLI: Auth setup, global flags, env vars, and common patterns."
metadata:
  requires:
    bins: ["konvu"]
---
# Konvu CLI — Shared Reference

## Authentication

```bash
konvu login                    # Interactive picker (OAuth or API key)
konvu login --api-key API_KEY  # Direct API key (CI/CD)
konvu whoami                   # Verify authentication
konvu logout                   # Clear credentials
```

## Global Flags

All commands support:
- `-o, --output json|table|csv` — Output format (default: table for TTY, json for pipe)

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `KONVU_API_URL` | API base URL (default: https://api.konvu.com) |
| `KONVU_ACCESS_TOKEN` | Token auth (skips OAuth) |
| `KONVU_ZITADEL_DOMAIN` | OAuth domain |
| `KONVU_ZITADEL_CLIENT_ID` | OAuth client ID |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | Not found |
| 4 | Authentication failed |

## Principles and PR checks

```bash
konvu principles list [--status proposed|validated|all]  # Principles, by rank
konvu principles guide [-o json]             # Konvu's guide to a good principle: Markdown, even piped
konvu principles get <id>                    # One principle: controls, code anchors, agent steering
konvu principles push --file principles.json [--dry-run]      # Add principles you wrote
konvu principles push --file principles.json --onboarding K7P2XQ9M  # No login yet: save with a web onboarding code
konvu principles accept <id> [--dry-run]     # Accept a suggested principle, making it active
konvu principles decline <id>                # Decline a suggested principle
konvu principles edit <id> --file <json>|- [--dry-run]   # Edit an active principle: the file is 'get -o json', edited
konvu principles delete <id> [--yes]         # Delete an active principle: no more PR checks or agent steering
konvu finding pr-check counts                # What Konvu's PR check analyzed
konvu finding pr-check list [--outcome violation|pass] [--broke-principle]   # Analyzed PRs and their flaws
konvu finding pr-check get <pr-url>          # One PR's flaws: attack path, evidence, principles broken
konvu finding pr-check dismiss <pr-url> <finding-id> --reason TEXT [--dry-run]      # Not a real flaw
konvu finding pr-check accept-risk <pr-url> <finding-id> --reason TEXT [--dry-run]  # Real, but accepted
```

`accept` and `decline` act only on a suggestion (`proposed`): accept exits 1 for anything else, decline for an active or retired one (an already declined one exits 0). A malformed id exits 2, an unknown one 3.

`edit` and `delete` act only on an active principle (`validated`). `edit` sends the file's `statement`, `why_it_matters`, `property`, `rank` and `protected_assets` with its `revision`; a file read before someone else's edit writes nothing and exits 1 (`STALE_REVISION`): get it again and redo the edit. `delete` names the principle and asks on a terminal; from a script or an agent it needs `--yes` (exit 2 without). Show the user the change or the principle and get their go-ahead first. A server too old for either exits 3 (`NOT_SUPPORTED`); a read-only key or the demo view exits 4.

## Security Book

```bash
konvu book ls [folder] [-o json]             # The book's pages, with EDIT WITH: the command that changes each page's data
konvu book read <path> [-o json]             # One page as markdown, with the sources it cites; Markdown even piped
konvu company get > company.json             # Company and infrastructure facts as one JSON document
konvu company apply --file company.json [--dry-run]                  # Change them: prints one line per change
konvu threat-model get / apply --file F [--dry-run]                  # The threat model, the same way
konvu threat-profile get --repo <owner/name|id> / apply --repo R --file F [--dry-run]   # A repository's threat profile
konvu threat-profile get --app <name|id> [-o table] / apply --app A --file F [--dry-run]   # An app's: corrections kept on the app, never on its repos
konvu inventory endpoints get --repo <owner/name|id> / apply --repo R --file F|- [--dry-run]      # A repository's endpoints
konvu inventory data-objects get --repo <owner/name|id> / apply --repo R --file F|- [--dry-run]   # A repository's data objects and their fields
konvu history [--object company|threat-model|threat-profile|app-threat-profile|catalog|note] [--key ID] [--limit N] [--offset N]   # Every edit: when, who, before → after
konvu history revert <id> --revision <current revision>              # Undo one edit; the revision is the object's 'get' one (a note's REVISION from note list)
konvu knowledge note add --label L --repo R [--repo R2] --text T|--file F|-   # A note no other data holds; Konvu places it in the book
konvu knowledge note list [--label L] [--repo R] [--limit N] [--offset N]      # Notes, newest first, with their revision and the page each was placed on
konvu knowledge note edit <id> [--label L] [--text T|--file F|-] [--repo R ...]  # Change a note; what is left out keeps its value
konvu knowledge note rm <id> [--yes]                                          # Remove a note (undo with history revert); --yes without a terminal
```

Every page is read-only, and no command writes a page's text: change the data behind a page with the command `book ls` names for it, and Konvu rewrites the page. `get` prints `revision`, the data and `provenance` (read-only, never sent; the threat model adds read-only `overrides`, and its sections `assets`, `boundaries` and `exposures` show exactly the list applied, items left out staying hidden after later runs; `inventory endpoints` and `inventory data-objects` add read-only `hidden`, and their list shows exactly what is applied: an item left as printed stays Konvu's, one added or changed is the person's and kept by every later mapping run, one left out stays hidden; `konvu history` does not list them); in `apply`, a value left out keeps its value and `null` clears it, and the apply is refused with `DATA_CHANGED` (exit 1, nothing written) once the data changed since that `get`: get it again and redo the edit. Exit 2 lists every problem in the file, exit 3 an unknown repository or edit, or a server without these routes yet, exit 4 a read-only key or the demo view. Notes are for knowledge that fits none of that data, always with a label of the user's choosing (1 to 40 lowercase letters, digits and dashes) and a `--repo` for each repository they concern; after each add, edit or removal Konvu places the note in the book, blending it into one of its pages or writing a `context/<page>.md` page for it (`note list` shows where, or "not placed yet"). To change what a `context/` page says, edit the notes behind it. Every note change is in `konvu history --object note` and can be reverted with the note's revision (a removal moved it one past the last one listed; a refusal names the current one). Findings, scans, ratings, triage history, the repository list, the rest of the inventory graph (controls and their code), collected files, sources and scores are not book data and are not edited here (rating or dismissing a finding keeps its own `konvu finding` commands). Change data only when the user asks, after showing them the dry run.

## Tips

- Pipe with `-o json` for structured output: `konvu finding list -o json | jq '.findings[]'`
- Use `-q` on finding list for bare IDs: `konvu finding list -q | xargs -I {} konvu finding get {}`
- Use `--help-all` for the complete reference of all commands and flags
