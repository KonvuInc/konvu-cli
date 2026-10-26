#!/usr/bin/env python3
"""Rank the repos this user has worked on, by how much and how recently: local clones plus GitHub if `gh` is signed in.

Each of the user's commits (or PRs on GitHub) counts for 2^(-age in days / 30), so recent work weighs most;
repos the user hasn't touched in 180 days are left out. Prints the top 10 as JSON, best first. Read-only.
"""

from __future__ import annotations

import json
import subprocess
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

HOME = Path.home()
WINDOW_DAYS = 180
HALF_LIFE_DAYS = 30
NOW = time.time()


def run(cmd: list[str], timeout: int = 15) -> str:
    try:
        return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout).stdout
    except (OSError, subprocess.TimeoutExpired):
        return ""


def weight(ts: float) -> float:
    return 2 ** (-(NOW - ts) / 86400 / HALF_LIFE_DAYS)


def identities() -> list[str]:
    ids = run(["git", "config", "--global", "--get-all", "user.email"]).split()
    name = run(["git", "config", "--global", "user.name"]).strip()
    return ids + ([name] if name else [])


def local_repos() -> list[Path]:
    """Git repos under home whose .git changed within the window (a cheap pre-filter before reading history)."""
    out = run(["find", str(HOME), "-maxdepth", "6", "-type", "d", "-name", ".git", "-prune",
               "-not", "-path", "*/Library/*", "-not", "-path", "*/node_modules/*", "-not", "-path", "*/.cache/*"], timeout=30)
    cutoff = NOW - WINDOW_DAYS * 86400
    repos = []
    for line in out.splitlines():  # one path per line: paths may contain spaces
        try:
            if Path(line).stat().st_mtime > cutoff:
                repos.append(Path(line).parent)
        except OSError:
            continue
    # Skip clones inside hidden folders (tool caches, agent scratch copies).
    return [r for r in repos if not any(part.startswith(".") for part in r.relative_to(HOME).parts)]


def origin(repo: Path) -> str:
    """owner/name of the repo's origin, following local-path remotes; the path itself when there's no remote."""
    url = run(["git", "-C", str(repo), "remote", "get-url", "origin"]).strip()
    if url.startswith("/") and Path(url).is_dir():
        url = run(["git", "-C", url, "remote", "get-url", "origin"]).strip() or url
    url = url.removesuffix(".git").replace(":", "/")
    return "/".join(url.split("/")[-2:]).lower() if "github" in url or "gitlab" in url else str(repo)


def local_score(repo: Path, who: list[str]) -> dict[str, object] | None:
    args = ["git", "-C", str(repo), "log", "--all", f"--since={WINDOW_DAYS}.days", "--format=%ct"]
    for w in who:
        args += ["--author", w]
    stamps = [int(t) for t in run(args).split() if t.isdigit()]
    if not stamps:
        return None
    return {"name": repo.name, "path": str(repo), "origin": origin(repo), "your_commits": len(stamps),
            "last_touched_days": int((NOW - max(stamps)) / 86400), "score": round(sum(weight(t) for t in stamps), 1)}


def github_scores() -> dict[str, dict[str, object]]:
    """The user's PRs from the last 180 days on GitHub, by repo: catches repos that aren't cloned here."""
    try:
        if subprocess.run(["gh", "auth", "status"], capture_output=True, timeout=10).returncode != 0:
            return {}
    except (OSError, subprocess.TimeoutExpired):
        return {}
    since = time.strftime("%Y-%m-%d", time.gmtime(NOW - WINDOW_DAYS * 86400))
    raw = run(["gh", "search", "prs", "--author=@me", f"--updated=>={since}", "--limit", "200",
               "--json", "repository,updatedAt"], timeout=30)
    try:
        prs = json.loads(raw or "[]")
    except ValueError:
        return {}
    repos: dict[str, dict[str, object]] = {}
    for pr in prs:
        full = pr["repository"]["nameWithOwner"]
        ts = time.mktime(time.strptime(pr["updatedAt"][:19], "%Y-%m-%dT%H:%M:%S"))
        r = repos.setdefault(full, {"name": full.split("/")[-1], "github": full, "your_prs": 0, "score": 0.0, "last": 0.0})
        r["your_prs"] += 1
        r["score"] += weight(ts)
        r["last"] = max(r["last"], ts)
    return repos


def main() -> None:
    try:
        rank()
    except Exception as e:  # the onboarding must go on without a ranking
        print(json.dumps({"error": f"ranking failed: {e!r}"}))


def rank() -> None:
    who = identities()
    with ThreadPoolExecutor(16) as ex:
        local = [r for r in ex.map(lambda p: local_score(p, who), local_repos()) if r]
        gh = ex.submit(github_scores).result()
    # Several clones or worktrees of one repo share its history: keep the most active one.
    by_origin: dict[str, dict[str, object]] = {}
    for r in sorted(local, key=lambda r: r["score"], reverse=True):
        by_origin.setdefault(str(r["origin"]), r)
    for full, g in gh.items():
        r = by_origin.get(full.lower())
        if r:
            r["your_prs"] = g["your_prs"]
            r["score"] = round(r["score"] + g["score"], 1)
        else:
            by_origin[full.lower()] = {"name": g["name"], "origin": full, "cloned": False, "your_prs": g["your_prs"],
                                       "last_touched_days": int((NOW - g["last"]) / 86400), "score": round(g["score"], 1)}
    top = sorted(by_origin.values(), key=lambda r: r["score"], reverse=True)[:10]
    print(json.dumps(top, indent=1))


if __name__ == "__main__":
    main()
