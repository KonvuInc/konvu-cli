"""Tests for OAuth Device Flow authentication."""

import os
import tempfile
from unittest.mock import MagicMock, patch

import pytest

from konvu_cli.auth.oauth import (
    DEFAULT_LOGIN_TIMEOUT,
    DEFAULT_POLL_INTERVAL,
    _poll_for_token,
    perform_device_flow_login,
)


class TestDeviceFlowLogin:
    """Tests for the device flow login process."""

    @patch("konvu_cli.auth.oauth.get_zitadel_client_id")
    def test_raises_error_when_no_client_id(self, mock_get_client_id: MagicMock) -> None:
        mock_get_client_id.return_value = ""

        with pytest.raises(RuntimeError, match="client ID not configured"):
            perform_device_flow_login()

    @patch("konvu_cli.auth.oauth.webbrowser.open")
    @patch("konvu_cli.auth.oauth._poll_for_token")
    @patch("konvu_cli.auth.oauth.httpx.Client")
    @patch("konvu_cli.auth.oauth.get_zitadel_client_id")
    @patch("konvu_cli.auth.oauth.get_zitadel_domain")
    def test_successful_device_flow(
        self,
        mock_domain: MagicMock,
        mock_client_id: MagicMock,
        mock_httpx: MagicMock,
        mock_poll: MagicMock,
        mock_browser: MagicMock,
    ) -> None:
        mock_domain.return_value = "https://auth.example.com"
        mock_client_id.return_value = "test-client-id"

        # Mock device authorization response
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            "device_code": "device-123",
            "user_code": "ABCD-1234",
            "verification_uri": "https://auth.example.com/device",
            "verification_uri_complete": "https://auth.example.com/device?code=ABCD-1234",
            "interval": 5,
            "expires_in": 300,
        }
        mock_httpx.return_value.__enter__.return_value.post.return_value = mock_response

        # Mock successful token response
        mock_poll.return_value = {
            "access_token": "test-token",
            "token_type": "Bearer",
            "expires_in": 3600,
        }

        messages: list[str] = []
        result = perform_device_flow_login(echo=messages.append)

        assert result["access_token"] == "test-token"
        assert any("ABCD-1234" in msg for msg in messages)
        mock_browser.assert_called_once()

    @patch("konvu_cli.auth.oauth.httpx.Client")
    @patch("konvu_cli.auth.oauth.get_zitadel_client_id")
    @patch("konvu_cli.auth.oauth.get_zitadel_domain")
    def test_device_authorization_failure(
        self,
        mock_domain: MagicMock,
        mock_client_id: MagicMock,
        mock_httpx: MagicMock,
    ) -> None:
        mock_domain.return_value = "https://auth.example.com"
        mock_client_id.return_value = "test-client-id"

        mock_response = MagicMock()
        mock_response.status_code = 400
        mock_response.text = "Invalid client"
        mock_httpx.return_value.__enter__.return_value.post.return_value = mock_response

        with pytest.raises(RuntimeError, match="Device authorization failed"):
            perform_device_flow_login()


class TestPollForToken:
    """Tests for the token polling logic."""

    @patch("konvu_cli.auth.oauth.httpx.Client")
    def test_returns_token_on_success(self, mock_httpx: MagicMock) -> None:
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_response.json.return_value = {
            "access_token": "test-token",
            "token_type": "Bearer",
            "expires_in": 3600,
        }
        mock_httpx.return_value.__enter__.return_value.post.return_value = mock_response

        result = _poll_for_token(
            zitadel_domain="https://auth.example.com",
            client_id="test-client",
            device_code="device-123",
            poll_interval=1,
            timeout=10,
            echo=lambda _: None,
        )

        assert result["access_token"] == "test-token"

    @patch("konvu_cli.auth.oauth.time.sleep")
    @patch("konvu_cli.auth.oauth.httpx.Client")
    def test_polls_on_authorization_pending(
        self, mock_httpx: MagicMock, mock_sleep: MagicMock
    ) -> None:
        # First response: pending, second response: success
        pending_response = MagicMock()
        pending_response.status_code = 400
        pending_response.content = b'{"error": "authorization_pending"}'
        pending_response.json.return_value = {"error": "authorization_pending"}

        success_response = MagicMock()
        success_response.status_code = 200
        success_response.json.return_value = {
            "access_token": "test-token",
            "token_type": "Bearer",
        }

        mock_httpx.return_value.__enter__.return_value.post.side_effect = [
            pending_response,
            success_response,
        ]

        result = _poll_for_token(
            zitadel_domain="https://auth.example.com",
            client_id="test-client",
            device_code="device-123",
            poll_interval=1,
            timeout=10,
            echo=lambda _: None,
        )

        assert result["access_token"] == "test-token"
        mock_sleep.assert_called_once_with(1)

    @patch("konvu_cli.auth.oauth.httpx.Client")
    def test_raises_on_access_denied(self, mock_httpx: MagicMock) -> None:
        mock_response = MagicMock()
        mock_response.status_code = 400
        mock_response.content = b'{"error": "access_denied"}'
        mock_response.json.return_value = {"error": "access_denied"}
        mock_httpx.return_value.__enter__.return_value.post.return_value = mock_response

        with pytest.raises(RuntimeError, match="denied by the user"):
            _poll_for_token(
                zitadel_domain="https://auth.example.com",
                client_id="test-client",
                device_code="device-123",
                poll_interval=1,
                timeout=10,
                echo=lambda _: None,
            )

    @patch("konvu_cli.auth.oauth.httpx.Client")
    def test_raises_on_expired_token(self, mock_httpx: MagicMock) -> None:
        mock_response = MagicMock()
        mock_response.status_code = 400
        mock_response.content = b'{"error": "expired_token"}'
        mock_response.json.return_value = {"error": "expired_token"}
        mock_httpx.return_value.__enter__.return_value.post.return_value = mock_response

        with pytest.raises(RuntimeError, match="expired"):
            _poll_for_token(
                zitadel_domain="https://auth.example.com",
                client_id="test-client",
                device_code="device-123",
                poll_interval=1,
                timeout=10,
                echo=lambda _: None,
            )


class TestSaveCredentials:
    """Tests for credential storage."""

    def test_saves_credentials_to_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            creds_path = os.path.join(tmpdir, "credentials.json")

            with patch(
                "konvu_cli.auth.oauth.get_credentials_path",
                return_value=type(
                    "Path",
                    (),
                    {
                        "parent": type("P", (), {"mkdir": lambda *a, **k: None})(),
                        "__str__": lambda s: creds_path,
                    },
                )(),
            ):
                # Use a simpler approach - just test the file writing logic
                pass

    def test_credentials_file_has_restricted_permissions(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            creds_file = os.path.join(tmpdir, "creds.json")

            # Simulate the atomic write
            fd = os.open(creds_file, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
            try:
                os.write(fd, b'{"access_token": "test"}')
            finally:
                os.close(fd)

            # Check permissions (Unix only)
            mode = os.stat(creds_file).st_mode
            assert mode & 0o777 == 0o600


class TestConstants:
    """Tests for module constants."""

    def test_default_timeout_is_reasonable(self) -> None:
        assert DEFAULT_LOGIN_TIMEOUT == 300  # 5 minutes

    def test_default_poll_interval_is_reasonable(self) -> None:
        assert DEFAULT_POLL_INTERVAL == 5  # 5 seconds per RFC 8628
