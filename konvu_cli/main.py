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
    no_args_is_help=True,
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


@app.callback()
def callback() -> None:
    """Konvu CLI for security vulnerability management.

    Get started:
      konvu login              # Authenticate
      konvu whoami             # Check your company
      konvu finding list       # List security findings
      konvu finding get <id>   # Deep dive into a finding
      konvu vuln CVE-X         # Look up a CVE
      konvu metrics            # Security posture summary
    """
    pass


if __name__ == "__main__":
    app()
