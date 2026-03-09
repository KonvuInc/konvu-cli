from typing import Any, cast

import typer

from konvu_cli.api.client import APIError, AuthenticationError, KonvuClient
from konvu_cli.output.detection import OutputFormat, detect_output_format
from konvu_cli.output.formatters import format_json

app = typer.Typer(help="Security metrics and reporting")


@app.command("show")
def show_metrics(
    since: str = typer.Option("30d", "--since", help="Start date: '30d', '90d', or ISO date"),
    until: str = typer.Option("now", "--until", help="End date: 'now' or ISO date"),
    interval: str = typer.Option(
        "week", "--interval", help="Aggregation interval: day, week, month"
    ),
    include: list[str] | None = typer.Option(
        None,
        "--include",
        "-i",
        help="Data to include: summary,trends,breakdown,top_cves,new_vs_closed",
    ),
    compare: str | None = typer.Option(
        None, "--compare", help="Compare to: previous_period, 30d_ago, 90d_ago"
    ),
    output_fmt: str | None = typer.Option(
        None, "--output", "-o", help="Output format: json, table"
    ),
) -> None:
    """Show security posture metrics.

    \b
    Examples:
      konvu metrics
      konvu metrics --include top_cves,new_vs_closed
      konvu metrics --include summary --output json

    \b
    Exit codes:
      0  Success
      1  General error
      4  Authentication failed
    """
    include = include or ["summary", "trends"]

    try:
        with KonvuClient() as client:
            # Map interval to API parameter
            period = "week" if interval == "week" else "day"

            output: dict[str, Any] = {
                "period": {
                    "since": since,
                    "until": until,
                    "interval": interval,
                },
            }

            if "summary" in include or "trends" in include:
                # Get backlog data (these endpoints return lists)
                backlog = cast(
                    list[dict[str, Any]],
                    client.get("/overview/backlog", params={"period": period}),
                )
                to_fix = cast(
                    list[dict[str, Any]],
                    client.get("/overview/backlog_to_fix", params={"period": period}),
                )
                to_dismiss = cast(
                    list[dict[str, Any]],
                    client.get("/overview/backlog_to_dismiss", params={"period": period}),
                )

                if backlog:
                    latest = backlog[-1] if backlog else {}
                    latest_fix = to_fix[-1] if to_fix else {}
                    latest_dismiss = to_dismiss[-1] if to_dismiss else {}

                    output["summary"] = {
                        "total_open": latest.get("open_issues", 0),
                        "exploitable": latest_fix.get("open_to_fix", 0),
                        "false_positive": latest_dismiss.get("open_to_dismiss", 0),
                    }

                if "trends" in include:
                    output["trends"] = {
                        "backlog": backlog,
                        "to_fix": to_fix,
                        "to_dismiss": to_dismiss,
                    }

            if "top_cves" in include:
                top_cves = cast(
                    list[dict[str, Any]],
                    client.get("/overview/top_cves_to_prioritize"),
                )
                output["top_cves"] = [
                    {
                        "vulnerability_id": item.get("vulnerability_id"),
                        "aliases": item.get("aliases", []),
                        "recommendation": item.get("recommendation"),
                    }
                    for item in (top_cves or [])
                ]

            if "new_vs_closed" in include:
                new_vs_closed = cast(
                    list[dict[str, Any]],
                    client.get("/overview/new_vs_closed", params={"period": period}),
                )
                output["new_vs_closed"] = new_vs_closed

            # Output
            output_format = detect_output_format(output_fmt)

            if output_format == OutputFormat.JSON:
                typer.echo(format_json(output))
            else:
                # Human-readable format
                summary = output.get("summary", {})
                typer.echo("\nSecurity Posture Summary")
                typer.echo("=" * 25)
                typer.echo(f"Total Open Issues:  {summary.get('total_open', 0)}")
                typer.echo(f"  Exploitable:      {summary.get('exploitable', 0)}")
                typer.echo(f"  False Positive:   {summary.get('false_positive', 0)}")

                if "top_cves" in output:
                    typer.echo("\nTop CVEs to Prioritize:")
                    for cve in output["top_cves"][:5]:
                        aliases = cve.get("aliases", [])
                        cve_id = aliases[0] if aliases else cve.get("vulnerability_id", "Unknown")
                        typer.echo(f"  - {cve_id}")

                if "new_vs_closed" in output:
                    typer.echo("\nNew vs Closed:")
                    for point in output["new_vs_closed"][-5:]:
                        date = point.get("date", "?")
                        new = point.get("new", 0)
                        closed = point.get("closed", 0)
                        typer.echo(f"  {date}: +{new} / -{closed}")

    except AuthenticationError as e:
        typer.echo(f"Error: {e}", err=True)
        raise typer.Exit(1)
    except APIError as e:
        typer.echo(f"API Error: {e}", err=True)
        raise typer.Exit(1)
