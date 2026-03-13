# Konvu CLI

Go CLI for security vulnerability management. Connects to the Konvu API for browsing, triaging, and acting on SCA findings.

The CLI also bundles AI skills (Claude Code, Cursor, etc.) directly in the binary using `go:embed`. When users run `konvu login` or `konvu skills install`, skills are extracted to `~/.claude/skills/` and `~/.agents/skills/` so AI agents can use the CLI effectively without any manual setup.

## Quick Reference

```bash
go build -o konvu .       # Build
go test ./... -v           # Test all packages
go vet ./...               # Lint
go generate ./internal/skills  # Sync embedded skills from skills/ → internal/skills/
```

## Project Structure

```
cmd/               # Cobra commands (one file per command group)
internal/skills/   # Embedded skills (go:embed) — copies of skills/, do NOT edit directly
pkg/api/           # HTTP client for Konvu API
pkg/auth/          # OAuth device flow + credential storage
pkg/config/        # Config dir, env vars, defaults
pkg/errors/        # Structured CLI errors with exit codes
pkg/output/        # Table/JSON/CSV formatting, interactive picker
pkg/mapping/       # Field mapping utilities
skills/            # Source of truth for Claude Code skills — edit here
scripts/           # Install script
```

## Architecture

- **Framework**: [spf13/cobra](https://github.com/spf13/cobra) for commands
- **API client**: `pkg/api.Client` — all API calls go through this
- **Auth**: OAuth device flow (`pkg/auth`) or API key, stored in OS-specific config dir
- **Output**: Auto-detects TTY for table vs JSON; `--output` flag overrides
- **Exit codes**: 0 success, 1 general, 2 usage, 3 not found, 4 auth failed (see `pkg/errors`)
- **Skills**: Embedded in binary via `go:embed`, extracted to `~/.claude/skills/` and `~/.agents/skills/` on `konvu login` or `konvu skills install`

## Conventions

- Commands go in `cmd/<name>.go` with a corresponding Cobra command
- All API responses are `map[string]any` — no generated types
- Use `pkg/errors.CLIError` for user-facing errors with suggestions
- Use `pkg/output` for all formatted output — never `fmt.Println` raw data
- Tests use the standard `testing` package, no external test frameworks
- Keep dependencies minimal — only cobra and golang.org/x/term

## Skills Sync

Skills live in `skills/` (source of truth) and are copied to `internal/skills/` for embedding.

```bash
# After editing any file in skills/:
go generate ./internal/skills
# Then commit both skills/ and internal/skills/ changes
```

CI fails if these are out of sync.

## Environment Variables

| Variable | Default | Purpose |
|---|---|---|
| `KONVU_ACCESS_TOKEN` | — | API auth (skips OAuth) |
| `KONVU_API_URL` | `https://api.konvu.com` | API base URL |
| `KONVU_ZITADEL_DOMAIN` | `https://auth.konvu.com` | OAuth provider |
| `KONVU_ZITADEL_CLIENT_ID` | — | OAuth client ID |
