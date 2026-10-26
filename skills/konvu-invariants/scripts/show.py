#!/usr/bin/env python3
"""Print one fixed onboarding block. The agent pastes it unchanged and writes the rest of the message itself.

Fixed text lives here and in ../blocks; the rules block renders the JSON the agent saved under
~/.konvu/onboard/. Drawings are picked to fit the terminal width.

  show.py progress <1|2|3>   the onboarding progress bar
  show.py rules <repo>       invariants.json → one box per rule
  show.py enforce <repo>     how the rules get enforced, once the GitHub App is plugged in
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import textwrap
from pathlib import Path

BLOCKS = Path(__file__).resolve().parent.parent / "blocks"
STATE = Path(os.environ.get("KONVU_STATE", Path.home() / ".konvu" / "invariants"))


def width() -> int:
    """Columns of the nearest ancestor process that owns a terminal (macOS and Linux); 80 otherwise."""
    if os.environ.get("KONVU_COLUMNS", "").isdigit():
        return int(os.environ["KONVU_COLUMNS"])
    pid = os.getpid()
    while pid > 1:
        try:
            tty = subprocess.run(["ps", "-o", "tty=", "-p", str(pid)], capture_output=True, text=True).stdout.strip()
            if tty and tty not in ("?", "??"):
                for flag in ("-f", "-F"):
                    out = subprocess.run(["stty", flag, f"/dev/{tty}", "size"], capture_output=True, text=True).stdout.split()
                    if len(out) == 2:
                        return int(out[1])
            pid = int(subprocess.run(["ps", "-o", "ppid=", "-p", str(pid)], capture_output=True, text=True).stdout.strip() or 0)
        except (OSError, ValueError):
            break
    return 80


def fenced(text: str) -> str:
    return f"```\n{text}\n```"


def load(repo: str, name: str) -> dict:
    path = STATE / repo / name
    if not path.exists():
        sys.exit(f"Missing {path}: save it first.")
    return json.loads(path.read_text())


def progress(step: int) -> str:
    steps = ["context", "security rules", "enforce"]
    line = "      ".join(steps)
    starts, pos = [], 0
    for name in steps:
        starts.append(pos)
        pos += len(name) + 6
    end = starts[step - 1] + len(steps[step - 1]) // 2
    return fenced("\n".join([line, ("═" * end + "▶").ljust(len(line), "·"), "onboarding progress".rjust(len(line))]))


def boxed(title: str, body: list[str], w: int) -> str:
    inner = min(max(w, 50), 78) - 6
    rows = textwrap.wrap(title, inner) + ([""] + [r for b in body for r in textwrap.wrap(b, inner)] if body else [])
    top = "┌" + "─" * (inner + 2) + "┐"
    return "\n".join([top] + [f"│ {r.ljust(inner)} │" for r in rows] + ["└" + "─" * (inner + 2) + "┘"])


def rules(repo: str) -> str:
    data = load(repo, "invariants.json")
    rules = data["invariants"]
    if len(rules) == 1:
        return fenced(boxed(rules[0]["statement"], [], width()))
    return fenced("\n".join(boxed(f"RULE {i} · {r['statement']}", [], width()) for i, r in enumerate(rules, 1)))


def enforce(repo: str) -> str:
    load(repo, "invariants.json")
    diagram = (BLOCKS / ("enforce-wide.txt" if width() >= 72 else "enforce-narrow.txt")).read_text().rstrip("\n")
    return fenced(diagram)


def main() -> None:
    args = sys.argv[1:]
    if len(args) == 2 and args[0] == "progress" and args[1] in ("1", "2", "3"):
        print(progress(int(args[1])))
    elif len(args) >= 2 and args[0] in ("rules", "enforce"):
        print({"rules": rules, "enforce": enforce}[args[0]](args[1]))
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()
