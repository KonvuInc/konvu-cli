import json
from typing import Any
from unittest.mock import MagicMock, patch

from typer.testing import CliRunner

from konvu_cli.main import app

runner = CliRunner()


def _extract_json(output: str) -> dict[str, Any]:
    """Extract JSON object from mixed stdout/stderr output.

    Typer's CliRunner mixes stderr into stdout, so we need to find the JSON
    portion (starting with '{') from the output.
    """
    # Find the first '{' which starts the JSON output
    idx = output.find("{")
    if idx == -1:
        raise ValueError(f"No JSON found in output: {output!r}")
    result: dict[str, Any] = json.loads(output[idx:])
    return result


MOCK_FINDING_LIST_RESPONSE: dict[str, Any] = {
    "facets": [],
    "sort": "recommendation",
    "order": "desc",
    "total": 2,
    "per_page": 50,
    "page": 1,
    "items": [
        {
            "id": "finding-001",
            "manifest_location_id": "ml-001",
            "vulnerability_id": "GHSA-xxxx",
            "vulnerability": {
                "id": "GHSA-xxxx",
                "aliases": ["CVE-2024-1234"],
                "severity": "HIGH",
                "has_fix": "fixed",
                "summary": "Test vulnerability",
            },
            "manifest_location": {
                "id": "ml-001",
                "vcs_repository_url": "github:org/repo",
                "vcs_base_url": "https://github.com/org/repo",
                "location": "package.json",
            },
            "dependency": {"name": "lodash", "version": "4.17.20"},
            "source": {
                "id": "finding-001",
                "identifier": "42",
                "source_name": "dependabot",
                "state": "open",
                "remote_created_at": "2026-03-05T10:00:00Z",
            },
            "analyses": {
                "qualification_summary": "User input flows to vulnerable merge call.",
            },
            "calculated_recommendation": "to_fix",
        },
        {
            "id": "finding-002",
            "manifest_location_id": "ml-002",
            "vulnerability_id": "GHSA-yyyy",
            "vulnerability": {
                "id": "GHSA-yyyy",
                "aliases": ["CVE-2024-5678"],
                "severity": "MODERATE",
                "has_fix": "no_fix",
                "summary": "Another vulnerability",
            },
            "manifest_location": {
                "id": "ml-002",
                "vcs_repository_url": "github:org/other",
                "vcs_base_url": "https://github.com/org/other",
                "location": "requirements.txt",
            },
            "dependency": {"name": "requests", "version": "2.28.0"},
            "source": {
                "id": "finding-002",
                "identifier": "87",
                "source_name": "snyk",
                "state": "open",
                "remote_created_at": "2026-03-01T08:00:00Z",
            },
            "calculated_recommendation": "to_dismiss",
        },
    ],
}

MOCK_FINDING_DETAIL: dict[str, Any] = {
    "id": "finding-001",
    "manifest_location_id": "ml-001",
    "vulnerability_id": "GHSA-xxxx",
    "vulnerability": {
        "id": "GHSA-xxxx",
        "aliases": ["CVE-2024-1234"],
        "severity": "HIGH",
        "summary": "Test vulnerability summary",
        "cvss": ["CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N"],
        "epss": {"score": 0.5, "percentile": 0.9},
    },
    "manifest_location": {
        "id": "ml-001",
        "vcs_repository_url": "github:org/repo",
        "location": "package.json",
    },
    "dependency": {"name": "lodash", "version": "4.17.20"},
    "source": {"id": "finding-001", "state": "open"},
    "analyses": {
        "assessment_status": "completed",
        "qualification": {
            "id": "qual-001",
            "outcome": "applicable",
            "summary": "The vulnerable function is called in production code paths.",
            "checklist": {
                "items": [
                    {
                        "description": "Attacker-controlled input reaches function",
                        "status": "completed",
                        "check_conclusion": "User input flows to vulnerable call.",
                        "investigation_steps": ["Checked routes", "Traced data flow"],
                        "proofs": [
                            {
                                "file": "src/handler.js",
                                "line": 42,
                                "code": "lodash.merge(req.body)",
                                "comment": "Direct user input",
                            }
                        ],
                    }
                ],
            },
        },
        "runtime_reachability": {"some": "data"},
    },
    "calculated_recommendation": "to_fix",
    "latest_recommendation": {
        "confidence_score": 0.85,
        "recommendation_reason": "ai_qualification_applicable",
    },
    "analysis_events": [
        {"date": "2026-03-01T10:00:00Z", "event": "started", "analysis_type": "qualification"},
    ],
}

MOCK_FINDING_LOGS: dict[str, Any] = {
    "logs": {},
    "recommendation_decisions": [
        {
            "recommendation_type": "to_fix",
            "recommendation_reason": "ai_qualification_applicable",
            "confidence_score": 0.85,
            "confidence_factors": {"ai": {"base_score": 0.9}, "final_confidence": 0.85},
            "decision_logic_version": "v1.8.0",
            "created_at": "2026-03-01T10:05:00Z",
        }
    ],
}


def _mock_client(get_responses: dict[str, Any] | None = None) -> MagicMock:
    """Create a mock KonvuClient context manager."""
    mock = MagicMock()
    responses = get_responses or {}

    def mock_get(path: str, params: Any = None) -> Any:
        for key, val in responses.items():
            if path.startswith(key):
                return val
        return MOCK_FINDING_LIST_RESPONSE

    mock.__enter__ = MagicMock(return_value=mock)
    mock.__exit__ = MagicMock(return_value=False)
    mock.get = MagicMock(side_effect=mock_get)
    return mock


# --- version command ---


class TestVersion:
    def test_version_text(self) -> None:
        result = runner.invoke(app, ["version"])
        assert result.exit_code == 0
        assert "0.1.0" in result.output

    def test_version_json(self) -> None:
        result = runner.invoke(app, ["version", "--output", "json"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert "version" in data
        assert "api_url" in data


# --- finding list ---


class TestFindingList:
    def test_help(self) -> None:
        result = runner.invoke(app, ["finding", "list", "--help"])
        assert result.exit_code == 0
        assert "--severity" in result.output
        assert "--since" in result.output
        assert "--sort" in result.output
        assert "--output" in result.output
        assert "--quiet" in result.output
        assert "--fields" in result.output
        assert "--count" in result.output
        assert "--has-fix" in result.output

    def test_list_json(self) -> None:
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "list", "--output", "json"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert "findings" in data
        assert "summary" in data
        assert len(data["findings"]) == 2
        assert data["findings"][0]["id"] == "finding-001"
        assert data["findings"][0]["assessment"] == "exploitable"
        assert data["findings"][0]["scanner"] == "dependabot"
        assert data["findings"][0]["source_id"] == "42"

    def test_list_json_has_assessment_summary(self) -> None:
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "list", "--output", "json"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        # Finding with backend qualification_summary uses it
        exploitable = data["findings"][0]
        assert exploitable["assessment_summary"] == "User input flows to vulnerable merge call."
        assert "assessment_next_steps" not in exploitable
        # Finding without qualification_summary falls back to generic mapping
        false_positive = data["findings"][1]
        assert false_positive["assessment_summary"] == "Not exploitable in your context."

    def test_list_json_summary_has_assessment_breakdown(self) -> None:
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "list", "--output", "json"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        summary = data["summary"]
        assert summary["total"] == 2
        assert summary["showing"] == 2
        assert "assessment_breakdown" in summary
        assert summary["assessment_breakdown"]["exploitable"] == 1
        assert summary["assessment_breakdown"]["false-positive"] == 1

    def test_list_quiet(self) -> None:
        mock = _mock_client()
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "list", "--quiet"])
        assert result.exit_code == 0
        lines = result.output.strip().split("\n")
        assert lines == ["finding-001", "finding-002"]

    def test_list_since(self) -> None:
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "list", "--since", "7d", "--output", "json"])
        assert result.exit_code == 0
        call_args = mock.get.call_args_list
        findings_calls = [c for c in call_args if c[0][0] == "/sca_findings"]
        assert len(findings_calls) == 1
        params = findings_calls[0][1]["params"]
        assert "first_seen_after" in params

    def test_list_sort_order(self) -> None:
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app,
                [
                    "finding",
                    "list",
                    "--sort",
                    "first_seen_at",
                    "--order",
                    "asc",
                    "--output",
                    "json",
                ],
            )
        assert result.exit_code == 0
        call_args = mock.get.call_args_list
        findings_calls = [c for c in call_args if c[0][0] == "/sca_findings"]
        params = findings_calls[0][1]["params"]
        assert params["sort"] == "first_seen_at"
        assert params["order"] == "asc"

    def test_list_fields(self) -> None:
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app, ["finding", "list", "--fields", "id,cve", "--output", "json"]
            )
        assert result.exit_code == 0
        data = json.loads(result.output)
        first = data["findings"][0]
        assert "id" in first
        assert "cve" in first
        assert "repository" not in first

    def test_list_default_limit_50(self) -> None:
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            runner.invoke(app, ["finding", "list", "--output", "json"])
        call_args = mock.get.call_args_list
        findings_calls = [c for c in call_args if c[0][0] == "/sca_findings"]
        params = findings_calls[0][1]["params"]
        assert params["per_page"] == 50


class TestFindingListGroupBy:
    def test_group_by_repository(self) -> None:
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app, ["finding", "list", "--group-by", "repository", "--output", "json"]
            )
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert "groups" in data
        assert data["summary"]["group_by"] == "repository"
        assert data["summary"]["groups"] == 2
        # Sorted by count desc
        keys = [g["key"] for g in data["groups"]]
        assert len(keys) == 2
        # Each group has findings
        for g in data["groups"]:
            assert g["count"] > 0
            assert len(g["findings"]) == g["count"]

    def test_group_by_dependency(self) -> None:
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app, ["finding", "list", "--group-by", "dependency", "--output", "json"]
            )
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert data["summary"]["group_by"] == "dependency"
        keys = [g["key"] for g in data["groups"]]
        assert "lodash" in keys
        assert "requests" in keys

    def test_group_by_with_fields(self) -> None:
        """Field filtering should apply within groups, not strip group keys."""
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app,
                [
                    "finding",
                    "list",
                    "--group-by",
                    "repository",
                    "--fields",
                    "id,cve",
                    "--output",
                    "json",
                ],
            )
        assert result.exit_code == 0
        data = json.loads(result.output)
        # Group key should still be present
        assert data["groups"][0]["key"] != ""
        # But findings inside should be filtered
        first_finding = data["groups"][0]["findings"][0]
        assert "id" in first_finding
        assert "cve" in first_finding
        assert "repository" not in first_finding

    def test_group_by_invalid(self) -> None:
        result = runner.invoke(app, ["finding", "list", "--group-by", "cve"])
        assert result.exit_code == 2


class TestFindingCount:
    def test_count_flag_outputs_number(self) -> None:
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "list", "--count"])
        assert result.exit_code == 0
        assert result.output.strip() == "2"

    def test_count_with_filters(self) -> None:
        response = {**MOCK_FINDING_LIST_RESPONSE, "total": 42}
        mock = _mock_client({"/sca_findings": response})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "list", "--severity", "critical", "--count"])
        assert result.exit_code == 0
        assert result.output.strip() == "42"


class TestFindingHasFix:
    def test_has_fix_filter(self) -> None:
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app, ["finding", "list", "--has-fix", "fixed", "--output", "json"]
            )
        assert result.exit_code == 0
        call_args = mock.get.call_args_list
        findings_calls = [c for c in call_args if c[0][0] == "/sca_findings"]
        params = findings_calls[0][1]["params"]
        assert params["has_fix"] == "fixed"

    def test_has_fix_in_output(self) -> None:
        mock = _mock_client({"/sca_findings": MOCK_FINDING_LIST_RESPONSE})
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "list", "--output", "json"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert data["findings"][0]["has_fix"] == "fixed"
        assert data["findings"][1]["has_fix"] == "no_fix"


# --- finding get ---


class TestFindingGet:
    def test_help(self) -> None:
        result = runner.invoke(app, ["finding", "get", "--help"])
        assert result.exit_code == 0
        assert "--include" in result.output
        assert "--output" in result.output

    def test_get_json(self) -> None:
        mock = _mock_client()
        mock.get = MagicMock(return_value=MOCK_FINDING_DETAIL)
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "get", "finding-001", "--output", "json"])
        assert result.exit_code == 0
        data = _extract_json(result.output)
        assert data["id"] == "finding-001"
        assert data["assessment"]["status"] == "exploitable"
        assert data["assessment"]["confidence"] == 0.85
        assert (
            data["qualification_summary"]
            == "The vulnerable function is called in production code paths."
        )

    def test_get_with_evidence(self) -> None:
        mock = _mock_client()
        mock.get = MagicMock(return_value=MOCK_FINDING_DETAIL)
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app, ["finding", "get", "finding-001", "--include", "evidence", "--output", "json"]
            )
        assert result.exit_code == 0
        data = _extract_json(result.output)
        assert "evidence" in data
        assert len(data["evidence"]["checklist"]) == 1
        assert data["evidence"]["checklist"][0]["proofs"][0]["file"] == "src/handler.js"

    def test_get_with_logs(self) -> None:
        mock = _mock_client()

        def mock_get(path: str, **kwargs: Any) -> dict[str, Any]:
            if "/logs" in path:
                return MOCK_FINDING_LOGS
            return MOCK_FINDING_DETAIL

        mock.get = MagicMock(side_effect=mock_get)
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app, ["finding", "get", "finding-001", "--include", "logs", "--output", "json"]
            )
        assert result.exit_code == 0
        data = _extract_json(result.output)
        assert "logs" in data
        assert data["logs"]["recommendation_decisions"][0]["confidence"] == 0.85

    def test_get_fields_filter(self) -> None:
        mock = _mock_client()
        mock.get = MagicMock(return_value=MOCK_FINDING_DETAIL)
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app,
                ["finding", "get", "finding-001", "--fields", "id,assessment", "--output", "json"],
            )
        assert result.exit_code == 0
        data = _extract_json(result.output)
        assert set(data.keys()) == {"id", "assessment"}

    def test_get_handles_null_qualification(self) -> None:
        """Should not crash when analyses.qualification is null."""
        detail_no_qual = {
            **MOCK_FINDING_DETAIL,
            "analyses": {
                "assessment_status": "completed",
                "qualification": None,
            },
            "latest_recommendation": None,
        }
        mock = _mock_client()
        mock.get = MagicMock(return_value=detail_no_qual)
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "get", "finding-001", "--output", "json"])
        assert result.exit_code == 0
        data = _extract_json(result.output)
        assert data["qualification_summary"] == ""


# --- finding counts ---


class TestFindingCounts:
    def test_help(self) -> None:
        result = runner.invoke(app, ["finding", "counts", "--help"])
        assert result.exit_code == 0
        assert "--since" in result.output
        assert "--severity" in result.output
        assert "--repo" in result.output
        assert "--group-by" in result.output

    def test_counts_json_shows_assessment_categories(self) -> None:
        """Counts should return assessment categories, not pipeline statuses."""

        def mock_get(path: str, params: Any = None) -> Any:
            if path == "/sca_findings":
                recs = params.get("recommendation", [])
                if "to_fix" in recs:
                    return {"total": 10, "items": []}
                if "to_dismiss" in recs:
                    return {"total": 20, "items": []}
                if "no_qualification" in recs:
                    return {"total": 100, "items": []}
                return {"total": 5, "items": []}
            return {}

        mock = MagicMock()
        mock.__enter__ = MagicMock(return_value=mock)
        mock.__exit__ = MagicMock(return_value=False)
        mock.get = MagicMock(side_effect=mock_get)

        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "counts", "--output", "json"])
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert "exploitable" in data
        assert "false-positive" in data
        assert "inconclusive" in data
        assert "not-assessed" in data
        assert "total" in data
        assert data["exploitable"] == 10
        assert data["false-positive"] == 20

    def test_counts_with_since_filter(self) -> None:
        def mock_get(path: str, params: Any = None) -> Any:
            assert "first_seen_after" in (params or {})
            return {"total": 1, "items": []}

        mock = MagicMock()
        mock.__enter__ = MagicMock(return_value=mock)
        mock.__exit__ = MagicMock(return_value=False)
        mock.get = MagicMock(side_effect=mock_get)

        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "counts", "--since", "7d", "--output", "json"])
        assert result.exit_code == 0

    def test_counts_group_by_severity(self) -> None:
        """--group-by severity should return assessment × severity matrix."""

        def mock_get(path: str, params: Any = None) -> Any:
            if path == "/sca_findings":
                sev = (params or {}).get("severity", [None])[0]
                recs = (params or {}).get("recommendation", [])
                # Critical exploitable = 5, rest = 0
                if sev == "CRITICAL" and "to_fix" in recs:
                    return {"total": 5, "items": []}
                if sev == "HIGH" and "to_dismiss" in recs:
                    return {"total": 3, "items": []}
                return {"total": 0, "items": []}
            return {}

        mock = MagicMock()
        mock.__enter__ = MagicMock(return_value=mock)
        mock.__exit__ = MagicMock(return_value=False)
        mock.get = MagicMock(side_effect=mock_get)

        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app, ["finding", "counts", "--group-by", "severity", "--output", "json"]
            )
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert data["group_by"] == "severity"
        assert "rows" in data
        # Only rows with total > 0 should appear
        severities = {r["severity"] for r in data["rows"]}
        assert "critical" in severities
        assert "high" in severities
        # Find critical row
        critical = next(r for r in data["rows"] if r["severity"] == "critical")
        assert critical["exploitable"] == 5
        assert critical["total"] == 5

    def test_counts_group_by_week(self) -> None:
        """--group-by week should return assessment counts per week."""

        def mock_get(path: str, params: Any = None) -> Any:
            if path == "/sca_findings":
                recs = (params or {}).get("recommendation", [])
                if "to_fix" in recs:
                    return {"total": 2, "items": []}
                return {"total": 0, "items": []}
            return {}

        mock = MagicMock()
        mock.__enter__ = MagicMock(return_value=mock)
        mock.__exit__ = MagicMock(return_value=False)
        mock.get = MagicMock(side_effect=mock_get)

        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app, ["finding", "counts", "--group-by", "week", "--output", "json"]
            )
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert data["group_by"] == "week"
        assert "rows" in data
        assert len(data["rows"]) == 4  # default 4 weeks
        assert all("period" in r for r in data["rows"])
        assert all("week of" in r["period"] for r in data["rows"])
        # Each week should have exploitable=2 based on mock
        assert data["rows"][0]["exploitable"] == 2

    def test_counts_group_by_month(self) -> None:
        """--group-by month should return assessment counts per month."""

        def mock_get(path: str, params: Any = None) -> Any:
            if path == "/sca_findings":
                recs = (params or {}).get("recommendation", [])
                if "to_dismiss" in recs:
                    return {"total": 7, "items": []}
                return {"total": 0, "items": []}
            return {}

        mock = MagicMock()
        mock.__enter__ = MagicMock(return_value=mock)
        mock.__exit__ = MagicMock(return_value=False)
        mock.get = MagicMock(side_effect=mock_get)

        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app, ["finding", "counts", "--group-by", "month", "--output", "json"]
            )
        assert result.exit_code == 0
        data = json.loads(result.output)
        assert data["group_by"] == "month"
        assert "rows" in data
        assert len(data["rows"]) == 3  # default 3 months
        assert all("period" in r for r in data["rows"])
        # Period labels should be YYYY-MM format
        assert all(len(r["period"]) == 7 for r in data["rows"])
        assert data["rows"][0]["false-positive"] == 7

    def test_counts_group_by_week_with_since(self) -> None:
        """--since should control how many weeks back to go."""

        def mock_get(path: str, params: Any = None) -> Any:
            return {"total": 1, "items": []}

        mock = MagicMock()
        mock.__enter__ = MagicMock(return_value=mock)
        mock.__exit__ = MagicMock(return_value=False)
        mock.get = MagicMock(side_effect=mock_get)

        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(
                app,
                ["finding", "counts", "--group-by", "week", "--since", "14d", "--output", "json"],
            )
        assert result.exit_code == 0
        data = json.loads(result.output)
        # 14 days = ~2-3 weeks
        assert len(data["rows"]) >= 2

    def test_counts_group_by_invalid(self) -> None:
        mock = MagicMock()
        mock.__enter__ = MagicMock(return_value=mock)
        mock.__exit__ = MagicMock(return_value=False)
        with patch("konvu_cli.commands.finding.KonvuClient", return_value=mock):
            result = runner.invoke(app, ["finding", "counts", "--group-by", "cve"])
        assert result.exit_code == 2
