"""OAuth Device Flow helpers for CLI authentication (RFC 8628)."""

import json
import os
import time
import webbrowser
from collections.abc import Callable
from typing import Any

import httpx

from konvu_cli.config import (
    get_credentials_path,
    get_zitadel_client_id,
    get_zitadel_domain,
)

# Device flow configuration
DEFAULT_LOGIN_TIMEOUT = 300  # seconds
DEFAULT_POLL_INTERVAL = 5  # seconds


def perform_device_flow_login(
    timeout: float = DEFAULT_LOGIN_TIMEOUT,
    echo: Callable[[str], None] | None = None,
) -> dict[str, Any]:
    """Perform OAuth Device Flow login (RFC 8628).

    This flow works for CLI/headless environments:
    1. Request device code from authorization server
    2. Display verification URL and code to user
    3. Poll token endpoint until user completes authentication

    Args:
        timeout: Maximum seconds to wait for user to complete login
        echo: Optional callback for status messages (e.g., typer.echo)

    Returns:
        Dict with access_token, token_type, and expires_in

    Raises:
        RuntimeError: If login fails or times out
    """
    _echo = echo if echo is not None else (lambda _: None)

    zitadel_domain = get_zitadel_domain()
    client_id = get_zitadel_client_id()

    if not client_id:
        raise RuntimeError("Zitadel client ID not configured. Set KONVU_ZITADEL_CLIENT_ID.")

    # Step 1: Request device code
    device_auth_url = f"{zitadel_domain}/oauth/v2/device_authorization"

    with httpx.Client(http2=True) as client:
        response = client.post(
            device_auth_url,
            data={
                "client_id": client_id,
                "scope": "openid profile email",
            },
        )

    if response.status_code != 200:
        raise RuntimeError(f"Device authorization failed: {response.text}")

    device_data = response.json()
    device_code = device_data["device_code"]
    user_code = device_data["user_code"]
    verification_uri = device_data["verification_uri"]
    verification_uri_complete = device_data.get("verification_uri_complete")
    poll_interval = device_data.get("interval", DEFAULT_POLL_INTERVAL)
    expires_in = device_data.get("expires_in", timeout)

    # Step 2: Display instructions to user
    _echo(f"\nTo authenticate, visit: {verification_uri}")
    _echo(f"And enter code: {user_code}\n")

    # Try to open browser with pre-filled URL if available
    open_url = verification_uri_complete or verification_uri
    if webbrowser.open(open_url):
        _echo("Browser opened automatically.")
    else:
        _echo("Could not open browser automatically.")

    _echo(f"\nWaiting for authentication (timeout: {int(min(timeout, expires_in))}s)...")

    # Step 3: Poll for token
    return _poll_for_token(
        zitadel_domain=zitadel_domain,
        client_id=client_id,
        device_code=device_code,
        poll_interval=poll_interval,
        timeout=min(timeout, expires_in),
        echo=_echo,
    )


def _poll_for_token(
    zitadel_domain: str,
    client_id: str,
    device_code: str,
    poll_interval: int,
    timeout: float,
    echo: Callable[[str], None],
) -> dict[str, Any]:
    """Poll token endpoint until user completes authentication."""
    token_url = f"{zitadel_domain}/oauth/v2/token"
    start_time = time.time()

    with httpx.Client(http2=True) as client:
        while time.time() - start_time < timeout:
            response = client.post(
                token_url,
                data={
                    "grant_type": "urn:ietf:params:oauth:grant-type:device_code",
                    "device_code": device_code,
                    "client_id": client_id,
                },
            )

            if response.status_code == 200:
                token_data = response.json()
                if "access_token" not in token_data:
                    raise RuntimeError("Token response missing access_token")
                return {
                    "access_token": token_data["access_token"],
                    "token_type": token_data.get("token_type", "Bearer"),
                    "expires_in": token_data.get("expires_in"),
                }

            # Handle expected polling responses
            error_data = response.json() if response.content else {}
            error = error_data.get("error", "")

            if error == "authorization_pending":
                # User hasn't completed auth yet, keep polling
                time.sleep(poll_interval)
                continue
            elif error == "slow_down":
                # Server wants us to slow down
                poll_interval += 5
                time.sleep(poll_interval)
                continue
            elif error == "expired_token":
                raise RuntimeError("Device code expired. Please try again.")
            elif error == "access_denied":
                raise RuntimeError("Authentication was denied by the user.")
            else:
                # Unknown error
                error_msg = error_data.get("error_description", error or response.text)
                raise RuntimeError(f"Authentication failed: {error_msg}")

    raise RuntimeError(
        "Login timed out. Please try again.\n"
        "You can also set KONVU_ACCESS_TOKEN environment variable manually."
    )


def save_credentials(token_data: dict[str, Any]) -> None:
    """Save OAuth tokens to credentials file.

    The credentials file is created with restrictive permissions (0o600)
    to protect the access token.
    """
    creds_path = get_credentials_path()
    creds_path.parent.mkdir(parents=True, exist_ok=True)

    credentials: dict[str, str | int] = {
        "access_token": str(token_data["access_token"]),
    }

    # Add expiration time if available
    expires_in = token_data.get("expires_in")
    if expires_in is not None:
        credentials["expires_at"] = int(time.time()) + int(expires_in)

    # Write credentials with restricted permissions atomically (no race condition)
    fd = os.open(creds_path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    try:
        os.write(fd, json.dumps(credentials, indent=2).encode())
    finally:
        os.close(fd)


# Backwards compatibility alias
perform_oauth_login = perform_device_flow_login
