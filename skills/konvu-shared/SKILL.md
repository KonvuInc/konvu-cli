---
name: konvu-shared
version: 1.1.0
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
konvu principles get <id>                    # One rule: controls, code anchors, agent steering
konvu principles push --file rules.json [--dry-run]      # Add rules you wrote
konvu principles push --file rules.json --onboarding K7P2XQ9M  # No login yet: save with a web onboarding code
konvu principles accept <id> [--dry-run]     # Accept a suggested rule, making it active
konvu principles decline <id>                # Decline a suggested rule
konvu finding pr-check counts                # What Konvu's PR check analyzed
konvu finding pr-check list [--outcome violation|pass]   # Analyzed PRs and their flaws
konvu finding pr-check get <pr-url>          # One PR's flaws: attack path, evidence
```

`accept` and `decline` act only on a suggestion (`proposed`): accept exits 1 for anything else, decline for an active or retired one (an already declined one exits 0). A malformed id exits 2, an unknown one 3.

## Tips

- Pipe with `-o json` for structured output: `konvu finding list -o json | jq '.findings[]'`
- Use `-q` on finding list for bare IDs: `konvu finding list -q | xargs -I {} konvu finding get {}`
- Use `--help-all` for the complete reference of all commands and flags
