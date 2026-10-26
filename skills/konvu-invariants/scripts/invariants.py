#!/usr/bin/env python3
"""Hand the agent the invariant workflow one step at a time, and only move on once the step is really done.

  invariants.py prepare            start ranking the user's repos in the background (prints nothing to show)
  invariants.py start              begin at the repo step and print it
  invariants.py next               print the current step again
  invariants.py done repo <path>   the user picked this repo
  invariants.py done direction <text>
                                   the user picked where to start in it
  invariants.py done rules         the security rule is saved and the user is happy with it
  invariants.py done upload        konvu-upload-invariants reported it was added

Progress lives in ~/.konvu/invariants/progress.json; step instructions in ../steps.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
from pathlib import Path

SKILL = Path(__file__).resolve().parent.parent
STATE = Path(os.environ.get("KONVU_STATE", Path.home() / ".konvu" / "invariants"))
PROGRESS = STATE / "progress.json"
RANKING = STATE / "repos.json"
CMD = f"python3 {Path(__file__).resolve()}"
STEPS = ["repo", "direction", "rules", "upload"]

FINISHED = (
    "The security rule is written and uploaded. If konvu-onboarding brought you here, go back to it for its "
    "last step now. Otherwise close in one line: it is saved in Konvu, which maps it to the controls in "
    "their code once the GitHub App is plugged in, and they can write more anytime."
)


def load() -> dict[str, str]:
    return json.loads(PROGRESS.read_text()) if PROGRESS.exists() else {"step": "repo"}


def save(progress: dict[str, str]) -> None:
    STATE.mkdir(parents=True, exist_ok=True)
    PROGRESS.write_text(json.dumps(progress, indent=2))


def start_ranking() -> None:
    """Rank the user's repos in the background, so the repo step doesn't wait on it."""
    STATE.mkdir(parents=True, exist_ok=True)
    RANKING.unlink(missing_ok=True)
    tmp = RANKING.with_suffix(".tmp")
    with tmp.open("w") as out:
        subprocess.Popen(
            ["sh", "-c", f'python3 "{SKILL / "scripts" / "repos.py"}" > "{tmp}" && mv "{tmp}" "{RANKING}"'],
            stdout=out, stderr=subprocess.DEVNULL, start_new_session=True,
        )


def ranking() -> str:
    """The background ranking, waiting up to 40 seconds; run here if it never started (some sandboxes kill background jobs)."""
    for _ in range(80):
        if RANKING.exists():
            return RANKING.read_text().strip()
        if not RANKING.with_suffix(".tmp").exists():
            break
        time.sleep(0.5)
    return subprocess.run(
        ["python3", str(SKILL / "scripts" / "repos.py")], capture_output=True, text=True
    ).stdout.strip()


def render(progress: dict[str, str]) -> str:
    """The current step's instructions, with how to finish it; nothing about later steps."""
    step = progress["step"]
    if step == "finished":
        return FINISHED
    n = STEPS.index(step) + 1
    body = (SKILL / "steps" / f"{n}-{step}.md").read_text().strip()
    for key in ("repo", "repo_path", "direction"):
        body = body.replace("{" + key + "}", progress.get(key, ""))
    body = body.replace("{skill_dir}", str(SKILL)).replace("{state}", str(STATE))
    if "{repos_ranking}" in body:
        body = body.replace("{repos_ranking}", ranking())
    finish = {
        "repo": "the user picks a repo: run `invariants.py done repo <its path>`",
        "direction": "the user picks where to start: run `invariants.py done direction \"<the direction, in a few words>\"`",
        "rules": "the user is happy with their security rule (sounds good, yes, looks right, or anything that means it): run `invariants.py done rules`",
        "upload": "konvu-upload-invariants reports the rule was added: run `invariants.py done upload`",
    }[step].replace("`invariants.py", f"`{CMD}")
    if step == "upload":
        tail = f"This step is finished when {finish}, in the same turn; that prints what comes next."
    else:
        tail = (
            f"End your message there and wait for the user's reply. This step is finished only when {finish}; "
            f"that prints the next step. Until then, stay on this step."
        )
    return f"[Current step: {step}, {n} of {len(STEPS)}. This line is for you, not the user.]\n\n{body}\n\n{tail}"


def blocked(reason: str, progress: dict[str, str]) -> None:
    print(f"BLOCKED: {reason}\n\n{render(progress)}")
    sys.exit(1)


def done(step: str, args: list[str]) -> None:
    progress = load()
    if progress["step"] != step:
        blocked(f"the current step is {progress['step']}, not {step}.", progress)
    if step == "repo":
        path = Path(args[0]).expanduser().resolve() if args else None
        if not path or not path.is_dir():
            blocked("give the path of the repo the user picked: `invariants.py done repo <path>`.", progress)
        top = subprocess.run(["git", "-C", str(path), "rev-parse", "--show-toplevel"], capture_output=True, text=True)
        if top.returncode != 0:
            blocked(f"{path} is not inside a git repo.", progress)
        progress.update(repo=path.name, repo_path=str(path))
    if step == "direction":
        if not " ".join(args).strip():
            blocked("say which direction the user picked: `invariants.py done direction \"<direction>\"`.", progress)
        progress["direction"] = " ".join(args).strip()
    if step == "rules":
        rules = STATE / progress["repo"] / "invariants.json"
        if not rules.exists() or not json.loads(rules.read_text()).get("invariants"):
            blocked(f"no security rule saved in {rules} yet.", progress)
    i = STEPS.index(step)
    progress["step"] = STEPS[i + 1] if i + 1 < len(STEPS) else "finished"
    save(progress)
    print(render(progress))


def main() -> None:
    args = sys.argv[1:]
    if args == ["prepare"]:
        start_ranking()
        print("Ranking the user's repos in the background. Nothing to show the user.")
    elif args == ["start"]:
        if not RANKING.exists() and not RANKING.with_suffix(".tmp").exists():
            start_ranking()
        save({"step": "repo"})
        print(render(load()))
    elif args == ["next"]:
        print(render(load()))
    elif len(args) >= 2 and args[0] == "done" and args[1] in STEPS:
        done(args[1], args[2:])
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()
