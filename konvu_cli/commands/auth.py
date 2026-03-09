import typer

from konvu_cli.api.client import APIError, AuthenticationError, KonvuClient
from konvu_cli.auth.oauth import (
    DEFAULT_LOGIN_TIMEOUT,
    perform_oauth_login,
    save_credentials,
)
from konvu_cli.config import get_credentials_path
from konvu_cli.output.detection import OutputFormat, detect_output_format
from konvu_cli.output.formatters import format_json

app = typer.Typer(help="Authentication commands")


@app.command()
def whoami(
    output: str | None = typer.Option(
        None,
        "--output",
        "-o",
        help="Output format: json, table",
    ),
) -> None:
    """Show current user and company information.

    \b
    Examples:
      konvu whoami
      konvu whoami --output json

    \b
    Exit codes:
      0  Success
      1  General error
      4  Authentication failed
    """
    try:
        with KonvuClient() as client:
            data = client.get("/companies/current")

        output_format = detect_output_format(output)

        if output_format == OutputFormat.JSON:
            typer.echo(format_json(data))
        else:
            typer.echo(f"Company:        {data.get('name', 'Unknown')}")
            typer.echo(f"Repositories:   {data.get('repositories_count', 0)}")
            typer.echo(f"Integrations:   {data.get('integrations_count', 0)}")

    except AuthenticationError as e:
        typer.echo(f"Error: {e}", err=True)
        raise typer.Exit(1)


@app.command()
def login(
    timeout: int = typer.Option(
        DEFAULT_LOGIN_TIMEOUT, "--timeout", "-t", help="Login timeout in seconds"
    ),
) -> None:
    """Authenticate with Konvu via OAuth Device Flow."""
    try:
        typer.echo("Starting Konvu login...")

        token_data = perform_oauth_login(timeout=timeout, echo=typer.echo)
        save_credentials(token_data)

        typer.echo("\nLogin successful!")

        # Show company info (optional - don't fail login if this fails)
        try:
            with KonvuClient() as client:
                company = client.get("/companies/current")
                typer.echo(f"Logged in to: {company.get('name', 'Unknown')}")
        except (AuthenticationError, APIError):
            pass  # Token valid but couldn't fetch company info

    except RuntimeError as e:
        typer.echo(f"Error: {e}", err=True)
        raise typer.Exit(1)
    except KeyboardInterrupt:
        typer.echo("\nLogin cancelled.")
        raise typer.Exit(1)


@app.command()
def logout() -> None:
    """Clear stored credentials."""
    creds_path = get_credentials_path()
    if creds_path.exists():
        creds_path.unlink()
        typer.echo("Logged out successfully.")
    else:
        typer.echo("Not currently logged in.")
