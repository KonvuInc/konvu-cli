# Konvu CLI

Go CLI for security vulnerability management. Connects to the Konvu API for browsing, triaging, and acting on SCA findings.

The CLI bundles AI skills in the binary itself. On `konvu login` or `konvu skills install`, skills are extracted to `~/.claude/skills/` and `~/.agents/skills/` so AI agents can use the CLI effectively without any manual setup.

## Build & Verify

```bash
go install ./cmd/konvu    # Build & install
go test ./... -v           # Run all tests
go vet ./...               # Static analysis
```

Always run tests after making changes. CI runs the same commands.

## Project Layout

- `cmd/` — Cobra CLI commands. One file per command group.
- `skills/` — Embedded skills (`go:embed`) and Claude Code plugin skills. Edit skill content here.
- `pkg/` — Reusable packages: `api` (HTTP client), `auth` (OAuth/credentials), `config` (env/paths), `errors` (structured errors), `output` (formatting), `mapping` (field mapping).
- `scripts/` — Install script.

## Architecture

- **Framework**: [spf13/cobra](https://github.com/spf13/cobra) for commands
- **API client**: `pkg/api.Client` — all API calls go through this
- **Auth**: OAuth device flow (`pkg/auth`) or API key, stored in OS-specific config dir
- **Output**: Auto-detects TTY for table vs JSON; `--output` flag overrides
- **Exit codes**: 0 success, 1 general, 2 usage, 3 not found, 4 auth failed (see `pkg/errors`)
- **Skills**: Embedded in binary via `go:embed`, extracted to `~/.claude/skills/` and `~/.agents/skills/` on `konvu login` or `konvu skills install`

## Rules

1. **Run tests before claiming done.** `go test ./...` must pass.
2. **Don't add dependencies** without explicit approval. The CLI intentionally has minimal deps (cobra and golang.org/x/term).
3. **Use `pkg/output`** for all user-facing output. Never print raw JSON or unformatted data.
4. **Use `pkg/errors.CLIError`** for errors shown to users. Include a `Suggestion` field.
5. **Skills live in `skills/`** — edit skill content there directly.
6. **Exit codes matter.** 0=success, 1=general, 2=usage, 3=not found, 4=auth. Use constants from `pkg/errors`.
7. **No generated API types.** API responses are `map[string]any`. Parse what you need.
8. **Keep commands in `cmd/`** following the existing pattern: Cobra command var + `init()` registration.
9. **Tests use the standard `testing` package**, no external test frameworks.

## Environment Variables

| Variable | Default | Purpose |
|---|---|---|
| `KONVU_ACCESS_TOKEN` | — | API auth (skips OAuth) |
| `KONVU_API_URL` | `https://api.konvu.com` | API base URL |
| `KONVU_ZITADEL_DOMAIN` | `https://auth.konvu.com` | OAuth provider |
| `KONVU_ZITADEL_CLIENT_ID` | — | OAuth client ID |
