import json
import os
from typing import Any

import httpx

from konvu_cli.config import get_api_base_url, get_credentials_path


class AuthenticationError(Exception):
    """Raised when authentication fails or user is not logged in."""


class APIError(Exception):
    """Raised when API request fails."""

    def __init__(self, message: str, status_code: int | None = None):
        super().__init__(message)
        self.status_code = status_code


class KonvuClient:
    """HTTP client for Konvu API."""

    def __init__(self, base_url: str | None = None, access_token: str | None = None):
        self.base_url = base_url or get_api_base_url()
        self._client = httpx.Client(timeout=120.0)
        self._explicit_token = access_token

    def _get_token(self) -> str:
        """Get access token from available sources.

        Token sources are checked in priority order:
        1. Explicit token (passed to __init__)
        2. Environment variable (KONVU_ACCESS_TOKEN)
        3. Credentials file (~/.konvu/credentials.json)
        """
        if self._explicit_token:
            return self._explicit_token

        env_token = os.environ.get("KONVU_ACCESS_TOKEN")
        if env_token:
            return env_token

        return self._read_token_from_file()

    def _read_token_from_file(self) -> str:
        """Read access token from credentials file."""
        creds_path = get_credentials_path()

        if not creds_path.exists():
            raise AuthenticationError("Not logged in. Run 'konvu login' first.")

        try:
            creds = json.loads(creds_path.read_text())
        except json.JSONDecodeError as e:
            raise AuthenticationError("Corrupted credentials. Run 'konvu login' again.") from e

        token = creds.get("access_token")
        if not token:
            raise AuthenticationError("Invalid credentials. Run 'konvu login' again.")

        return str(token)

    def get_auth_header(self) -> dict[str, str]:
        """Get authorization header with Bearer token."""
        return {"Authorization": f"Bearer {self._get_token()}"}

    def _check_response(self, response: httpx.Response) -> None:
        """Check response for errors and raise appropriate exceptions."""
        if response.status_code == 401:
            raise AuthenticationError("Session expired. Run 'konvu login' again.")
        if response.status_code >= 400:
            raise APIError(f"API error: {response.text}", status_code=response.status_code)

    def get(self, path: str, params: dict[str, Any] | None = None) -> dict[str, Any]:
        """Make authenticated GET request."""
        url = f"{self.base_url}{path}"
        response = self._client.get(url, headers=self.get_auth_header(), params=params)
        self._check_response(response)
        return response.json()  # type: ignore[no-any-return]

    def post(self, path: str, data: dict[str, Any] | None = None) -> dict[str, Any] | None:
        """Make authenticated POST request."""
        url = f"{self.base_url}{path}"
        response = self._client.post(url, headers=self.get_auth_header(), json=data)
        self._check_response(response)

        if response.status_code == 204:
            return None
        return response.json()  # type: ignore[no-any-return]

    def close(self) -> None:
        """Close the HTTP client."""
        self._client.close()

    def __enter__(self) -> "KonvuClient":
        return self

    def __exit__(self, *args: object) -> None:
        self.close()
