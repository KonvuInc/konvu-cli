import json
from pathlib import Path
from unittest.mock import MagicMock, patch

from typer.testing import CliRunner

from konvu_cli.main import app

runner = CliRunner()


def test_help() -> None:
    result = runner.invoke(app, ["--help"])
    assert result.exit_code == 0
    assert "Konvu CLI" in result.output



def test_vuln_help() -> None:
    result = runner.invoke(app, ["vuln", "--help"])
    assert result.exit_code == 0
    assert "CVE ID" in result.output


def test_metrics_help() -> None:
    result = runner.invoke(app, ["metrics", "--help"])
    assert result.exit_code == 0
    assert "--since" in result.output


def test_dismiss_help() -> None:
    result = runner.invoke(app, ["dismiss", "--help"])
    assert result.exit_code == 0
    assert "--dry-run" in result.output


def test_whoami_no_auth() -> None:
    result = runner.invoke(app, ["whoami"])
    assert result.exit_code == 1
    assert "Not logged in" in result.output


def test_login_help() -> None:
    result = runner.invoke(app, ["login", "--help"])
    assert result.exit_code == 0
    assert "--timeout" in result.output
    assert "--api-key" in result.output


def test_login_interactive_picker_shows_options() -> None:
    """Interactive login should show both auth methods."""
    # Input "3" to trigger an invalid choice, then ctrl-c / EOF
    result = runner.invoke(app, ["login"], input="3\n")
    assert "Browser login (OAuth)" in result.output
    assert "API key" in result.output


def test_login_api_key_direct(tmp_path: Path) -> None:
    """--api-key with a value should validate and save credentials."""
    creds_file = tmp_path / "credentials.json"
    mock_client = MagicMock()
    mock_client.__enter__ = MagicMock(return_value=mock_client)
    mock_client.__exit__ = MagicMock(return_value=False)
    mock_client.get = MagicMock(return_value={"name": "Acme Corp"})

    with (
        patch("konvu_cli.commands.auth.KonvuClient", return_value=mock_client),
        patch("konvu_cli.commands.auth.save_credentials") as mock_save,
    ):
        result = runner.invoke(app, ["login", "--api-key", "api_test123"])

    assert result.exit_code == 0
    assert "Logged in to: Acme Corp" in result.output
    mock_save.assert_called_once_with({"access_token": "api_test123"})
    # Verify KonvuClient was called with the token for validation
    from konvu_cli.commands.auth import KonvuClient

    mock_client_call = mock_client.get.call_args
    assert mock_client_call[0][0] == "/companies/current"


def test_login_api_key_invalid() -> None:
    """Invalid API key should show error."""
    from konvu_cli.api.client import AuthenticationError

    mock_client = MagicMock()
    mock_client.__enter__ = MagicMock(return_value=mock_client)
    mock_client.__exit__ = MagicMock(return_value=False)
    mock_client.get = MagicMock(side_effect=AuthenticationError("Unauthorized"))

    with patch("konvu_cli.commands.auth.KonvuClient", return_value=mock_client):
        result = runner.invoke(app, ["login", "--api-key", "bad_key"])

    assert result.exit_code == 1
    assert "Invalid API key" in result.output


def test_login_api_key_interactive_prompt(tmp_path: Path) -> None:
    """Choosing option 2 in interactive picker should prompt for API key."""
    mock_client = MagicMock()
    mock_client.__enter__ = MagicMock(return_value=mock_client)
    mock_client.__exit__ = MagicMock(return_value=False)
    mock_client.get = MagicMock(return_value={"name": "Acme Corp"})

    with (
        patch("konvu_cli.commands.auth.KonvuClient", return_value=mock_client),
        patch("konvu_cli.commands.auth.save_credentials") as mock_save,
    ):
        # Pick option 2, then enter the key
        result = runner.invoke(app, ["login"], input="2\napi_mykey456\n")

    assert result.exit_code == 0
    assert "Logged in to: Acme Corp" in result.output
    mock_save.assert_called_once_with({"access_token": "api_mykey456"})


def test_logout_when_not_logged_in(tmp_path: Path) -> None:
    """Logout should work even when not logged in."""
    creds_file = tmp_path / "nonexistent" / "credentials.json"
    with patch("konvu_cli.commands.auth.get_credentials_path", return_value=creds_file):
        result = runner.invoke(app, ["logout"])
        assert result.exit_code == 0
        assert "Not currently logged in" in result.output


def test_logout_removes_credentials(tmp_path: Path) -> None:
    """Logout should remove credentials file."""
    creds_file = tmp_path / "credentials.json"
    creds_file.write_text('{"access_token": "test"}')

    with patch("konvu_cli.commands.auth.get_credentials_path", return_value=creds_file):
        result = runner.invoke(app, ["logout"])
        assert result.exit_code == 0
        assert "Logged out successfully" in result.output
        assert not creds_file.exists()
