import os
from pathlib import Path
from unittest.mock import patch

from konvu_cli.config import (
    get_config_dir,
    get_credentials_path,
    get_zitadel_client_id,
    get_zitadel_domain,
)


def test_get_config_dir_returns_path() -> None:
    config_dir = get_config_dir()
    assert isinstance(config_dir, Path)
    assert config_dir.name == "konvu"


def test_get_credentials_path() -> None:
    creds_path = get_credentials_path()
    assert creds_path.name == "credentials.json"
    assert creds_path.parent.name == "konvu"


class TestZitadelConfig:
    def test_zitadel_domain_from_konvu_var(self) -> None:
        with patch.dict(
            os.environ, {"KONVU_ZITADEL_DOMAIN": "https://custom.auth.com"}, clear=False
        ):
            assert get_zitadel_domain() == "https://custom.auth.com"

    def test_zitadel_domain_fallback_to_zitadel_var(self) -> None:
        env = {"ZITADEL_DOMAIN": "https://zitadel.example.com"}
        with patch.dict(os.environ, env, clear=True):
            assert get_zitadel_domain() == "https://zitadel.example.com"

    def test_zitadel_domain_default(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            assert get_zitadel_domain() == "https://auth.konvu.com"

    def test_zitadel_client_id_from_konvu_var(self) -> None:
        with patch.dict(os.environ, {"KONVU_ZITADEL_CLIENT_ID": "cli-client"}, clear=False):
            assert get_zitadel_client_id() == "cli-client"

    def test_zitadel_client_id_fallback_to_cli_var(self) -> None:
        env = {"ZITADEL_CLI_CLIENT_ID": "cli-client-from-env"}
        with patch.dict(os.environ, env, clear=True):
            assert get_zitadel_client_id() == "cli-client-from-env"
