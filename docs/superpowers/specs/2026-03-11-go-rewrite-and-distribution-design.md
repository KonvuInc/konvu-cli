# Konvu CLI: Go Rewrite & Distribution Design

**Date**: 2026-03-11
**Status**: Approved

## Summary

Rewrite the konvu-cli from Python to Go for single-binary distribution, and establish a skills directory structure for Claude Code integration. Distribute via npm (primary), GitHub Releases, and optionally Homebrew.

## Motivation

- Target audience is Claude Code users (already have npm/Node.js)
- Enterprise customers need frictionless installation — no Python version management
- Security tooling ecosystem is Go-native (trivy, grype, cosign)
- Current codebase is 2,700 LOC at v0.1.0 — cheapest possible time to rewrite
- Skills bundling is trivial with a compiled binary + npm package

## Project Structure

```
konvu-cli/
├── cmd/                        # CLI command definitions (cobra)
│   ├── root.go                 # Root command, global flags, --help-all
│   ├── auth.go                 # login, logout, whoami
│   ├── finding.go              # list, get, counts, rate
│   ├── vuln.go                 # vulnerability lookup (vuln get)
│   ├── metrics.go              # security metrics (metrics show)
│   ├── dismiss.go              # bulk dismiss (dismiss run)
│   └── version.go              # version command
├── internal/
│   ├── api/
│   │   └── client.go           # HTTP client for Konvu API
│   ├── auth/
│   │   └── device_flow.go      # RFC 8628 Device Authorization Grant (Zitadel)
│   ├── config/
│   │   └── config.go           # Platform-specific config dirs, env vars
│   ├── errors/
│   │   └── errors.go           # CLIError, exit codes, JSON error formatting
│   ├── output/
│   │   ├── format.go           # JSON/table/CSV detection + formatting
│   │   ├── table.go            # Colored tables (text/tabwriter + ANSI)
│   │   └── picker.go           # Interactive arrow-key picker (auth method selection)
│   └── mapping/
│       └── mapping.go          # Assessment/recommendation mappings
├── skills/                     # Claude Code skills (bundled)
│   ├── konvu-shared/
│   │   └── SKILL.md            # Auth, global flags, env vars, common patterns
│   ├── konvu-auth/
│   │   └── SKILL.md
│   ├── konvu-finding-list/
│   │   └── SKILL.md
│   ├── konvu-finding-get/
│   │   └── SKILL.md
│   ├── konvu-finding-rate/
│   │   └── SKILL.md
│   ├── konvu-vuln/
│   │   └── SKILL.md
│   ├── konvu-metrics/
│   │   └── SKILL.md
│   ├── konvu-dismiss/
│   │   └── SKILL.md
│   ├── recipe-weekly-triage/
│   │   └── SKILL.md
│   └── recipe-posture-report/
│       └── SKILL.md
├── registry/
│   └── recipes.yaml            # Workflow skill index
├── .claude/
│   └── settings.json           # Claude Code permissions for konvu commands
├── go.mod
├── go.sum
├── main.go                     # Entry point
├── goreleaser.yml              # Cross-platform builds
├── package.json                # npm wrapper for distribution
├── konvu_cli/                  # (Python - removed after port complete)
├── pyproject.toml              # (Python - removed after port complete)
└── README.md
```

## Dependencies

| Purpose | Package |
|---------|---------|
| CLI framework | `github.com/spf13/cobra` |
| HTTP client | `net/http` (stdlib) |
| JSON | `encoding/json` (stdlib) |
| Config/env | `os.Getenv` (stdlib) with fallbacks |
| Tables | `text/tabwriter` (stdlib) |
| CSV | `encoding/csv` (stdlib) |
| Color | Internal helper using ANSI codes |
| Terminal raw mode | `golang.org/x/term` (for interactive picker) |

2 external dependencies (cobra, x/term). Everything else is stdlib.

**Notably absent:**
- No `golang.org/x/oauth2` — the Python auth uses RFC 8628 Device Authorization Grant, which is a simple HTTP polling loop. Implemented directly with `net/http`.
- No `github.com/spf13/viper` — current Python config is just `os.Getenv` with fallbacks (63 lines). No need for a config framework.
- No `github.com/zalando/go-keyring` — Python stores credentials as a plain JSON file at the platform config dir with `0o600` permissions. The Go port preserves this behavior (no new features).

## Distribution

### Primary: npm

```bash
npm install -g @konvu/cli
```

- `goreleaser` builds binaries for macOS (arm64, x86), Linux (arm64, x86), Windows (x86)
- `package.json` wraps the binaries — npm install downloads the right one for the platform
- Skills directory and registry bundled in the npm package

### Secondary: GitHub Releases

```bash
curl -L https://github.com/KonvuTeam/konvu-cli/releases/latest/download/konvu-darwin-arm64.tar.gz | tar xz
sudo mv konvu /usr/local/bin/
```

Same binaries goreleaser produces. Skills bundled in the tarball.

### Future: Homebrew tap

```bash
brew install konvu/tap/konvu
```

goreleaser can auto-generate the Homebrew formula. Add when there's demand.

### CI Pipeline

GitHub Actions runs goreleaser on tagged releases → produces binaries → publishes to npm + GitHub Releases.

### Skills Bundling

The npm package includes the `skills/` and `registry/` directories alongside the binary. A `konvu skills path` command tells Claude Code where to find them.

## Command Parity

Direct 1:1 port. No new features, no changes to flags or behavior. The authoritative reference is the `_HELP_ALL_TEXT` block in `konvu_cli/main.py:44-132`.

### Authentication

| Command | Description |
|---------|-------------|
| Command | Flags |
|---------|-------|
| `konvu login` | `-t/--timeout` (default: 300s), `--api-key` (direct API key auth for CI/CD). Without flags: interactive picker (OAuth vs API key) |
| `konvu logout` | (no flags) |
| `konvu whoami` | `-o/--output` |

Auth method selection uses an interactive arrow-key picker (Go equivalent: raw terminal input via `golang.org/x/term` with numbered fallback for non-TTY).

### Findings

| Command | Flags |
|---------|-------|
| `konvu finding list` | `--since`, `--until`, `-s/--severity`, `-a/--assessment`, `--state`, `--has-fix`, `-r/--repo`, `--cve`, `-d/--dependency`, `--source`, `--source-id`, `--sort` (default: recommendation; values: severity, recommendation, first_seen_at, updated_at, dependency_name, cve), `--order` (default: desc), `-n/--limit` (default: 50), `--offset`, `-o/--output`, `-q/--quiet`, `--count`, `-g/--group-by`, `--fields` |
| `konvu finding get <id>` | `-i/--include` (evidence, logs), `-v/--verbose`, `-o/--output`, `--fields` |
| `konvu finding rate <id> <rating>` | `rating` is a positional arg (agree/disagree), `-c/--comment`, `--recommendation-id`, `-o/--output` |
| `konvu finding counts` | `--since`, `--until`, `-s/--severity`, `-r/--repo`, `--source`, `-g/--group-by`, `-o/--output` |

### Vulnerability Lookup

| Command | Flags |
|---------|-------|
| `konvu vuln get <vuln_id>` | `-i/--include` (summary, technical, exploitability, remediation, references, affected), `-o/--output` |

Note: `konvu vuln <id>` is registered as a top-level convenience alias for `konvu vuln get <id>`.

### Metrics

| Command | Flags |
|---------|-------|
| `konvu metrics show` | `--since` (default: 30d), `--until` (default: now), `--interval` (day/week/month, default: week), `-i/--include` (summary, trends, breakdown, top_cves, new_vs_closed), `--compare`, `-o/--output` |

Note: `konvu metrics` is registered as a top-level convenience alias for `konvu metrics show`.

### Dismiss

| Command | Flags |
|---------|-------|
| `konvu dismiss run` | `--issues`, `-a/--assessment`, `-s/--severity`, `-r/--repo`, `--reason` (default: "Dismissed via Konvu CLI"), `--dry-run`, `-o/--output` |

Note: `konvu dismiss` is registered as a top-level convenience alias for `konvu dismiss run`.

### Other Commands

| Command | Description |
|---------|-------------|
| `konvu version` | Show CLI version (`-o json` includes api_url) |
| `konvu help-all` | Print full CLI reference (hidden command) |
| `konvu --help-all` | Same, as a flag |

### Top-Level Convenience Aliases

The following subcommands are also registered as top-level commands for ergonomics:

- `konvu whoami` → `konvu auth whoami`
- `konvu login` → `konvu auth login`
- `konvu logout` → `konvu auth logout`
- `konvu vuln` → `konvu vuln get`
- `konvu metrics` → `konvu metrics show`
- `konvu dismiss` → `konvu dismiss run`

### Output Formats

- `--output json` (structured), `--output table` (human), `--output csv` (finding list only)
- Default: JSON when piped, table when interactive TTY
- `-q/--quiet` on finding list outputs bare finding IDs (useful for piping)

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | Not found |
| 4 | Authentication failed |

### Error Handling

The Go port preserves the structured error system from Python:
- `CLIError` struct with `Code`, `Message`, `Suggestion`, `Retryable`, `ExitCode` fields
- `FormatErrorJSON()` for machine-readable error output in `--output json` mode
- Located in `internal/errors/errors.go`

### Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `KONVU_API_URL` | API base URL | `https://api.konvu.com` |
| `KONVU_ACCESS_TOKEN` | Token auth (skips OAuth) | — |
| `KONVU_ZITADEL_DOMAIN` | OAuth domain | `https://auth.konvu.com` |
| `KONVU_ZITADEL_CLIENT_ID` | OAuth client ID | — |
| `ZITADEL_DOMAIN` | Fallback OAuth domain | — |
| `ZITADEL_CLI_CLIENT_ID` | Fallback OAuth client ID | — |

### Credential Storage

File-based: `~/.config/konvu/credentials.json` (Linux), `~/Library/Application Support/konvu/credentials.json` (macOS), `%APPDATA%/konvu/credentials.json` (Windows). File permissions `0600`.

## Testing Strategy

- Use Go's standard `testing` package (no external test framework)
- Use `net/http/httptest` for API client tests (mock HTTP server)
- Port all 8 existing test modules: API client, commands (auth, finding, vuln, metrics, dismiss), config, errors, mapping, output
- Run tests in CI with `go test ./...`
- No integration tests against the live API (same as Python — tests use mocked responses)

## Skills Structure

Following GWS convention. Each skill is a directory with a `SKILL.md` file containing YAML frontmatter and markdown instructions.

### SKILL.md Format

```yaml
---
name: konvu-finding-list
version: 1.0.0
description: "List and filter security findings from the terminal."
metadata:
  requires:
    bins: ["konvu"]
  cliHelp: "konvu finding list --help"
---
# finding list

> **PREREQUISITE:** Read `../konvu-shared/SKILL.md` for auth and global flags.

List security findings with filtering options.

## Usage
...

## Flags
...

## Examples
...
```

### Skill Types

- **Service skills** (`konvu-*`): One per command group, teach Claude how to use specific CLI commands
- **Workflow/recipe skills** (`recipe-*`): Multi-step sequences for common tasks (e.g., weekly triage)

### Registry

`registry/recipes.yaml` indexes workflow skills with descriptions and steps.

Skills content is out of scope for this design — the structure is established here and skills will be authored after the Go port.

## Migration Strategy

### Phase 1 — Scaffold Go project alongside Python

- Initialize `go.mod`, `main.go`, `cmd/`, `internal/`
- Python code stays untouched and functional
- Both coexist in the repo

### Phase 2 — Port commands one by one

- Start with `auth` (foundation everything else depends on)
- Then `finding list` (most-used command, validates API client + output formatting)
- Then remaining commands in any order
- Each command gets tests ported too

### Phase 3 — Distribution setup

- Add `goreleaser.yml`, `package.json`, GitHub Actions workflow
- Set up npm publishing under `@konvu/cli`
- Test installation on macOS, Linux, Windows

### Phase 4 — Skills scaffolding

- Create `skills/` directory structure and `registry/`
- Add `konvu-shared/SKILL.md` as the foundation
- Stub out remaining skill files
- Add `.claude/settings.json` with appropriate permissions

### Phase 5 — Cutover

- Remove `konvu_cli/`, `pyproject.toml`, Python test files
- Update README for new installation methods
- Tag v0.2.0

### What Stays the Same

The `konvu` binary name, all command names, all flags, all exit codes, all env vars. Drop-in replacement.
