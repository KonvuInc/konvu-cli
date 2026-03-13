# Konvu CLI — Agent Guidelines

Cross-agent instructions for AI coding assistants (Claude Code, Cursor, Copilot, OpenCode).

This CLI bundles AI skills in the binary itself. On `konvu login` or `konvu skills install`, skills are extracted to `~/.claude/skills/` (Claude Code) and `~/.agents/skills/` (cross-agent convention). This gives agents zero-config access to guided workflows like security triage.

## Build & Verify

```bash
go install ./cmd/konvu      # Build & install binary
go test ./... -v            # Run all tests
go vet ./...                # Static analysis
```

Always run tests after making changes. CI runs the same commands.

## Project Layout

- `cmd/` — Cobra CLI commands. One file per command group.
- `pkg/` — Reusable packages: `api` (HTTP client), `auth` (OAuth/credentials), `config` (env/paths), `errors` (structured errors), `output` (formatting).
- `skills/` — Embedded skills (`go:embed`) and Claude Code plugin skills. Edit skill content here.

## Rules

1. **Run tests before claiming done.** `go test ./...` must pass.
2. **Don't add dependencies** without explicit approval. The CLI intentionally has minimal deps.
3. **Use `pkg/output`** for all user-facing output. Never print raw JSON or unformatted data.
4. **Use `pkg/errors.CLIError`** for errors shown to users. Include a `Suggestion` field.
5. **Skills live in `skills/`** — edit skill content there directly.
6. **Exit codes matter.** 0=success, 1=general, 2=usage, 3=not found, 4=auth. Use constants from `pkg/errors`.
7. **No generated API types.** API responses are `map[string]any`. Parse what you need.
8. **Keep commands in `cmd/`** following the existing pattern: Cobra command var + `init()` registration.
