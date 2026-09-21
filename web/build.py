#!/usr/bin/env python3
"""Build a standalone Konvu inventory viewer from one baseline.json.

Development harness for `web/viewer.html`. The shipped path is the Go command,
which does the same two substitutions against the go:embed'd template; this
exists so the page can be iterated on without a Go build.

    python3 web/build.py <baseline.json> [-o out.html] [--redact]

--redact strips `quote` fields (verbatim source excerpts) and keeps `decl`
(file#symbol). That is the default posture for any file meant to leave the
machine: the reader who needs the source has the repository.
"""

import argparse
import json
import pathlib
import sys

HERE = pathlib.Path(__file__).resolve().parent


def strip_quotes(node):
    """Recursively drop `quote` keys. Mutates in place, returns the count."""
    n = 0
    if isinstance(node, dict):
        if "quote" in node:
            del node["quote"]
            n += 1
        for v in node.values():
            n += strip_quotes(v)
    elif isinstance(node, list):
        for v in node:
            n += strip_quotes(v)
    return n


def inline_json(data):
    """Serialize for embedding in <script type="application/json">.

    `<` is escaped to \\u003c so a source `quote` containing `</script>` cannot
    terminate the element. This is the one XSS-shaped hazard in the whole page:
    the payload is verbatim source from the user's own repository.
    """
    raw = json.dumps(data, separators=(",", ":"), ensure_ascii=False)
    return raw.replace("<", "\\u003c").replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")


def build(baseline_path, redact=False):
    data = json.loads(pathlib.Path(baseline_path).read_text(encoding="utf-8"))
    if data.get("schema_version") != 1:
        print(
            f"warning: schema_version is {data.get('schema_version')!r}, expected 1",
            file=sys.stderr,
        )

    removed = strip_quotes(data) if redact else 0

    template = (HERE / "viewer.html").read_text(encoding="utf-8")
    tokens = (HERE / "tokens.css").read_text(encoding="utf-8")

    if "/* @@TOKENS@@ */" not in template or "/* @@BASELINE@@ */" not in template:
        sys.exit("viewer.html is missing a substitution marker")

    html = template.replace("/* @@TOKENS@@ */", tokens)
    html = html.replace("/* @@BASELINE@@ */", inline_json(data))
    return html, removed


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("baseline")
    ap.add_argument("-o", "--out", default="inventory.html")
    ap.add_argument("--redact", action="store_true", help="strip verbatim source quotes")
    args = ap.parse_args()

    html, removed = build(args.baseline, args.redact)
    out = pathlib.Path(args.out)
    out.write_text(html, encoding="utf-8")
    out.chmod(0o600)

    size = len(html.encode("utf-8"))
    note = f", {removed} source quote(s) stripped" if args.redact else ""
    print(f"wrote {out} ({size / 1024:.0f} KB{note})")
    if not args.redact:
        print("note: contains verbatim source excerpts — treat as source code")


if __name__ == "__main__":
    main()
