from typing import Any

import typer

from konvu_cli.api.client import APIError, AuthenticationError, KonvuClient
from konvu_cli.mapping import recommendation_to_assessment
from konvu_cli.output.detection import OutputFormat, detect_output_format
from konvu_cli.output.formatters import format_json

app = typer.Typer(help="Vulnerability lookup")


@app.command("get")
def get_vulnerability(
    vuln_id: str = typer.Argument(..., help="CVE ID, GHSA ID, or Konvu vulnerability ID"),
    include: list[str] | None = typer.Option(
        None,
        "--include",
        "-i",
        help="Data to include: summary,technical,exploitability,remediation,references,affected",
    ),
    output_fmt: str | None = typer.Option(
        None, "--output", "-o", help="Output format: json, table"
    ),
) -> None:
    """Get detailed information about a vulnerability.

    \b
    Examples:
      konvu vuln get CVE-2024-1234
      konvu vuln get GHSA-xxxx --include remediation --output json

    \b
    Exit codes:
      0  Success
      1  General error
      3  Vulnerability not found
      4  Authentication failed
    """
    include = include or ["summary", "affected"]

    try:
        with KonvuClient() as client:
            typer.echo(f"Looking up {vuln_id}...", err=True)

            # Try to get affected issues for this CVE
            params: dict[str, Any] = {"cve": [vuln_id], "per_page": 100}
            issues_data = client.get("/sca_issues", params=params)

            items = issues_data.get("items", [])

            if not items:
                typer.echo(
                    f"Vulnerability {vuln_id} not found or you are not affected.",
                    err=True,
                )
                raise typer.Exit(1)

            # Get vulnerability info from first issue
            first_issue = items[0]
            vuln_info = first_issue.get("vulnerability", {})

            # Build output
            output: dict[str, Any] = {
                "vulnerability": {
                    "id": vuln_info.get("id") or vuln_id,
                    "aliases": vuln_info.get("aliases", []),
                    "severity": (vuln_info.get("severity") or "unknown").lower(),
                    "summary": vuln_info.get("summary", ""),
                    "scoring": {
                        "cvss": {
                            "score": vuln_info.get("cvss"),
                            "vector": None,
                        },
                        "epss": {
                            "score": vuln_info.get("epss"),
                            "percentile": None,
                        },
                    },
                },
            }

            if "remediation" in include:
                output["vulnerability"]["remediation"] = {
                    "fix_available": vuln_info.get("fixed") is not None,
                    "fixed_in": vuln_info.get("fixed"),
                }

            # Calculate affected summary
            by_assessment: dict[str, int] = {}
            repositories: set[str] = set()

            for issue in items:
                rec = issue.get("recommendation")
                assessment = recommendation_to_assessment(rec)
                by_assessment[assessment.value] = by_assessment.get(assessment.value, 0) + 1

                manifest = issue.get("manifest_location", {})
                if manifest.get("repository_url"):
                    repositories.add(manifest["repository_url"])

            # Determine overall assessment
            if by_assessment.get("exploitable", 0) > 0:
                overall_assessment = "exploitable"
                count = by_assessment["exploitable"]
                summary = f"You have {count} exploitable instance(s) of this vulnerability."
                next_steps = "Prioritize remediation."
            elif by_assessment.get("false-positive", 0) > 0:
                overall_assessment = "false-positive"
                summary = "Not exploitable in your context."
                next_steps = "You may deprioritize remediation."
            else:
                overall_assessment = "inconclusive"
                summary = "Unable to determine exploitability."
                next_steps = "Review manually."

            output["affected"] = {
                "you_are_affected": True,
                "issue_count": len(items),
                "by_assessment": by_assessment,
                "repositories": list(repositories),
            }

            output["assessment"] = {
                "status": overall_assessment,
                "summary": summary,
                "next_steps": next_steps,
            }

            # Output
            output_format = detect_output_format(output_fmt)

            if output_format == OutputFormat.JSON:
                typer.echo(format_json(output))
            else:
                # Human-readable format
                v = output["vulnerability"]
                typer.echo(f"\n{v['id']}")
                typer.echo("=" * len(str(v["id"])))
                typer.echo(f"Severity: {v['severity'].upper()}")
                cvss_score = v.get("scoring", {}).get("cvss", {}).get("score")
                if cvss_score:
                    typer.echo(f"CVSS: {cvss_score}")
                typer.echo(f"\n{v.get('summary', 'No summary available.')}\n")

                typer.echo(f"Assessment: {output['assessment']['status'].upper()}")
                typer.echo(f"  {output['assessment']['summary']}")
                typer.echo(f"  {output['assessment']['next_steps']}")

                typer.echo(f"\nAffected repositories ({len(repositories)}):")
                for repo in sorted(repositories):
                    typer.echo(f"  - {repo}")

    except AuthenticationError as e:
        typer.echo(f"Error: {e}", err=True)
        raise typer.Exit(1)
    except APIError as e:
        typer.echo(f"API Error: {e}", err=True)
        raise typer.Exit(1)
