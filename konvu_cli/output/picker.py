"""Interactive arrow-key picker for terminal menus.

Uses raw terminal input + rich for rendering. Falls back to a numbered
prompt when stdin is not a TTY (CI/CD, piped input).
"""

import sys
from typing import Any

from rich.console import Console
from rich.text import Text


def pick(title: str, options: list[str], default: int = 0) -> int:
    """Present an interactive picker and return the selected index.

    Args:
        title: Prompt text shown above the options.
        options: List of option labels.
        default: Index of the initially selected option.

    Returns:
        The index of the chosen option.

    Raises:
        KeyboardInterrupt: If the user presses Ctrl-C.
    """
    if not sys.stdin.isatty():
        return _fallback_pick(title, options, default)

    try:
        return _interactive_pick(title, options, default)
    except Exception:
        # Any terminal issue → fall back to numbered prompt
        return _fallback_pick(title, options, default)


def _render(console: Console, title: str, options: list[str], selected: int) -> None:
    """Render the picker state."""
    # Move cursor up to overwrite previous render (options + title + blank lines)
    lines_to_clear = len(options) + 3  # title + blank + options + trailing blank
    for _ in range(lines_to_clear):
        console.file.write("\033[A\033[2K")

    console.print(f"  {title}")
    console.print()
    for i, opt in enumerate(options):
        if i == selected:
            line = Text("  ❯ ", style="bold cyan")
            line.append(opt, style="bold")
        else:
            line = Text("    ", style="dim")
            line.append(opt, style="dim")
        console.print(line)
    console.print()


def _interactive_pick(title: str, options: list[str], default: int) -> int:
    """Arrow-key picker using raw terminal input."""
    import termios
    import tty

    console = Console(stderr=True)
    selected = default
    fd = sys.stdin.fileno()
    old_settings: list[Any] = termios.tcgetattr(fd)

    try:
        # Initial render — print placeholder lines first so _render can overwrite
        console.print(f"  {title}")
        console.print()
        for i, opt in enumerate(options):
            if i == selected:
                line = Text("  ❯ ", style="bold cyan")
                line.append(opt, style="bold")
            else:
                line = Text("    ", style="dim")
                line.append(opt, style="dim")
            console.print(line)
        console.print()

        tty.setraw(fd)

        while True:
            ch = sys.stdin.read(1)

            if ch in ("\r", "\n"):
                break

            if ch == "\x03":  # Ctrl-C
                raise KeyboardInterrupt

            if ch == "\x1b":  # Escape sequence (arrow keys)
                seq1 = sys.stdin.read(1)
                if seq1 == "[":
                    seq2 = sys.stdin.read(1)
                    if seq2 == "A":  # Up
                        selected = (selected - 1) % len(options)
                    elif seq2 == "B":  # Down
                        selected = (selected + 1) % len(options)

            # Restore terminal briefly to render, then go raw again
            termios.tcsetattr(fd, termios.TCSADRAIN, old_settings)
            _render(console, title, options, selected)
            tty.setraw(fd)

    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old_settings)

    return selected


def _fallback_pick(title: str, options: list[str], default: int) -> int:
    """Numbered prompt fallback for non-TTY environments."""
    import typer

    typer.echo(f"\n{title}\n", err=True)
    for i, opt in enumerate(options):
        typer.echo(f"  {i + 1}. {opt}", err=True)
    typer.echo("", err=True)

    choice = typer.prompt("Enter choice", default=str(default + 1))
    try:
        idx = int(choice) - 1
        if 0 <= idx < len(options):
            return idx
    except ValueError:
        pass

    return default
