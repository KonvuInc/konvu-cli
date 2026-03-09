"""Structured errors and semantic exit codes for the Konvu CLI."""

import json
from typing import Any

EXIT_GENERAL_ERROR = 1
EXIT_USAGE_ERROR = 2
EXIT_NOT_FOUND = 3
EXIT_AUTH_FAILED = 4


class CLIError(Exception):
    """Structured CLI error with code, suggestion, and exit code."""

    def __init__(
        self,
        code: str,
        message: str,
        suggestion: str = "",
        retryable: bool = False,
        exit_code: int = EXIT_GENERAL_ERROR,
    ):
        super().__init__(message)
        self.code = code
        self.suggestion = suggestion
        self.retryable = retryable
        self.exit_code = exit_code


def format_error_json(err: CLIError) -> str:
    """Format a CLIError as a JSON string for --output json mode."""
    obj: dict[str, Any] = {
        "error": {
            "code": err.code,
            "message": str(err),
            "suggestion": err.suggestion,
            "retryable": err.retryable,
        }
    }
    return json.dumps(obj, indent=2)
