from typing import Any

import typer

from konvu_cli.api.client import APIError, AuthenticationError, KonvuClient
from konvu_cli.mapping import AssessmentStatus, assessment_to_recommendation
from konvu_cli.output.detection import OutputFormat, detect_output_format
from konvu_cli.output.formatters import format_json

app = typer.Typer(help="Dismiss security issues")


@app.command("run")
def dismiss_issues(
    issues_list: str | None = typer.Option(
        None, "--issues", help="Comma-separated list of issue IDs to dismiss"
    ),
    assessment: list[str] | None = typer.Option(
        None,
        "--assessment",
        "-a",
        help="Filter: dismiss all with this assessment (e.g., false-positive)",
    ),
    severity: list[str] | None = typer.Option(None, "--severity", "-s", help="Filter by severity"),
    repo: str | None = typer.Option(None, "--repo", "-r", help="Filter by repository"),
    reason: str = typer.Option("Dismissed via Konvu CLI", "--reason", help="Reason for dismissal"),
    dry_run: bool = typer.Option(
        False, "--dry-run", help="Preview what would be dismissed without executing"
    ),
    output_fmt: str | None = typer.Option(
        None, "--output", "-o", help="Output format: json, table"
    ),
) -> None:
    """Dismiss security issues.

    \b
    Examples:
      # Preview dismissals
      konvu dismiss --assessment false-positive --dry-run

      # Dismiss specific issues
      konvu dismiss --issues id1,id2 --reason "Not applicable"

      # Dismiss all false positives in a repo
      konvu dismiss --assessment false-positive --repo github:org/repo

    \b
    Exit codes:
      0  Success
      1  General error
      2  Invalid arguments
      4  Authentication failed
    """
    if not issues_list and not assessment:
        typer.echo("Error: Must specify --issues or --assessment filter", err=True)
        raise typer.Exit(1)

    try:
        with KonvuClient() as client:
            to_dismiss: list[dict[str, Any]] = []

            # If using filters, first query matching issues
            if assessment or severity or repo:
                params: dict[str, Any] = {"per_page": 500}

                if assessment:
                    recommendations: list[str] = []
                    for a in assessment:
                        try:
                            status = AssessmentStatus(a.lower().replace("_", "-"))
                            recommendations.extend(assessment_to_recommendation(status))
                        except ValueError:
                            typer.echo(f"Invalid assessment: {a}", err=True)
                            raise typer.Exit(1)
                    params["recommendation"] = recommendations

                if severity:
                    params["severity"] = [s.upper() for s in severity]

                if repo:
                    params["vcs_repository_url"] = [repo]

                params["any_source_state"] = ["open"]

                data = client.get("/sca_issues", params=params)
                items = data.get("items", [])

                # Build list of issues to dismiss
                for item in items:
                    for source in item.get("sources", []):
                        to_dismiss.append(
                            {
                                "integration_id": source.get("integration_id"),
                                "issue_id": source.get("id"),
                                "cve": item.get("vulnerability", {}).get("cve_number"),
                                "repository": item.get("manifest_location", {}).get(
                                    "repository_url"
                                ),
                            }
                        )

            elif issues_list:
                # Parse comma-separated issue IDs
                issue_ids = [id.strip() for id in issues_list.split(",")]
                to_dismiss = [{"issue_id": id} for id in issue_ids]

            if not to_dismiss:
                typer.echo("No issues found matching criteria.")
                raise typer.Exit(0)

            # Output what will be dismissed
            output: dict[str, Any] = {
                "action": "dismiss",
                "dry_run": dry_run,
                "reason": reason,
                "total": len(to_dismiss),
                "items": to_dismiss[:50],  # Show first 50
            }

            if dry_run:
                output["message"] = (
                    f"Would dismiss {len(to_dismiss)} issues. Use without --dry-run to execute."
                )
                output_format = detect_output_format(output_fmt)

                if output_format == OutputFormat.JSON:
                    typer.echo(format_json(output))
                else:
                    typer.echo(f"\nDry run: would dismiss {len(to_dismiss)} issues")
                    typer.echo(f"Reason: {reason}\n")
                    for item in to_dismiss[:10]:
                        typer.echo(
                            f"  - {item.get('cve', item.get('issue_id'))} "
                            f"in {item.get('repository', 'unknown')}"
                        )
                    if len(to_dismiss) > 10:
                        typer.echo(f"  ... and {len(to_dismiss) - 10} more")

                raise typer.Exit(0)

            # Execute dismissals
            succeeded: list[dict[str, Any]] = []
            failed: list[dict[str, Any]] = []

            for item in to_dismiss:
                integration_id = item.get("integration_id")
                issue_id = item.get("issue_id")

                if not integration_id or not issue_id:
                    failed.append({"id": issue_id, "error": "Missing integration_id or issue_id"})
                    continue

                try:
                    client.post(
                        f"/integrations/{integration_id}/issue/{issue_id}/dismiss",
                        data={"reason": reason},
                    )
                    succeeded.append(item)
                except APIError as e:
                    failed.append({"id": issue_id, "error": str(e)})

            output["results"] = {
                "succeeded": len(succeeded),
                "failed": len(failed),
            }
            output["succeeded"] = succeeded
            output["failed"] = failed

            output_format = detect_output_format(output_fmt)

            if output_format == OutputFormat.JSON:
                typer.echo(format_json(output))
            else:
                typer.echo(f"\nDismissed {len(succeeded)} issues")
                if failed:
                    typer.echo(f"Failed: {len(failed)}")
                    for f in failed[:5]:
                        typer.echo(f"  - {f['id']}: {f['error']}")

    except AuthenticationError as e:
        typer.echo(f"Error: {e}", err=True)
        raise typer.Exit(1)
    except APIError as e:
        typer.echo(f"API Error: {e}", err=True)
        raise typer.Exit(1)
