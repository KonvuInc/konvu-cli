import sys
from enum import Enum


class OutputFormat(str, Enum):
    JSON = "json"
    TABLE = "table"
    CSV = "csv"


def detect_output_format(explicit_format: str | None) -> OutputFormat:
    """Detect output format based on explicit flag or TTY detection.

    - Explicit format always wins
    - If stdout is a TTY (interactive terminal), use table
    - If stdout is piped/redirected, use JSON (machine-readable)
    """
    if explicit_format:
        return OutputFormat(explicit_format.lower())

    if sys.stdout.isatty():
        return OutputFormat.TABLE

    return OutputFormat.JSON
