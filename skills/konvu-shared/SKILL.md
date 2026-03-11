---
name: konvu-shared
version: 1.0.0
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

## Tips

- Pipe with `-o json` for structured output: `konvu finding list -o json | jq '.findings[]'`
- Use `-q` on finding list for bare IDs: `konvu finding list -q | xargs -I {} konvu finding get {}`
- Use `--help-all` for the complete reference of all commands and flags
