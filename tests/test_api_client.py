import os
from unittest.mock import MagicMock, patch

import pytest

from konvu_cli.api.client import AuthenticationError, KonvuClient


def test_default_timeout_is_120_seconds() -> None:
    client = KonvuClient()
    assert client._client.timeout.read == 120.0
    client.close()


def test_client_raises_when_no_token() -> None:
    with patch("konvu_cli.api.client.get_credentials_path") as mock_path:
        mock_path.return_value = MagicMock()
        mock_path.return_value.exists.return_value = False

        client = KonvuClient()
        with pytest.raises(AuthenticationError, match="Not logged in"):
            client.get_auth_header()


def test_client_returns_bearer_token() -> None:
    with patch("konvu_cli.api.client.get_credentials_path") as mock_path:
        mock_path.return_value = MagicMock()
        mock_path.return_value.exists.return_value = True
        mock_path.return_value.read_text.return_value = '{"access_token": "test-token"}'

        client = KonvuClient()
        header = client.get_auth_header()
        assert header == {"Authorization": "Bearer test-token"}


class TestTokenSourceHierarchy:
    def test_explicit_token_takes_priority(self) -> None:
        """Explicit token should be used even if env var and file exist."""
        with patch.dict(os.environ, {"KONVU_ACCESS_TOKEN": "env-token"}, clear=False):
            client = KonvuClient(access_token="explicit-token")
            header = client.get_auth_header()
            assert header["Authorization"] == "Bearer explicit-token"

    def test_env_token_over_file(self, tmp_path: pytest.TempPathFactory) -> None:
        """Env var should be used over credentials file."""
        creds_file = tmp_path / "credentials.json"  # type: ignore[operator]
        creds_file.write_text('{"access_token": "file-token"}')

        with patch("konvu_cli.api.client.get_credentials_path", return_value=creds_file):
            with patch.dict(os.environ, {"KONVU_ACCESS_TOKEN": "env-token"}, clear=False):
                client = KonvuClient()
                header = client.get_auth_header()
                assert header["Authorization"] == "Bearer env-token"

    def test_file_token_when_no_env(self, tmp_path: pytest.TempPathFactory) -> None:
        """Credentials file should be used when no env var."""
        creds_file = tmp_path / "credentials.json"  # type: ignore[operator]
        creds_file.write_text('{"access_token": "file-token"}')

        with patch("konvu_cli.api.client.get_credentials_path", return_value=creds_file):
            with patch.dict(os.environ, {}, clear=True):
                os.environ.pop("KONVU_ACCESS_TOKEN", None)
                client = KonvuClient()
                header = client.get_auth_header()
                assert header["Authorization"] == "Bearer file-token"

    def test_no_token_raises_error(self, tmp_path: pytest.TempPathFactory) -> None:
        """Should raise AuthenticationError when no token available."""
        creds_file = tmp_path / "nonexistent" / "credentials.json"  # type: ignore[operator]

        with patch("konvu_cli.api.client.get_credentials_path", return_value=creds_file):
            with patch.dict(os.environ, {}, clear=True):
                os.environ.pop("KONVU_ACCESS_TOKEN", None)
                client = KonvuClient()
                with pytest.raises(AuthenticationError, match="Not logged in"):
                    client.get_auth_header()
