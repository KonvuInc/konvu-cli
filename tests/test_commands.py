from pathlib import Path
from unittest.mock import patch

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
