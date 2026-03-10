# konvu_cli/main.py
import json

import typer

from konvu_cli import __version__
from konvu_cli.commands import auth, dismiss, finding, metrics, vuln
from konvu_cli.config import get_api_base_url
from konvu_cli.output.detection import OutputFormat, detect_output_format

app = typer.Typer(
    name="konvu",
    help="Konvu CLI - Security vulnerability management",
    invoke_without_command=True,
)

# Register command groups
app.add_typer(auth.app, name="auth", help="Authentication commands")
app.add_typer(finding.app, name="finding", help="Security findings")

# Top-level convenience commands
app.command(name="whoami", help="Show current user and company")(auth.whoami)
app.command(name="login", help="Authenticate with Konvu")(auth.login)
app.command(name="logout", help="Clear stored credentials")(auth.logout)
app.command(name="vuln", help="Look up vulnerability details")(vuln.get_vulnerability)
app.command(name="metrics", help="Show security metrics")(metrics.show_metrics)
app.command(name="dismiss", help="Dismiss security issues")(dismiss.dismiss_issues)


@app.command()
def version(
    output: str | None = typer.Option(None, "--output", "-o", help="Output format: json, text"),
) -> None:
    """Show CLI version."""
    output_format = detect_output_format(output)
    if output_format == OutputFormat.JSON:
        typer.echo(json.dumps({"version": __version__, "api_url": get_api_base_url()}, indent=2))
    else:
        typer.echo(f"konvu-cli {__version__} (api: {get_api_base_url()})")


# --- Compact full reference for LLM / automation consumers ---

_HELP_ALL_TEXT = """\
konvu-cli — Security vulnerability management

AUTHENTICATION
  konvu login                          Authenticate with Konvu (opens browser)
  konvu logout                         Clear stored credentials
  konvu whoami [-o json]               Show current user and company

FINDINGS
  konvu finding list [OPTIONS]         List security findings
    --since TEXT          Start date: '7d', '30d', or ISO date
    --until TEXT          End date: 'now' or ISO date
    -s, --severity TEXT   Filter: critical, high, moderate, low
    -a, --assessment TEXT Filter: exploitable, false-positive, inconclusive, not-assessed
    --state TEXT          Filter: open, dismissed, fixed, muted
    --has-fix TEXT        Filter: fixed, no_fix
    -r, --repo TEXT       Filter by repository URL or name
    --cve TEXT            Filter by CVE ID
    -d, --dependency TEXT Filter by dependency name
    --sort TEXT           Sort: severity, recommendation, first_seen_at, updated_at [default: recommendation]
    --order TEXT          Order: asc, desc [default: desc]
    -n, --limit INT       Max findings [default: 50]
    --offset INT          Skip N results [default: 0]
    -o, --output TEXT     Format: json, table, csv
    -q, --quiet           Output bare finding IDs only
    --count               Output only the total count
    -g, --group-by TEXT   Group by: repository, dependency, severity, assessment
    --fields TEXT         Comma-separated fields to include

  konvu finding get FINDING_ID [OPTIONS]  Get finding detail
    -i, --include TEXT    Include: evidence, logs
    -o, --output TEXT     Format: json, table
    --fields TEXT         Comma-separated fields to include

  konvu finding counts [OPTIONS]       Assessment counts
    --since TEXT          Start date: '7d', '30d', or ISO date
    --until TEXT          End date: 'now' or ISO date
    -s, --severity TEXT   Filter: critical, high, moderate, low
    -r, --repo TEXT       Filter by repository
    -g, --group-by TEXT   Break down by: severity, week, month
    -o, --output TEXT     Format: json, table

VULNERABILITY LOOKUP
  konvu vuln VULN_ID [OPTIONS]         Look up a CVE/GHSA
    -i, --include TEXT    Include: summary, technical, exploitability, remediation, references
    -o, --output TEXT     Format: json, table

METRICS
  konvu metrics [OPTIONS]              Security posture summary
    --since TEXT          Start date [default: 30d]
    --until TEXT          End date [default: now]
    --interval TEXT       Aggregation: day, week, month [default: week]
    -i, --include TEXT    Include: summary, trends, breakdown, top_cves, new_vs_closed
    --compare TEXT        Compare to: previous_period, 30d_ago, 90d_ago
    -o, --output TEXT     Format: json, table

DISMISS
  konvu dismiss [OPTIONS]              Dismiss security issues
    --issues TEXT         Comma-separated issue IDs
    -a, --assessment TEXT Filter: dismiss all with this assessment
    -s, --severity TEXT   Filter by severity
    -r, --repo TEXT       Filter by repository
    --reason TEXT         Reason [default: "Dismissed via Konvu CLI"]
    --dry-run             Preview without executing
    -o, --output TEXT     Format: json, table

EXAMPLES
  konvu finding list --since 7d --assessment exploitable
  konvu finding list --severity critical --sort first_seen_at -o json
  konvu finding list --assessment exploitable --group-by repository
  konvu finding list --assessment not-assessed --count
  konvu finding get abc-123 --include evidence --include logs
  konvu finding counts --group-by severity
  konvu vuln CVE-2024-1234 --include technical
  konvu metrics --since 90d --include trends --compare previous_period
  konvu dismiss --assessment false-positive --repo org/repo --dry-run

OUTPUT FORMATS
  Most commands support: -o json (structured), -o table (human), -o csv (finding list only)
  Default is json when piped, table when interactive.
  Use -q/--quiet on finding list for bare IDs (useful for piping).

EXIT CODES
  0  Success
  1  General error
  2  Invalid arguments
  3  Not found
  4  Authentication failed
"""


@app.command(name="help-all", hidden=True)
def help_all() -> None:
    """Print full CLI reference (all commands, options, and examples)."""
    typer.echo(_HELP_ALL_TEXT)


@app.callback(invoke_without_command=True)
def callback(
    ctx: typer.Context,
    help_all_flag: bool = typer.Option(
        False, "--help-all", help="Show full CLI reference (all commands and options)", is_eager=True
    ),
) -> None:
    """Konvu CLI for security vulnerability management.

    Get started:
      konvu login              # Authenticate
      konvu whoami             # Check your company
      konvu finding list       # List security findings
      konvu finding get <id>   # Deep dive into a finding
      konvu vuln CVE-X         # Look up a CVE
      konvu metrics            # Security posture summary

    Use --help-all for the full reference of all commands and options.
    """
    if help_all_flag:
        typer.echo(_HELP_ALL_TEXT)
        raise typer.Exit()
    if ctx.invoked_subcommand is None:
        typer.echo(ctx.get_help())
        raise typer.Exit()


if __name__ == "__main__":
    app()
