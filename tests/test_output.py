import json
from unittest.mock import patch

from konvu_cli.output.detection import OutputFormat, detect_output_format
from konvu_cli.output.formatters import (
    filter_fields,
    format_csv,
    format_json,
    format_quiet,
    format_table,
)

# --- detect_output_format tests ---


def test_explicit_json_format() -> None:
    assert detect_output_format("json") == OutputFormat.JSON


def test_explicit_table_format() -> None:
    assert detect_output_format("table") == OutputFormat.TABLE


def test_explicit_csv_format() -> None:
    assert detect_output_format("csv") == OutputFormat.CSV


def test_auto_format_tty_returns_table() -> None:
    with patch("konvu_cli.output.detection.sys.stdout.isatty", return_value=True):
        assert detect_output_format(None) == OutputFormat.TABLE


def test_auto_format_pipe_returns_json() -> None:
    with patch("konvu_cli.output.detection.sys.stdout.isatty", return_value=False):
        assert detect_output_format(None) == OutputFormat.JSON


# --- format_json tests ---


def test_format_json_returns_valid_json() -> None:
    data = {"findings": [{"id": "123", "cve": "CVE-2024-1234"}]}
    result = format_json(data)
    parsed = json.loads(result)
    assert parsed == data


def test_format_json_pretty_prints() -> None:
    data = {"key": "value"}
    result = format_json(data)
    assert "\n" in result


# --- format_table tests ---


def test_format_table_returns_string() -> None:
    data = {
        "findings": [
            {"id": "123", "cve": "CVE-2024-1234", "severity": "critical"},
            {"id": "456", "cve": "CVE-2024-5678", "severity": "high"},
        ]
    }
    result = format_table(data, columns=["id", "cve", "severity"], list_key="findings")
    assert "CVE-2024-1234" in result
    assert "critical" in result


# --- format_csv tests ---


def test_format_csv_returns_csv() -> None:
    data = {
        "findings": [
            {"id": "123", "cve": "CVE-2024-1234"},
            {"id": "456", "cve": "CVE-2024-5678"},
        ]
    }
    result = format_csv(data, columns=["id", "cve"], list_key="findings")
    assert "id,cve" in result
    assert "123,CVE-2024-1234" in result


# --- format_quiet tests ---


def test_format_quiet_outputs_one_id_per_line() -> None:
    items = [{"id": "abc"}, {"id": "def"}, {"id": "ghi"}]
    result = format_quiet(items, id_field="id")
    assert result == "abc\ndef\nghi"


def test_format_quiet_empty_list() -> None:
    result = format_quiet([], id_field="id")
    assert result == ""


# --- filter_fields tests ---


def test_filter_fields_keeps_only_requested() -> None:
    data = {"id": "123", "cve": "CVE-X", "severity": "high", "extra": "gone"}
    result = filter_fields(data, ["id", "cve"])
    assert result == {"id": "123", "cve": "CVE-X"}


def test_filter_fields_none_returns_original() -> None:
    data = {"id": "123", "cve": "CVE-X"}
    result = filter_fields(data, None)
    assert result == data
