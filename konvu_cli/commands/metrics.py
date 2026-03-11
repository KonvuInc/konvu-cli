from typing import Any, cast

import typer
from rich.console import Console
from rich.text import Text

from konvu_cli.api.client import APIError, AuthenticationError, KonvuClient
from konvu_cli.mapping import get_assessment_color
from konvu_cli.output.detection import OutputFormat, detect_output_format
from konvu_cli.output.formatters import format_json

from rich.table import Table

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
                console = Console(stderr=True)
                summary = output.get("summary", {})

                # --- Summary ---
                console.print()
                console.print("[bold]Security Posture[/bold]")

                summary_table = Table(
                    show_header=False, box=None, padding=(0, 1), expand=False,
                )
                summary_table.add_column(style="dim")
                summary_table.add_column()

                summary_table.add_row(
                    "Total Open", str(summary.get("total_open", 0))
                )

                exploitable_count = summary.get("exploitable", 0)
                summary_table.add_row(
                    "Exploitable",
                    Text(str(exploitable_count), style=get_assessment_color("exploitable")),
                )

                fp_count = summary.get("false_positive", 0)
                summary_table.add_row(
                    "False Positive",
                    Text(str(fp_count), style=get_assessment_color("false-positive")),
                )

                console.print(summary_table)

                # --- Trends ---
                trends = output.get("trends")
                if trends:
                    backlog_pts = trends.get("backlog", [])
                    fix_pts = trends.get("to_fix", [])
                    dismiss_pts = trends.get("to_dismiss", [])

                    # Index to_fix and to_dismiss by date for joining
                    fix_by_date = {
                        p.get("date") or p.get("period_start", ""): p
                        for p in fix_pts
                    }
                    dismiss_by_date = {
                        p.get("date") or p.get("period_start", ""): p
                        for p in dismiss_pts
                    }

                    if backlog_pts:
                        console.print()
                        console.print(f"[bold]Trend ({interval}ly)[/bold]")
                        trend_table = Table(
                            box=None, padding=(0, 1), expand=False,
                        )
                        trend_table.add_column("Period", style="dim")
                        trend_table.add_column("Total", justify="right")
                        trend_table.add_column("Exploitable", justify="right")
                        trend_table.add_column("False Positive", justify="right")

                        for pt in backlog_pts:
                            raw_date = pt.get("date") or pt.get("period_start", "?")
                            # Format label based on interval
                            label = raw_date[:10] if len(raw_date) >= 10 else raw_date
                            if interval == "month" and len(label) >= 7:
                                label = label[:7]  # YYYY-MM
                            elif interval == "week" and len(label) >= 10:
                                label = f"w/o {label}"  # week of YYYY-MM-DD

                            total = pt.get("open_issues", 0)
                            fix_pt = fix_by_date.get(raw_date, {})
                            dismiss_pt = dismiss_by_date.get(raw_date, {})
                            exploitable = fix_pt.get("open_to_fix", 0)
                            false_pos = dismiss_pt.get("open_to_dismiss", 0)

                            trend_table.add_row(
                                label,
                                str(total),
                                Text(
                                    str(exploitable),
                                    style=get_assessment_color("exploitable"),
                                ),
                                Text(
                                    str(false_pos),
                                    style=get_assessment_color("false-positive"),
                                ),
                            )

                        console.print(trend_table)

                # --- Top CVEs ---
                if "top_cves" in output and output["top_cves"]:
                    console.print()
                    console.print("[bold]Top CVEs to Prioritize[/bold]")
                    cve_table = Table(
                        box=None, padding=(0, 1), expand=False,
                    )
                    cve_table.add_column("#", style="dim")
                    cve_table.add_column("CVE")

                    for i, cve in enumerate(output["top_cves"][:5], 1):
                        aliases = cve.get("aliases", [])
                        cve_id = aliases[0] if aliases else cve.get("vulnerability_id", "Unknown")
                        cve_table.add_row(str(i), cve_id)

                    console.print(cve_table)

                # --- New vs Closed ---
                if "new_vs_closed" in output and output["new_vs_closed"]:
                    console.print()
                    console.print("[bold]New vs Closed[/bold]")
                    nvc_table = Table(
                        box=None, padding=(0, 1), expand=False,
                    )
                    nvc_table.add_column("Date", style="dim")
                    nvc_table.add_column("New", style="red")
                    nvc_table.add_column("Closed", style="green")

                    for point in output["new_vs_closed"][-5:]:
                        date = point.get("date", "?")
                        new = point.get("new", 0)
                        closed = point.get("closed", 0)
                        nvc_table.add_row(date, f"+{new}", f"-{closed}")

                    console.print(nvc_table)

    except AuthenticationError as e:
        typer.echo(f"Error: {e}", err=True)
        raise typer.Exit(1)
    except APIError as e:
        typer.echo(f"API Error: {e}", err=True)
        raise typer.Exit(1)
