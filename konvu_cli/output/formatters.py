import csv
import io
import json
from typing import Any, Callable

from rich.console import Console
from rich.table import Table
from rich.text import Text


def format_json(data: Any) -> str:
    """Format data as pretty-printed JSON."""
    return json.dumps(data, indent=2, default=str)


def format_table(
    data: dict[str, Any],
    columns: list[str],
    list_key: str = "issues",
    title: str | None = None,
    style_cell: Callable[[str, str], str | Text] | None = None,
) -> str:
    """Format data as a rich table.

    Args:
        data: Dict containing a list under `list_key`
        columns: Column names to display
        list_key: Key in data containing the list of items
        title: Optional table title
        style_cell: Optional callback(column, value) -> styled Text or str
    """
    console = Console(force_terminal=True, width=120)
    table = Table(title=title, show_header=True, header_style="bold")

    for col in columns:
        table.add_column(col.replace("_", " ").title())

    items = data.get(list_key, [])
    for item in items:
        row: list[str | Text] = []
        for col in columns:
            val = str(item.get(col, ""))
            if style_cell:
                val = style_cell(col, val)
            row.append(val)
        table.add_row(*row)

    with console.capture() as capture:
        console.print(table)

    return capture.get()


def format_csv(
    data: dict[str, Any],
    columns: list[str],
    list_key: str = "issues",
) -> str:
    """Format data as CSV."""
    output = io.StringIO()
    writer = csv.writer(output)

    # Header
    writer.writerow(columns)

    # Rows
    items = data.get(list_key, [])
    for item in items:
        row = [str(item.get(col, "")) for col in columns]
        writer.writerow(row)

    return output.getvalue()


def format_quiet(items: list[dict[str, Any]], id_field: str = "id") -> str:
    """Format items as bare IDs, one per line. For piping to other commands."""
    return "\n".join(str(item.get(id_field, "")) for item in items)


def filter_fields(data: dict[str, Any], fields: list[str] | None) -> dict[str, Any]:
    """Keep only the specified fields from a dict. Returns original if fields is None."""
    if fields is None:
        return data
    return {k: v for k, v in data.items() if k in fields}
