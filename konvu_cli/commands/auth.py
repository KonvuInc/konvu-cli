import typer

from konvu_cli.api.client import APIError, AuthenticationError, KonvuClient
from konvu_cli.auth.oauth import (
    DEFAULT_LOGIN_TIMEOUT,
    perform_oauth_login,
    save_credentials,
)
from konvu_cli.config import get_credentials_path, get_zitadel_client_id
from konvu_cli.output.detection import OutputFormat, detect_output_format
from konvu_cli.output.formatters import format_json
from konvu_cli.output.picker import pick

app = typer.Typer(help="Authentication commands")


def _validate_api_key(api_key: str) -> dict[str, str]:
    """Validate an API key by calling the API.

    Returns company data on success, raises on failure.
    """
    with KonvuClient(access_token=api_key) as client:
        return client.get("/companies/current")


def _login_with_api_key(api_key: str | None) -> None:
    """Handle API key login flow.

    If api_key is None, prompts the user for it (masked input).
    Validates the key, saves credentials, and shows confirmation.
    """
    if not api_key:
        typer.echo(
            "\nCreate an API key at: https://app.konvu.com/configuration/api_keys\n",
            err=True,
        )
        api_key = typer.prompt("Paste your API key", hide_input=True)

    if not api_key or not api_key.strip():
        typer.echo("Error: API key cannot be empty.", err=True)
        raise typer.Exit(1)

    api_key = api_key.strip()

    typer.echo("Validating API key...")
    try:
        company = _validate_api_key(api_key)
    except AuthenticationError:
        typer.echo("Error: Invalid API key.", err=True)
        raise typer.Exit(1)
    except APIError as e:
        typer.echo(f"Error: {e}", err=True)
        raise typer.Exit(1)

    save_credentials({"access_token": api_key})
    typer.echo(f"Logged in to: {company.get('name', 'Unknown')}")


def _login_with_oauth(timeout: int) -> None:
    """Handle OAuth browser login flow."""
    typer.echo("Starting browser login...")
    token_data = perform_oauth_login(timeout=timeout, echo=typer.echo)
    save_credentials(token_data)

    typer.echo("\nLogin successful!")

    try:
        with KonvuClient() as client:
            company = client.get("/companies/current")
            typer.echo(f"Logged in to: {company.get('name', 'Unknown')}")
    except (AuthenticationError, APIError):
        pass


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
    api_key: str | None = typer.Option(
        None,
        "--api-key",
        help="Authenticate with an API key (pass value or omit to prompt)",
    ),
) -> None:
    """Authenticate with Konvu.

    \b
    When run without flags, presents an interactive choice:
      1. Browser login (OAuth)
      2. API key

    \b
    Examples:
      konvu login                      # Interactive picker
      konvu login --api-key api_...    # Direct API key (CI/CD, scripts)

    \b
    Exit codes:
      0  Success
      1  General error
    """
    try:
        # Direct API key via flag
        if api_key is not None:
            _login_with_api_key(api_key if api_key else None)
            return

        # Interactive picker
        oauth_available = bool(get_zitadel_client_id())

        if not oauth_available:
            # No OAuth configured — go straight to API key
            _login_with_api_key(None)
            return

        choice = pick(
            "How would you like to authenticate?",
            ["Browser login (OAuth)", "API key"],
        )

        if choice == 1:
            _login_with_api_key(None)
        else:
            _login_with_oauth(timeout)

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
