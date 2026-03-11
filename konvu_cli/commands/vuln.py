from typing import Any

import typer
from rich.console import Console
from rich.table import Table
from rich.text import Text

from konvu_cli.api.client import APIError, AuthenticationError, KonvuClient
from konvu_cli.mapping import AssessmentStatus, get_assessment_color, get_assessment_summary, recommendation_to_assessment
from konvu_cli.output.detection import OutputFormat, detect_output_format
from konvu_cli.output.formatters import format_json

app = typer.Typer(help="Vulnerability lookup")

def _styled_assessment(status: str) -> Text:
    """Return a Rich Text with color-coded assessment status."""
    return Text(status.upper(), style=get_assessment_color(status))


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

            # Get affected issues for vuln info
            params: dict[str, Any] = {"cve": [vuln_id], "per_page": 100}
            issues_data = client.get("/sca_issues", params=params)
            items = issues_data.get("items", [])

            if not items:
                typer.echo(
                    f"Vulnerability {vuln_id} not found or you are not affected.",
                    err=True,
                )
                raise typer.Exit(1)

            # Vulnerability info from first issue
            vuln_info = items[0].get("vulnerability", {})

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

            # Fetch findings — single source of truth for counts & details
            findings_params: dict[str, Any] = {"cve": [vuln_id], "per_page": 100}
            findings_data = client.get("/sca_findings", params=findings_params)
            finding_items = findings_data.get("items", [])

            # Get dependency name from first finding
            if finding_items:
                first_dep = finding_items[0].get("dependency", {}).get("name", "")
                if first_dep:
                    output["vulnerability"]["dependency"] = first_dep

            by_assessment: dict[str, int] = {}
            repositories: set[str] = set()
            findings_list: list[dict[str, Any]] = []

            for finding in finding_items:
                f_dep = finding.get("dependency", {})
                f_ml = finding.get("manifest_location", {})
                f_source = finding.get("source", {})
                f_rec = finding.get("calculated_recommendation")
                f_assessment = recommendation_to_assessment(f_rec)
                f_analyses = finding.get("analyses") or {}

                f_summary = f_analyses.get("qualification_summary") or ""
                if not f_summary:
                    f_stack = f_analyses.get("stack_analysis_applicable")
                    if f_stack is False and f_assessment == AssessmentStatus.FALSE_POSITIVE:
                        f_summary = "Vulnerability not applicable to your dependency stack."
                    elif f_stack is True and f_assessment == AssessmentStatus.EXPLOITABLE:
                        f_summary = "Vulnerability applicable to your dependency stack."
                    else:
                        f_summary, _ = get_assessment_summary(f_assessment)

                by_assessment[f_assessment.value] = (
                    by_assessment.get(f_assessment.value, 0) + 1
                )
                repo = f_ml.get("vcs_repository_url", "")
                if repo:
                    repositories.add(repo)

                findings_list.append({
                    "id": finding.get("id", ""),
                    "dependency": f_dep.get("name", ""),
                    "repository": repo,
                    "scanner": f_source.get("source_name", ""),
                    "source_id": f_source.get("identifier", ""),
                    "assessment": f_assessment.value,
                    "assessment_summary": f_summary,
                })

            # Overall assessment
            if by_assessment.get("exploitable", 0) > 0:
                overall = "exploitable"
                count = by_assessment["exploitable"]
                summary = f"You have {count} exploitable instance(s) of this vulnerability."
                next_steps = "Prioritize remediation."
            elif by_assessment.get("false-positive", 0) > 0:
                overall = "false-positive"
                summary = "Not exploitable in your context."
                next_steps = "You may deprioritize remediation."
            else:
                overall = "inconclusive"
                summary = "Unable to determine exploitability."
                next_steps = "Review manually."

            output["assessment"] = {
                "status": overall,
                "summary": summary,
                "next_steps": next_steps,
                "breakdown": by_assessment,
                "total": len(findings_list),
                "repositories": sorted(repositories),
                "findings": findings_list,
            }

            # Render
            output_format = detect_output_format(output_fmt)

            if output_format == OutputFormat.JSON:
                typer.echo(format_json(output))
            else:
                console = Console(stderr=True)
                v = output["vulnerability"]
                a = output["assessment"]

                # --- Vulnerability ---
                vuln_table = Table(
                    show_header=False, box=None, padding=(0, 1), expand=False,
                )
                vuln_table.add_column(style="dim")
                vuln_table.add_column()

                vuln_table.add_row("ID", f"[bold]{v['id']}[/bold]")
                aliases = v.get("aliases", [])
                if aliases:
                    vuln_table.add_row("Aliases", ", ".join(aliases))
                vuln_table.add_row("Severity", v["severity"].upper())
                dep_name = v.get("dependency", "")
                if dep_name:
                    vuln_table.add_row("Dependency", dep_name)
                cvss_score = v.get("scoring", {}).get("cvss", {}).get("score")
                if cvss_score:
                    vuln_table.add_row("CVSS", str(cvss_score))
                vuln_table.add_row("Summary", v.get("summary", "No summary available."))

                console.print()
                console.print("[bold]Vulnerability[/bold]")
                console.print(vuln_table)

                # --- Konvu Assessment ---
                console.print()
                console.print("[bold]Konvu Assessment[/bold]")

                breakdown = a.get("breakdown", {})
                total = a.get("total", 0)
                repos = a.get("repositories", [])

                line = Text(f"{total} findings across {len(repos)} repositories: ")
                parts = []
                for status, cnt in sorted(breakdown.items()):
                    part = Text(f"{cnt} {status}", style=get_assessment_color(status))
                    parts.append(part)
                for i, part in enumerate(parts):
                    if i > 0:
                        line.append(" · ")
                    line.append_text(part)
                console.print(line)

                # Findings table
                findings = a.get("findings", [])
                if findings:
                    ft = Table(box=None, padding=(0, 1), expand=False)
                    ft.add_column("Repository")
                    ft.add_column("Assessment", no_wrap=True)
                    ft.add_column("Summary", max_width=60)

                    for f in findings:
                        status = f["assessment"]
                        ft.add_row(
                            f["repository"],
                            _styled_assessment(status),
                            f.get("assessment_summary", ""),
                        )

                    console.print()
                    console.print(ft)

    except AuthenticationError as e:
        typer.echo(f"Error: {e}", err=True)
        raise typer.Exit(1)
    except APIError as e:
        typer.echo(f"API Error: {e}", err=True)
        raise typer.Exit(1)
