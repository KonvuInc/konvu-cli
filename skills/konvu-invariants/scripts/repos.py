#!/usr/bin/env python3
"""Rank the repos this user has worked on, by how much and how recently: local clones plus GitHub if `gh` is signed in.

Each of the user's commits (or PRs on GitHub) counts for 2^(-age in days / 30), so recent work weighs most;
repos the user hasn't touched in 180 days are left out. Prints the top 10 as JSON, best first. Read-only.
"""

from __future__ import annotations

import calendar
import json
import subprocess
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from urllib.parse import urlsplit

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


def identities(repo: Path) -> tuple[set[str], str]:
    """The user's effective git emails (lowercased) and name in this repo (its own config, then global),
    matched exactly against each commit's author."""
    emails = {e.lower() for e in run(["git", "-C", str(repo), "config", "--get-all", "user.email"]).split()}
    return emails, run(["git", "-C", str(repo), "config", "user.name"]).strip()


def local_repos() -> list[Path]:
    """Git repos and worktrees under home. A .git folder that hasn't changed within the window is skipped
    (a cheap pre-filter before reading history); a worktree's .git file is a pointer, so its history decides."""
    out = run(["find", str(HOME), "-maxdepth", "6", "-name", ".git", "-prune",
               "-not", "-path", "*/Library/*", "-not", "-path", "*/node_modules/*", "-not", "-path", "*/.cache/*"], timeout=30)
    cutoff = NOW - WINDOW_DAYS * 86400
    repos = []
    for line in out.splitlines():  # one path per line: paths may contain spaces
        try:
            git = Path(line)
            if git.is_file() or git.stat().st_mtime > cutoff:
                repos.append(git.parent)
        except OSError:
            continue
    # Skip clones inside hidden folders (tool caches, agent scratch copies).
    return [r for r in repos if not any(part.startswith(".") for part in r.relative_to(HOME).parts)]


def origin(repo: Path) -> str:
    """The repo's identity: owner/name on github.com (the key `gh` uses), host/path on any other host,
    following local-path remotes; the path itself when there's no remote."""
    url = run(["git", "-C", str(repo), "remote", "get-url", "origin"]).strip()
    if url.startswith("/") and Path(url).is_dir():
        url = run(["git", "-C", url, "remote", "get-url", "origin"]).strip() or url
    if "://" in url:
        parts = urlsplit(url)
        host, path = parts.hostname or "", parts.path
    elif ":" in url and not url.startswith("/"):  # scp-like: git@github.com:owner/name.git
        host, path = url.split(":", 1)
        host = host.rsplit("@", 1)[-1]
    else:
        return str(repo)
    path = path.strip("/").removesuffix(".git").lower()
    host = host.lower()
    return path if host == "github.com" else f"{host}/{path}"


def local_score(repo: Path) -> dict[str, object] | None:
    emails, name = identities(repo)
    out = run(["git", "-C", str(repo), "log", "--all", f"--since={WINDOW_DAYS}.days", "--format=%ct%x09%ae%x09%an"])
    stamps = []
    for line in out.splitlines():
        fields = line.split("\t")
        if len(fields) != 3:
            continue
        mine = (not emails and not name) or fields[1].lower() in emails or (name and fields[2] == name)
        if fields[0].isdigit() and mine:
            stamps.append(int(fields[0]))
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
        ts = calendar.timegm(time.strptime(pr["updatedAt"][:19], "%Y-%m-%dT%H:%M:%S"))  # updatedAt is UTC
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
    with ThreadPoolExecutor(16) as ex:
        local = [r for r in ex.map(local_score, local_repos()) if r]
        gh = ex.submit(github_scores).result()
    # Several clones or worktrees of one repo share its history: keep the most active one, the main checkout on a tie.
    by_origin: dict[str, dict[str, object]] = {}
    for r in sorted(local, key=lambda r: (r["score"], (Path(str(r["path"])) / ".git").is_dir()), reverse=True):
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
