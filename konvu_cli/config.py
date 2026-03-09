import os
import sys
from pathlib import Path

APP_NAME = "konvu"


def _get_platform_config_dir() -> Path:
    """Get platform-specific base configuration directory."""
    if sys.platform == "win32":
        return Path(os.environ.get("APPDATA", str(Path.home())))
    if sys.platform == "darwin":
        return Path.home() / "Library" / "Application Support"
    # Linux/Unix - follow XDG Base Directory Specification
    return Path(os.environ.get("XDG_CONFIG_HOME", str(Path.home() / ".config")))


def get_config_dir() -> Path:
    """Get the configuration directory path.

    Uses XDG_CONFIG_HOME on Linux, ~/Library/Application Support on macOS,
    or APPDATA on Windows.
    """
    return _get_platform_config_dir() / APP_NAME


def get_credentials_path() -> Path:
    """Get the path to the credentials file."""
    return get_config_dir() / "credentials.json"


def get_api_base_url() -> str:
    """Get the API base URL from environment or default."""
    return os.environ.get("KONVU_API_URL", "https://api.konvu.com")


# Production defaults
DEFAULT_ZITADEL_DOMAIN = "https://auth.konvu.com"
DEFAULT_ZITADEL_CLIENT_ID = ""  # Must be set via env or baked in at build time


def get_zitadel_domain() -> str:
    """Get Zitadel domain for OAuth.

    Checks KONVU_ZITADEL_DOMAIN first, falls back to ZITADEL_DOMAIN.
    """
    return os.environ.get(
        "KONVU_ZITADEL_DOMAIN",
        os.environ.get("ZITADEL_DOMAIN", DEFAULT_ZITADEL_DOMAIN),
    )


def get_zitadel_client_id() -> str:
    """Get Zitadel client ID for OAuth.

    Checks KONVU_ZITADEL_CLIENT_ID first, falls back to ZITADEL_CLI_CLIENT_ID.
    The CLI has its own dedicated Zitadel application (not shared with MCP).
    """
    return os.environ.get(
        "KONVU_ZITADEL_CLIENT_ID",
        os.environ.get("ZITADEL_CLI_CLIENT_ID", DEFAULT_ZITADEL_CLIENT_ID),
    )
