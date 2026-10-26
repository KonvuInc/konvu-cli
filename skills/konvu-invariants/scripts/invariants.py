#!/usr/bin/env python3
"""Hand the agent the invariant workflow one step at a time, and only move on once the step is really done.

  invariants.py prepare                  start ranking the user's repos in the background (prints nothing to show)
  invariants.py start                    begin a new run at the repo step and print it; the run id is in every command it prints
  invariants.py next <run>               print the run's current step again
  invariants.py done <run> repo <path>   the user picked this repo
  invariants.py done <run> direction <text>
                                         the user picked where to start in it
  invariants.py done <run> rules         the security rule is saved and the user is happy with it
  invariants.py done <run> upload        konvu-upload-invariants reported it was added

Each run (one conversation) keeps its progress and files in ~/.konvu/invariants/runs/<run>/, so two
conversations never share a step or a rule. The repo ranking in ~/.konvu/invariants/repos.json is shared
and refreshed once it is an hour old. KONVU_STATE overrides ~/.konvu/invariants. Step instructions live in ../steps.
"""

from __future__ import annotations

import json
import os
import re
import secrets
import shlex
import subprocess
import sys
import time
from pathlib import Path

SKILL = Path(__file__).resolve().parent.parent
STATE = Path(os.environ.get("KONVU_STATE", Path.home() / ".konvu" / "invariants"))
RUNS = STATE / "runs"
RANKING = STATE / "repos.json"
RANKING_MAX_AGE = 3600
RANKING_TIMEOUT = 180  # a ranking still being written after this long was killed
CMD = f"python3 {shlex.quote(str(Path(__file__).resolve()))}"
SHOW = f"python3 {shlex.quote(str(SKILL / 'scripts' / 'show.py'))}"
STEPS = ["repo", "direction", "rules", "upload"]
RUN_ID = re.compile(r"\d{8}-[0-9a-f]{8}")

FINISHED = (
    "The security rule is written and uploaded. If konvu-onboarding brought you here, go back to it for its "
    "last step now; its diagram command is `{show} enforce {dir_q}`. Otherwise close in one line: it is saved "
    "in Konvu, which maps it to the controls in their code once the GitHub App is plugged in, and they can "
    "write more anytime."
)


def run_dir(run: str) -> Path:
    path = RUNS / run
    if not RUN_ID.fullmatch(run) or not path.is_dir():
        sys.exit(f"Unknown run {run!r}: use the run id from the commands `invariants.py start` printed.")
    return path


def load(run: str) -> dict[str, str]:
    return json.loads((run_dir(run) / "progress.json").read_text())


def save(progress: dict[str, str]) -> None:
    path = RUNS / progress["run"]
    path.mkdir(parents=True, exist_ok=True)
    tmp = path / "progress.json.tmp"
    tmp.write_text(json.dumps(progress, indent=2))
    tmp.replace(path / "progress.json")


def pending_rankings() -> list[Path]:
    """Ranking files still being written; older ones were left by a killed job and are removed."""
    pending = []
    for tmp in STATE.glob("repos.*.tmp"):
        try:
            if time.time() - tmp.stat().st_mtime < RANKING_TIMEOUT:
                pending.append(tmp)
            else:
                tmp.unlink()
        except OSError:
            continue
    return pending


def start_ranking() -> None:
    """Rank the user's repos in the background, so the repo step doesn't wait on it."""
    STATE.mkdir(parents=True, exist_ok=True)
    RANKING.unlink(missing_ok=True)
    tmp = STATE / f"repos.{os.getpid()}.tmp"  # one per job: concurrent jobs never write the same file
    tmp.touch()  # visible as pending before the job starts writing
    script, out, final = (shlex.quote(str(p)) for p in (SKILL / "scripts" / "repos.py", tmp, RANKING))
    subprocess.Popen(
        ["sh", "-c", f"python3 {script} > {out} && mv {out} {final}"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True,
    )


def ranking() -> str:
    """The background ranking, waiting up to 40 seconds; run here if none is on its way (some sandboxes kill background jobs)."""
    for _ in range(80):
        if RANKING.exists():
            return RANKING.read_text().strip()
        if not pending_rankings():
            break
        time.sleep(0.5)
    return subprocess.run(
        ["python3", str(SKILL / "scripts" / "repos.py")], capture_output=True, text=True
    ).stdout.strip()


def fill(body: str, values: dict[str, str]) -> str:
    """Fill {placeholders} in one pass, so text the user chose is never read as a placeholder."""
    return re.sub(r"\{(\w+)\}", lambda m: values.get(m[1], m[0]), body)


def render(progress: dict[str, str]) -> str:
    """The current step's instructions, with how to finish it; nothing about later steps."""
    step, run = progress["step"], progress["run"]
    folder = RUNS / run / progress.get("repo", "")
    values = {
        "repo": progress.get("repo", ""), "repo_path": progress.get("repo_path", ""),
        "direction": progress.get("direction", ""), "skill_dir": str(SKILL),
        "dir": str(folder), "dir_q": shlex.quote(str(folder)), "show": SHOW,
    }
    if step == "finished":
        return fill(FINISHED, values)
    n = STEPS.index(step) + 1
    body = (SKILL / "steps" / f"{n}-{step}.md").read_text().strip()
    if "{repos_ranking}" in body:
        values["repos_ranking"] = ranking()
    body = fill(body, values)
    done = f"`{CMD} done {run}"
    finish = {
        "repo": f"the user picks a repo: run {done} repo \"<its path>\"`",
        "direction": f"the user picks where to start: run {done} direction \"<the direction, in a few words>\"`",
        "rules": f"the user is happy with their security rule (sounds good, yes, looks right, or anything that means it): run {done} rules`",
        "upload": f"konvu-upload-invariants reports the rule was added: run {done} upload`",
    }[step]
    if step == "upload":
        tail = f"This step is finished when {finish}, in the same turn; that prints what comes next."
    else:
        tail = (
            f"End your message there and wait for the user's reply. This step is finished only when {finish}; "
            f"that prints the next step. Until then, stay on this step. If you lose track, `{CMD} next {run}` "
            f"prints it again."
        )
    return f"[Current step: {step}, {n} of {len(STEPS)}, run {run}. This line is for you, not the user.]\n\n{body}\n\n{tail}"


def blocked(reason: str, progress: dict[str, str]) -> None:
    print(f"BLOCKED: {reason}\n\n{render(progress)}")
    sys.exit(1)


def done(run: str, step: str, args: list[str]) -> None:
    progress = load(run)
    if progress["step"] != step:
        blocked(f"the current step is {progress['step']}, not {step}.", progress)
    if step == "repo":
        given = " ".join(args).strip()
        path = Path(given).expanduser().resolve() if given else None
        if not path or not path.is_dir():
            blocked("give the path of the repo the user picked, quoted.", progress)
        top = subprocess.run(["git", "-C", str(path), "rev-parse", "--show-toplevel"], capture_output=True, text=True)
        if top.returncode != 0:
            blocked(f"{path} is not inside a git repo.", progress)
        root = Path(top.stdout.strip())
        progress.update(repo=root.name, repo_path=str(root))
        (RUNS / run / root.name).mkdir(parents=True, exist_ok=True)
    if step == "direction":
        if not " ".join(args).strip():
            blocked("say which direction the user picked, in a few words.", progress)
        progress["direction"] = " ".join(args).strip()
    if step == "rules":
        rules = RUNS / run / progress["repo"] / "invariants.json"
        try:
            saved = json.loads(rules.read_text()).get("invariants")
        except (OSError, ValueError, AttributeError):
            saved = None
        if not isinstance(saved, list) or not saved or not all(
            isinstance(r, dict) and str(r.get("statement", "")).strip() for r in saved
        ):
            blocked(f"no security rule with a statement saved in {rules} yet (or it is not valid JSON).", progress)
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
        try:
            fresh = time.time() - RANKING.stat().st_mtime < RANKING_MAX_AGE
        except OSError:
            fresh = False
        if not fresh and not pending_rankings():
            start_ranking()
        progress = {"run": f"{time.strftime('%Y%m%d')}-{secrets.token_hex(4)}", "step": "repo"}
        save(progress)
        print(render(progress))
    elif len(args) == 2 and args[0] == "next":
        print(render(load(args[1])))
    elif len(args) >= 3 and args[0] == "done" and args[2] in STEPS:
        done(args[1], args[2], args[3:])
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()
