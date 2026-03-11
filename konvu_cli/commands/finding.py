# konvu_cli/commands/finding.py
"""Security findings commands."""

import re
from collections import Counter
from datetime import UTC, datetime, timedelta
from typing import Any

import typer

from konvu_cli.api.client import APIError, AuthenticationError, KonvuClient
from konvu_cli.errors import (
    EXIT_AUTH_FAILED,
    EXIT_NOT_FOUND,
    EXIT_USAGE_ERROR,
    CLIError,
    format_error_json,
)
from konvu_cli.mapping import (
    AssessmentStatus,
    assessment_to_recommendation,
    get_assessment_summary,
    recommendation_to_assessment,
)
from konvu_cli.output.detection import OutputFormat, detect_output_format
from konvu_cli.output.formatters import (
    filter_fields,
    format_csv,
    format_json,
    format_quiet,
    format_table,
)

app = typer.Typer(help="Security findings")

VALID_COUNTS_GROUP_BY = {"severity", "week", "month"}
VALID_LIST_GROUP_BY = {"repository", "dependency", "severity", "assessment"}


def _parse_relative_date(value: str) -> str:
    """Parse relative date ('7d', '30d') to ISO 8601 string."""
    match = re.match(r"^(\d+)d$", value)
    if match:
        days = int(match.group(1))
        dt = datetime.now(UTC) - timedelta(days=days)
        return dt.isoformat()
    return value


def _generate_time_periods(
    group_by: str,
    since: str | None = None,
) -> list[dict[str, Any]]:
    """Generate time period windows for week/month grouping.

    Returns list of dicts with 'label', 'start', 'end' (ISO strings).
    Most recent period first.
    """
    now = datetime.now(UTC)

    if group_by == "week":
        default_periods = 4
        # Align to Monday of current week
        today = now.replace(hour=0, minute=0, second=0, microsecond=0)
        current_monday = today - timedelta(days=today.weekday())
        periods = []
        if since:
            start_date = datetime.fromisoformat(_parse_relative_date(since))
            n = max(1, int((now - start_date).days // 7) + 1)
        else:
            n = default_periods
        for i in range(n):
            week_start = current_monday - timedelta(weeks=i)
            week_end = week_start + timedelta(days=7)
            label = week_start.strftime("%Y-%m-%d")
            periods.append(
                {
                    "label": f"week of {label}",
                    "start": week_start.isoformat(),
                    "end": week_end.isoformat(),
                }
            )
        return periods

    if group_by == "month":
        default_periods = 3
        if since:
            start_date = datetime.fromisoformat(_parse_relative_date(since))
            n = max(1, (now.year - start_date.year) * 12 + now.month - start_date.month + 1)
        else:
            n = default_periods
        periods = []
        for i in range(n):
            # Go back i months from current month
            month = now.month - i
            year = now.year
            while month < 1:
                month += 12
                year -= 1
            month_start = datetime(year, month, 1, tzinfo=UTC)
            # Next month start
            next_month = month + 1
            next_year = year
            if next_month > 12:
                next_month = 1
                next_year += 1
            month_end = datetime(next_year, next_month, 1, tzinfo=UTC)
            label = month_start.strftime("%Y-%m")
            periods.append(
                {
                    "label": label,
                    "start": month_start.isoformat(),
                    "end": month_end.isoformat(),
                }
            )
        return periods

    return []


def _transform_finding(finding: dict[str, Any]) -> dict[str, Any]:
    """Transform API finding to CLI output format."""
    vuln = finding.get("vulnerability", {})
    ml = finding.get("manifest_location", {})
    dep = finding.get("dependency", {})
    source = finding.get("source", {})
    rec = finding.get("calculated_recommendation")
    assessment = recommendation_to_assessment(rec)

    analyses = finding.get("analyses") or {}

    aliases = vuln.get("aliases") or []
    cve = aliases[0] if aliases else vuln.get("id", "")

    # Use the backend's per-finding summary; fall back to generic mapping
    qualification_summary = analyses.get("qualification_summary") or ""
    if not qualification_summary:
        qualification_summary, _ = get_assessment_summary(assessment)

    return {
        "id": finding.get("id", ""),
        "cve": cve,
        "severity": (vuln.get("severity") or "unknown").lower(),
        "dependency": dep.get("name", ""),
        "repository": ml.get("vcs_repository_url", ""),
        "manifest": ml.get("location", ""),
        "assessment": assessment.value,
        "assessment_summary": qualification_summary,
        "has_fix": (vuln.get("has_fix") or "unknown").lower(),
        "first_seen": source.get("remote_created_at", ""),
        "state": source.get("state", ""),
        "source_id": source.get("identifier", ""),
        "scanner": source.get("source_name", ""),
    }


def _compute_assessment_counts(
    client: KonvuClient,
    base_params: dict[str, Any] | None = None,
) -> dict[str, int]:
    """Compute assessment category counts by querying with recommendation filters."""
    counts: dict[str, int] = {}
    for status in AssessmentStatus:
        recs = assessment_to_recommendation(status)
        params: dict[str, Any] = {"per_page": 1, "recommendation": recs}
        if base_params:
            params.update(base_params)
            params["recommendation"] = recs  # always override with current category
        data = client.get("/sca_findings", params=params)
        counts[status.value] = data.get("total", 0)
    return counts


def _handle_error(err: Exception, output_format: OutputFormat) -> None:
    """Handle errors with structured output."""
    if isinstance(err, CLIError):
        cli_err = err
    elif isinstance(err, AuthenticationError):
        cli_err = CLIError(
            code="AUTH_FAILED",
            message=str(err),
            suggestion="Run 'konvu login' to authenticate.",
            retryable=False,
            exit_code=EXIT_AUTH_FAILED,
        )
    else:
        cli_err = CLIError(
            code="API_ERROR",
            message=str(err),
            suggestion="Check 'konvu whoami' to verify your session.",
            retryable=True,
        )

    if output_format == OutputFormat.JSON:
        typer.echo(format_error_json(cli_err))
    else:
        typer.echo(f"Error: {cli_err}", err=True)
        if cli_err.suggestion:
            typer.echo(f"  {cli_err.suggestion}", err=True)
    raise typer.Exit(cli_err.exit_code)


@app.command("list")
def list_findings(
    since: str | None = typer.Option(None, "--since", help="Start date: '7d', '30d', or ISO date"),
    until: str | None = typer.Option(None, "--until", help="End date: 'now' or ISO date"),
    severity: list[str] | None = typer.Option(
        None, "--severity", "-s", help="Filter: critical,high,moderate,low"
    ),
    assessment: list[str] | None = typer.Option(
        None,
        "--assessment",
        "-a",
        help="Filter: exploitable,false-positive,inconclusive,not-assessed",
    ),
    state: list[str] | None = typer.Option(
        None, "--state", help="Filter: open,dismissed,fixed,muted"
    ),
    has_fix: str | None = typer.Option(None, "--has-fix", help="Filter: fixed, no_fix"),
    repo: str | None = typer.Option(None, "--repo", "-r", help="Filter by repository URL or name"),
    cve: str | None = typer.Option(None, "--cve", help="Filter by CVE ID"),
    dependency: str | None = typer.Option(
        None, "--dependency", "-d", help="Filter by dependency name"
    ),
    sort: str = typer.Option(
        "recommendation",
        "--sort",
        help="Sort: severity,recommendation,first_seen_at,updated_at,dependency_name,cve",
    ),
    order: str = typer.Option("desc", "--order", help="Order: asc,desc"),
    limit: int = typer.Option(50, "--limit", "-n", help="Maximum findings to return"),
    offset: int = typer.Option(0, "--offset", help="Skip N results"),
    output: str | None = typer.Option(
        None, "--output", "-o", help="Output format: json, table, csv"
    ),
    quiet: bool = typer.Option(False, "--quiet", "-q", help="Output bare finding IDs only"),
    count: bool = typer.Option(False, "--count", help="Output only the total count"),
    group_by: str | None = typer.Option(
        None,
        "--group-by",
        "-g",
        help="Group results by: repository, dependency, severity, assessment",
    ),
    fields: str | None = typer.Option(
        None, "--fields", help="Comma-separated fields to include in JSON output"
    ),
) -> None:
    """List security findings with filtering and sorting.

    \b
    Examples:
      # This week's exploitable findings
      konvu finding list --since 7d --assessment exploitable

      # Critical findings sorted by recency
      konvu finding list --severity critical --sort first_seen_at --output json

      # Just the count of not-assessed findings
      konvu finding list --assessment not-assessed --count

      # Findings with available fixes
      konvu finding list --has-fix fixed --assessment exploitable

      # Group exploitable findings by repo to prioritize
      konvu finding list --assessment exploitable --group-by repository

      # Pipe finding IDs to detail
      konvu finding list --assessment exploitable -q | xargs -I {} konvu finding get {}

    \b
    Exit codes:
      0  Success
      1  General error
      2  Invalid arguments
      4  Authentication failed
    """
    output_format = detect_output_format(output)

    if group_by and group_by not in VALID_LIST_GROUP_BY:
        typer.echo(
            f"Invalid group-by: {group_by}. Valid: {', '.join(sorted(VALID_LIST_GROUP_BY))}",
            err=True,
        )
        raise typer.Exit(EXIT_USAGE_ERROR)

    try:
        with KonvuClient() as client:
            params: dict[str, Any] = {
                "per_page": min(limit, 1000),
                "page": (offset // max(limit, 1)) + 1,
                "sort": sort,
                "order": order,
            }

            if since:
                params["first_seen_after"] = _parse_relative_date(since)
            if until and until != "now":
                params["first_seen_before"] = _parse_relative_date(until)
            if severity:
                params["severity"] = [s.upper() for s in severity]
            if assessment:
                recommendations: list[str] = []
                for a in assessment:
                    try:
                        status = AssessmentStatus(a.lower().replace("_", "-"))
                        recommendations.extend(assessment_to_recommendation(status))
                    except ValueError:
                        typer.echo(f"Invalid assessment value: {a}", err=True)
                        raise typer.Exit(EXIT_USAGE_ERROR)
                params["recommendation"] = recommendations
            if state:
                params["any_source_state"] = state
            if has_fix:
                params["has_fix"] = has_fix
            if repo:
                params["vcs_repository_url"] = [repo]
            if cve:
                params["cve"] = [cve]
            if dependency:
                params["dependency_name"] = [dependency]

            data = client.get("/sca_findings", params=params)
            total = data.get("total", 0)

            if count:
                typer.echo(str(total))
                return

            items = data.get("items", [])
            transformed = [_transform_finding(f) for f in items]

            if quiet:
                typer.echo(format_quiet(transformed, id_field="id"))
                return

            # Compute assessment breakdown from returned items
            assessment_breakdown = dict(Counter(f["assessment"] for f in transformed))

            showing = len(transformed)

            if group_by:
                # Group findings by the requested field
                groups: dict[str, list[dict[str, Any]]] = {}
                for f in transformed:
                    key = f.get(group_by, "unknown") or "unknown"
                    groups.setdefault(key, []).append(f)

                # Apply field filtering within each group
                field_list = [f.strip() for f in fields.split(",")] if fields else None

                grouped_result: list[dict[str, Any]] = []
                for key in sorted(groups, key=lambda k: (-len(groups[k]), k)):
                    group_findings = groups[key]
                    if field_list:
                        group_findings = [filter_fields(f, field_list) for f in group_findings]
                    grouped_result.append(
                        {
                            "key": key,
                            "count": len(groups[key]),
                            "findings": group_findings,
                        }
                    )

                result: dict[str, Any] = {
                    "summary": {
                        "total": total,
                        "showing": showing,
                        "offset": offset,
                        "group_by": group_by,
                        "groups": len(grouped_result),
                        "assessment_breakdown": assessment_breakdown,
                    },
                    "groups": grouped_result,
                }

                if output_format == OutputFormat.JSON:
                    typer.echo(format_json(result))
                elif output_format == OutputFormat.CSV:
                    # Flatten groups for CSV: add group key column
                    flat: list[dict[str, Any]] = []
                    for g in grouped_result:
                        for f in g["findings"]:
                            flat.append({group_by: g["key"], **f})
                    typer.echo(
                        format_csv(
                            {"findings": flat},
                            columns=[group_by, "id", "cve", "severity", "dependency", "assessment"],
                            list_key="findings",
                        )
                    )
                else:
                    typer.echo(f"\nShowing {showing} of {total} findings", err=True)
                    typer.echo(f"  Grouped by {group_by}: {len(grouped_result)} groups", err=True)
                    if assessment_breakdown:
                        parts = [f"{k}: {v}" for k, v in sorted(assessment_breakdown.items())]
                        typer.echo(f"  Assessment: {', '.join(parts)}", err=True)
                    typer.echo("", err=True)
                    for g in grouped_result:
                        typer.echo(f"  {g['key']} ({g['count']})")
                    typer.echo("")
                    # Also show the full table
                    flat_for_table: list[dict[str, Any]] = []
                    for g in grouped_result:
                        for f in g["findings"]:
                            flat_for_table.append(f)
                    typer.echo(
                        format_table(
                            {"findings": flat_for_table},
                            columns=[
                                "cve",
                                "dependency",
                                "repository",
                                "scanner",
                                "source_id",
                                "assessment_summary",
                            ],
                            list_key="findings",
                        )
                    )
            else:
                field_list = [f.strip() for f in fields.split(",")] if fields else None
                if field_list:
                    transformed = [filter_fields(f, field_list) for f in transformed]

                result = {
                    "summary": {
                        "total": total,
                        "showing": showing,
                        "offset": offset,
                        "assessment_breakdown": assessment_breakdown,
                    },
                    "findings": transformed,
                }

                if output_format == OutputFormat.JSON:
                    typer.echo(format_json(result))
                elif output_format == OutputFormat.CSV:
                    typer.echo(
                        format_csv(
                            result,
                            columns=[
                                "id",
                                "cve",
                                "severity",
                                "dependency",
                                "repository",
                                "assessment",
                                "assessment_summary",
                            ],
                            list_key="findings",
                        )
                    )
                else:
                    typer.echo(f"\nShowing {showing} of {total} findings", err=True)
                    if assessment_breakdown:
                        parts = [f"{k}: {v}" for k, v in sorted(assessment_breakdown.items())]
                        typer.echo(f"  Assessment: {', '.join(parts)}", err=True)
                    typer.echo("", err=True)
                    typer.echo(
                        format_table(
                            result,
                            columns=[
                                "cve",
                                "dependency",
                                "repository",
                                "scanner",
                                "source_id",
                                "assessment_summary",
                            ],
                            list_key="findings",
                        )
                    )

    except (AuthenticationError, APIError, CLIError) as e:
        _handle_error(e, output_format)


@app.command("get")
def get_finding(
    finding_id: str = typer.Argument(..., help="Finding ID"),
    include: list[str] | None = typer.Option(
        None, "--include", "-i", help="Include: evidence, logs"
    ),
    verbose: bool = typer.Option(False, "--verbose", "-v", help="Show all details for each check"),
    output: str | None = typer.Option(None, "--output", "-o", help="Output format: json, table"),
    fields: str | None = typer.Option(None, "--fields", help="Comma-separated fields to include"),
) -> None:
    """Get detailed information about a finding.

    \b
    Examples:
      # Basic finding detail
      konvu finding get abc-123

      # Include evidence (exploitability checklist, reachability)
      konvu finding get abc-123 --include evidence

      # Include recommendation decision logs
      konvu finding get abc-123 --include logs

      # Both
      konvu finding get abc-123 --include evidence --include logs --output json

    \b
    Exit codes:
      0  Success
      1  General error
      3  Finding not found
      4  Authentication failed
    """
    include = include or []
    if verbose and "evidence" not in include:
        include.append("evidence")
    output_format = detect_output_format(output)

    try:
        with KonvuClient() as client:
            typer.echo(f"Fetching finding {finding_id}...", err=True)

            try:
                detail = client.get(f"/sca_findings/{finding_id}")
            except APIError as e:
                if e.status_code == 404:
                    raise CLIError(
                        code="FINDING_NOT_FOUND",
                        message=f"Finding '{finding_id}' not found",
                        suggestion="Run 'konvu finding list' to see available findings.",
                        exit_code=EXIT_NOT_FOUND,
                    ) from e
                raise

            vuln = detail.get("vulnerability", {})
            ml = detail.get("manifest_location", {})
            dep = detail.get("dependency", {})
            analyses = detail.get("analyses", {})
            qual = analyses.get("qualification") or {}
            latest_rec = detail.get("latest_recommendation") or {}
            rec = detail.get("calculated_recommendation")
            assessment_status = recommendation_to_assessment(rec)

            # --- Assessment (Konvu's analysis of this finding) ---
            qualification_summary = analyses.get("qualification_summary") or ""
            if not qualification_summary:
                qualification_summary = qual.get("summary", "")

            checklist = qual.get("checklist", {})
            checklist_items = []
            for item in checklist.get("items", []):
                entry: dict[str, Any] = {
                    "description": item.get("description", ""),
                    "status": item.get("status", ""),
                    "conclusion": item.get("check_conclusion", ""),
                }
                if "evidence" in include:
                    entry["investigation_steps"] = item.get("investigation_steps", [])
                    entry["proofs"] = [
                        {
                            "file": p.get("file", ""),
                            "line": p.get("line"),
                            "code": p.get("code", ""),
                            "comment": p.get("comment", ""),
                        }
                        for p in item.get("proofs", [])
                    ]
                checklist_items.append(entry)

            carto = analyses.get("carto_evidence") or {}
            carto_applicable = carto.get("applicable")
            carto_summary = carto.get("summary", "")
            if carto_applicable is not None or carto_summary:
                applicable = carto_applicable
                if applicable is True:
                    conclusion = "Applicable"
                elif applicable is False:
                    conclusion = "Not applicable"
                else:
                    conclusion = str(applicable) if applicable is not None else ""
                if carto_summary:
                    conclusion = f"{conclusion} — {carto_summary}" if conclusion else carto_summary
                stack_entry: dict[str, Any] = {
                    "description": "Vulnerability applicable to dependency stack",
                    "status": "completed",
                    "conclusion": conclusion,
                }
                checklist_items.insert(0, stack_entry)

            assessment_section: dict[str, Any] = {
                "status": assessment_status.value,
                "summary": qualification_summary,
                "checklist": checklist_items,
            }

            # --- Finding (this specific instance) ---
            source = detail.get("source", {})
            finding_section: dict[str, Any] = {
                "id": detail.get("id"),
                "dependency": dep.get("name", ""),
                "repository": ml.get("vcs_repository_url", ""),
                "manifest": ml.get("location", ""),
                "scanner": source.get("source_name", ""),
                "source_id": source.get("identifier", ""),
                "state": source.get("state", ""),
                "first_seen": source.get("remote_created_at", ""),
            }

            # --- Vulnerability (fixed for any finding with same vuln ID) ---
            vuln_section: dict[str, Any] = {
                "cve": vuln.get("id"),
                "aliases": vuln.get("aliases", []),
                "severity": (vuln.get("severity") or "unknown").lower(),
                "summary": vuln.get("summary", ""),
                "has_fix": (vuln.get("has_fix") or "unknown").lower(),
                "cvss": vuln.get("cvss", []),
                "epss": vuln.get("epss"),
            }

            result: dict[str, Any] = {
                "assessment": assessment_section,
                "finding": finding_section,
                "vulnerability": vuln_section,
            }

            if "evidence" in include:
                reachability = analyses.get("runtime_reachability", {})
                result["assessment"]["reachability"] = reachability

            if "logs" in include:
                logs_data = client.get(f"/sca_findings/{finding_id}/logs")
                decisions = logs_data.get("recommendation_decisions", [])
                result["logs"] = {
                    "recommendation_decisions": [
                        {
                            "type": d.get("recommendation_type")
                            or d.get("raw_recommendation_type"),
                            "reason": d.get("recommendation_reason"),
                            "confidence": d.get("confidence_score"),
                            "confidence_factors": d.get("confidence_factors"),
                            "version": d.get("decision_logic_version"),
                            "created_at": d.get("created_at"),
                        }
                        for d in decisions
                    ],
                    "analysis_events": detail.get("analysis_events", []),
                }

            field_list = [f.strip() for f in fields.split(",")] if fields else None
            if field_list:
                result = filter_fields(result, field_list)

            if output_format == OutputFormat.JSON:
                typer.echo(format_json(result))
            else:
                a = result.get("assessment", {})
                v = result.get("vulnerability", {})
                f = result.get("finding", {})

                # --- Assessment (most important) ---
                typer.echo(f"\nAssessment: {a.get('status', 'unknown').upper()}")
                if a.get("summary"):
                    typer.echo(f"\n{a['summary']}")

                checklist = a.get("checklist", [])
                if checklist:
                    typer.echo("\n--- Checklist ---")
                    for item in checklist:
                        typer.echo(
                            f"\n  [{item.get('status', '?').upper()}] {item.get('description', '')}"
                        )
                        if item.get("conclusion"):
                            typer.echo(f"  Conclusion: {item['conclusion']}")
                        for step in item.get("investigation_steps", []):
                            typer.echo(f"    - {step}")
                        for proof in item.get("proofs", []):
                            loc = proof.get("file", "")
                            if proof.get("line"):
                                loc += f":{proof['line']}"
                            typer.echo(f"    {loc}")
                            if proof.get("code"):
                                typer.echo(f"      {proof['code']}")
                            if proof.get("comment"):
                                typer.echo(f"      # {proof['comment']}")

                # --- Finding instance ---
                typer.echo(f"\n--- Finding ---")
                typer.echo(f"ID:         {f.get('id', '')}")
                typer.echo(f"Dependency: {f.get('dependency', '')}")
                typer.echo(f"Repository: {f.get('repository', '')}")
                typer.echo(f"Manifest:   {f.get('manifest', '')}")
                if f.get("scanner"):
                    typer.echo(f"Scanner:    {f['scanner']}")
                if f.get("source_id"):
                    typer.echo(f"Source ID:  {f['source_id']}")

                # --- Vulnerability ---
                cve = v.get("cve", "Unknown")
                typer.echo(f"\n--- Vulnerability ---")
                typer.echo(f"{cve}")
                typer.echo(f"Severity: {v.get('severity', 'unknown').upper()}")
                epss = v.get("epss") or {}
                if epss.get("score"):
                    typer.echo(
                        f"EPSS: {epss['score']} (percentile: {epss.get('percentile', 'N/A')})"
                    )
                typer.echo(f"\n{v.get('summary', 'No summary available.')}")

                # --- Logs ---
                logs = result.get("logs")
                if logs:
                    typer.echo("\n--- Recommendation History ---")
                    for d in logs.get("recommendation_decisions", []):
                        conf = (
                            f" (confidence: {d['confidence']:.2f})" if d.get("confidence") else ""
                        )
                        ts = d.get("created_at", "?")
                        dtype = d.get("type", "?")
                        reason = d.get("reason", "?")
                        typer.echo(f"  {ts}: {dtype} -- {reason}{conf}")

    except (AuthenticationError, APIError, CLIError) as e:
        _handle_error(e, output_format)


@app.command("counts")
def finding_counts(
    since: str | None = typer.Option(None, "--since", help="Start date: '7d', '30d', or ISO date"),
    until: str | None = typer.Option(None, "--until", help="End date: 'now' or ISO date"),
    severity: list[str] | None = typer.Option(
        None, "--severity", "-s", help="Filter: critical,high,moderate,low"
    ),
    repo: str | None = typer.Option(None, "--repo", "-r", help="Filter by repository URL or name"),
    group_by: str | None = typer.Option(
        None, "--group-by", "-g", help="Break down by: severity, week, month"
    ),
    output: str | None = typer.Option(None, "--output", "-o", help="Output format: json, table"),
) -> None:
    """Show assessment counts (exploitable, false-positive, etc.).

    \b
    Examples:
      konvu finding counts
      konvu finding counts --since 7d
      konvu finding counts --severity critical --output json
      konvu finding counts --group-by severity
      konvu finding counts --group-by week
      konvu finding counts --group-by month --since 180d

    \b
    Exit codes:
      0  Success
      1  General error
      2  Invalid arguments
      4  Authentication failed
    """
    output_format = detect_output_format(output)

    if group_by and group_by not in VALID_COUNTS_GROUP_BY:
        typer.echo(
            f"Invalid group-by: {group_by}. Valid: {', '.join(sorted(VALID_COUNTS_GROUP_BY))}",
            err=True,
        )
        raise typer.Exit(EXIT_USAGE_ERROR)

    try:
        with KonvuClient() as client:
            base_params: dict[str, Any] = {}
            if since:
                base_params["first_seen_after"] = _parse_relative_date(since)
            if until and until != "now":
                base_params["first_seen_before"] = _parse_relative_date(until)
            if severity:
                base_params["severity"] = [s.upper() for s in severity]
            if repo:
                base_params["vcs_repository_url"] = [repo]

            if group_by == "severity":
                severity_levels = ["CRITICAL", "HIGH", "MODERATE", "LOW"]
                rows: list[dict[str, Any]] = []
                for sev in severity_levels:
                    sev_params = {**base_params, "severity": [sev]}
                    counts = _compute_assessment_counts(client, sev_params)
                    row_total = sum(counts.values())
                    if row_total > 0:
                        rows.append({"severity": sev.lower(), **counts, "total": row_total})

                grand_total = sum(r["total"] for r in rows)
                result: dict[str, Any] = {
                    "total": grand_total,
                    "group_by": "severity",
                    "rows": rows,
                }

                if output_format == OutputFormat.JSON:
                    typer.echo(format_json(result))
                else:
                    typer.echo("\nAssessment Counts by Severity")
                    typer.echo("=" * 60)
                    header = f"  {'severity':<12}"
                    for status in AssessmentStatus:
                        header += f" {status.value:>15}"
                    header += f" {'total':>8}"
                    typer.echo(header)
                    for row in rows:
                        line = f"  {row['severity']:<12}"
                        for status in AssessmentStatus:
                            line += f" {row.get(status.value, 0):>15}"
                        line += f" {row['total']:>8}"
                        typer.echo(line)
            elif group_by in ("week", "month"):
                periods = _generate_time_periods(group_by, since)
                rows = []
                for period in periods:
                    period_params = {
                        **base_params,
                        "first_seen_after": period["start"],
                        "first_seen_before": period["end"],
                    }
                    counts = _compute_assessment_counts(client, period_params)
                    row_total = sum(counts.values())
                    rows.append({"period": period["label"], **counts, "total": row_total})

                grand_total = sum(r["total"] for r in rows)
                result = {
                    "total": grand_total,
                    "group_by": group_by,
                    "rows": rows,
                }

                if output_format == OutputFormat.JSON:
                    typer.echo(format_json(result))
                else:
                    label = "Week" if group_by == "week" else "Month"
                    typer.echo(f"\nAssessment Counts by {label}")
                    typer.echo("=" * 70)
                    header = f"  {'period':<20}"
                    for status in AssessmentStatus:
                        header += f" {status.value:>15}"
                    header += f" {'total':>8}"
                    typer.echo(header)
                    for row in rows:
                        line = f"  {row['period']:<20}"
                        for status in AssessmentStatus:
                            line += f" {row.get(status.value, 0):>15}"
                        line += f" {row['total']:>8}"
                        typer.echo(line)
            else:
                counts = _compute_assessment_counts(client, base_params or None)
                total = sum(counts.values())
                result = {"total": total, **counts}

                if output_format == OutputFormat.JSON:
                    typer.echo(format_json(result))
                else:
                    typer.echo("\nAssessment Counts")
                    typer.echo("=" * 25)
                    for label, value in counts.items():
                        typer.echo(f"  {label:<20} {value:>6}")
                    typer.echo(f"  {'total':<20} {total:>6}")

    except (AuthenticationError, APIError, CLIError) as e:
        _handle_error(e, output_format)
