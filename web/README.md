# Konvu inventory viewer

A browser view of one `baseline.json`, for the scale where the terminal
workspace stops being browsable. A customer running ~620 assets / 275 routes
built their own HTML viewer rather than page through the TUI; this is that
viewer, owned here.

No React, no Vite, no node, no npm. One HTML file with the artifact inlined,
served or written by the CLI. `<table>` renders 600 rows without help, and the
browser already has find, sort and filter.

## What is here

| File | Role |
|---|---|
| `viewer.html` | The page. Two substitution markers: `/* @@TOKENS@@ */` and `/* @@BASELINE@@ */`. |
| `tokens.css` | Konvu design tokens as CSS custom properties, generated from `konvu-core/dashboard/src/theme.ts`. |
| `build.py` | Development harness — substitutes both markers and writes a standalone file. |

The shipped path will be a Go command doing the same two substitutions against
the `go:embed`'d template. `build.py` exists so the page can be iterated on
without a Go build; it is not the product.

## Try it

```bash
python3 web/build.py ~/.konvu/guardrails/baselines/<run>/baseline.json -o /tmp/inventory.html
open /tmp/inventory.html

# strip verbatim source excerpts, keeping file#symbol
python3 web/build.py <baseline.json> --redact -o /tmp/inventory.html
```

## Why it shares tokens and not components

The dashboard's inventory tables cannot be reused here. `mappedAssets/` reaches
into route directories and `enforcement.ts` pulls a generated hook against the
authenticated backend, so the component tree does not run offline. Its
`MappedAsset` type is also the wrong shape for this artifact: it has no slot for
`decl`, `quote`, `origin`, `source_ids` or `ambiguous`, and it requires `routes`,
which is populated on about 1 asset in 100.

So the only thing shared is `tokens.css` — values, no runtime, no release
coupling. Regenerate it when the dashboard palette moves.

## Keeping the look identical

The page is a deliberate reimplementation of the product's mapped-asset UI, not
a lookalike. Every visual decision below is copied from a named source in
`konvu-core/dashboard`, so the two can be diffed by reading rather than guessed
at. When one of these changes in the dashboard, change it here.

| Element | Source | Rule |
|---|---|---|
| Presence | `presence.tsx` `PresenceTag` / `presenceColor` | A 6px dot plus a capitalised label. present → malachiteGreen.6, partial → sunnyYellow.8, everything else → gray.5. **Never a filled pill.** |
| Coverage | `presence.tsx` `CoverageSummary` | Three dot+count items, DM Mono 11px, gray.5 at zero. Absent always rendered even at zero, bold when non-zero. |
| Asset kind | `presence.tsx` `AssetKindIcon` / `assetKindLabels` | A lucide glyph at stroke 1.7 in deepPurple-5 plus the product noun: Endpoint group, Data object, Field, Code asset. Kind carries an icon, never a colour. |
| Security property | `styles.module.css` `.propertyAccent` | authorization → deepPurple-5, authentication → ghostBlue-8, confidentiality → frenchPink-8, integrity → malachiteGreen-6, availability → sunnyYellow-8, non-repudiation → royalBlue-5. |
| Control card | `styles.module.css` `.propertyGroup` / `.propertyHeader` | 3px accent left border; header tinted `color-mix(accent 7%, white)` with `20%` hairlines, min-height 34px. |
| Route | `styles.module.css` `.method` + `RoutePreview` | Method in DM Mono 10px/600 ghostBlue-8, path in mono gray.6 clamped to one line, then `+N`. |
| Panel sections | `SectionCard.tsx` | 14px radius, `#E9E5F1` border, 18px/15px header, 15px/600 deepPurple-7 heading, optional count badge. |
| Hairline group | `AssetOverview.tsx` `HairlineGroup` | `#EEEBF3` top border, 12px padding, uppercase 11px/700 ghostBlue-8 eyebrow. |
| Rows | `styles.module.css` `.assetRow` | mist-2 hairline, `lch(99.35 0.25 282)` hover, 2px deepPurple-3 focus ring inset. |

Two known departures, both deliberate. "No control linked" has no counterpart in
the dashboard because the hosted graph does not produce it; it borrows the
absent/gray.5 treatment so it never reads as a failure. And the `ambiguous`
marker has no counterpart at all — `MappedAsset` cannot represent it — so it uses
the dashed `borders.dashed` token rather than inventing a colour.

## What the page refuses to imply

The artifact is thinner than a table makes it look, and rendering it silently
would be worse than a terminal that cannot render it at all. Three deliberate
behaviours:

- **"No control linked" is its own state**, neutral, never red, and counted in
  the header strip. It records an absence of attribution, not an absence of
  protection.
- **`ambiguous` is surfaced per row.** It is set on ~90% of assets, so it cannot
  serve as a verified/unverified signal — the page says so rather than hiding it.
- **Catch-all tokens never count as coverage.** See below.

## URL search, and the join behind it

Searching a literal path is the second thing the customer asked for: groups store
regex patterns, so a real URL looks like a miss even when covered.

The page resolves it. Endpoint assets carry `tokens[]` (regex fragments) and
`routes[]` is a separate collection of literal paths; `literalPaths()` expands
alternation groups (`/(password-reminder|reset-password)` → two paths) and joins
them. On the `ihatemoney` baseline that resolves **15 of 17 endpoint assets** and
attributes **28 of 29 routes**.

The join is only sound when a token is specific. Token lists frequently end in a
bare catch-all (`@main\.route\(`, `restful_api\.add_resource\(`) that would
attribute every route in the repository to one asset — asserting "public home
covers /admin", which the baseline does not claim. A token resolving to more than
`BROAD_RATIO` of all routes is kept but marked broad, rendered dashed, and never
counted as coverage.

**This is a workaround for a schema gap.** `tokens[]` mixes a precise pattern and
a fallback with nothing to distinguish them. One field ranking token specificity
would turn this heuristic into a sound index and let the page state group
membership as fact. Until then the dashed styling is the honest rendering.

## Security

`quote` and `decl` are verbatim source from the user's repository. Two
consequences the code handles:

- **Inlining.** A repository may legitimately contain `</script>`. `build.py`
  escapes every `<` to a `<` JSON escape when serializing into the
  `<script type="application/json">` block, so no quote can terminate the
  element early, and every render path goes through `esc()`. Regression-test by
  poisoning a baseline's quotes and confirming nothing executes.
- **Sharing.** A written file is a *new copy* of proprietary source, outside the
  0600 artifact and outside whatever DLP the org already bought. There is no
  unshare. `--redact` (source stripped, `file#symbol` kept) is the right default
  for anything leaving the machine; the Go command should make the unredacted
  form an explicit, warned opt-in.

## Not done yet

- The Go command (`konvu inventory view`): `go:embed` the template, serve on
  `127.0.0.1:0` with a Host-header check and idle shutdown, open the browser.
- A golden-fixture snapshot test shared with the dashboard, so the two renderers
  can diverge freely but the schema cannot drift unnoticed.
