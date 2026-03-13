# Konvu CLI

Go CLI for security vulnerability management.

## Build & Test

```bash
go install ./cmd/konvu
go test ./... -v
go vet ./...
```

## Rules

- Run `go test ./...` before claiming done
- Don't add dependencies without explicit approval
- Use `pkg/output` for all user-facing output, never raw `fmt.Println`
- Use `pkg/errors.CLIError` for errors shown to users, include a `Suggestion` field
- API responses are `map[string]any` — no generated types
- Tests use the standard `testing` package, no external frameworks
- Edit skills directly in `skills/`
