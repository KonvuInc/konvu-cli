import json

from konvu_cli.errors import (
    EXIT_AUTH_FAILED,
    EXIT_GENERAL_ERROR,
    EXIT_NOT_FOUND,
    EXIT_USAGE_ERROR,
    CLIError,
    format_error_json,
)


def test_exit_code_constants() -> None:
    assert EXIT_GENERAL_ERROR == 1
    assert EXIT_USAGE_ERROR == 2
    assert EXIT_NOT_FOUND == 3
    assert EXIT_AUTH_FAILED == 4


def test_cli_error_attributes() -> None:
    err = CLIError(
        code="FINDING_NOT_FOUND",
        message="Finding 'abc' not found",
        suggestion="Run 'konvu finding list'",
        retryable=False,
        exit_code=EXIT_NOT_FOUND,
    )
    assert err.code == "FINDING_NOT_FOUND"
    assert err.exit_code == 3
    assert str(err) == "Finding 'abc' not found"


def test_format_error_json() -> None:
    err = CLIError(
        code="AUTH_FAILED",
        message="Session expired",
        suggestion="Run 'konvu login'",
        retryable=False,
        exit_code=EXIT_AUTH_FAILED,
    )
    result = json.loads(format_error_json(err))
    assert result["error"]["code"] == "AUTH_FAILED"
    assert result["error"]["message"] == "Session expired"
    assert result["error"]["suggestion"] == "Run 'konvu login'"
    assert result["error"]["retryable"] is False
