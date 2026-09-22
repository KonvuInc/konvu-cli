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

### The information, not just the look

Matching colours is the easy half. The tables carry the product's own columns,
because what you need to know about an endpoint group is not what you need to
know about a data object, and `InventoryAssetTables.tsx` defines three shapes,
not one generic list.

| View | Columns | Default sort |
|---|---|---|
| Endpoints | Endpoint group (name + `RoutePreview`) · Endpoints (route count) · Control coverage | gaps first (`absent*1000 + partial`, descending) |
| Data objects | Data object (name + location) · Fields (count + "N carrying controls") · Version tables · Control coverage | gaps first |
| Controls | Control · Property · Status · Protects (N assets) | status ascending, partial first |

Three consequences worth stating, because each one was wrong in an earlier draft:

- **A data object is a group, not a row per object.** `dataObjectGroups()` pairs
  a model with the version/audit tables generated for it and the fields it owns,
  and the coverage cell rolls up the model's control links *together with every
  field's*. On the `ihatemoney` baseline that turns 10 raw object assets into 6
  data objects. Fields are therefore not a rail destination: they are folded
  into the object that owns them, exactly as the product does.
- **There is no Kind column.** The rail already scopes to one kind, so a column
  repeating it on every row says nothing. There is no Declared-at column either;
  the declaration is the sub-line under the name, and in full in the panel.
- **Controls lists only implemented ones.** A control nothing implements is an
  expectation the code fails, which is a finding rather than an asset. The
  product omits them; this does too, and names the count in the caveats so the
  omission is visible rather than silent.

One local correction. `data.ts` pairs a version table to its model by `name`,
because the hosted graph names an object after its table. This artifact names it
for a human ("Bill version history" declared at `...#bill_version`), so matching
on `name` alone pairs nothing and the Version tables column is dead in every
row. `objectKey()` prefers the declared symbol and falls back to the name, which
reproduces the product's behaviour under both naming conventions.

### The side panel

`AssetPanel` and `ControlPanel` are a tabbed drawer, not a scroll of sections.

| Element | Source | Rule |
|---|---|---|
| Drawer | `AssetSidePanel` | 48% wide clamped to 460-720, inset 12px, 14px radius, deepPurple-8 scrim at 20%, slide-left 280ms `cubic-bezier(.22,.61,.36,1)`. |
| Header | `SidePanelHeader` | 18px/20px/8px. `PanelTitle` is 18px/600 capitalised deepPurple-7 with a light deepPurple kind badge, then `RepositoryRow`, then a facts row. |
| Facts | `factLabel` | DM Mono, 0.02em, `#8A8399`: CONTROLS with the coverage spread, ENDPOINTS with the route count. |
| Tabs | `ManifestAnalysisModalContent.module.css` | 26px gap, 24px gutter, `#e4e1ea` rule. 14px/400 `#2d1266b3`; active `#2d1266`/600 with a 2px frenchPink underline; counts in DM Mono 11px on `#f3f1f7` pills. |
| Body | `SidePanelBody` | 20px/24px/32px, scrolled. |
| Sub-tables | `SubTable` | Rounded card, `#faf9fc` header band, 7px/12px grid rows on `#EEEBF3` hairlines, a row opens what it names. |
| Protects | `ProtectsList` | Two-column grid at 2px/28px, capped at 6 with a "Show all" toggle, empty copy verbatim. |

Asset tabs are Overview, the relation named for the kind (Data objects for an
endpoint, Fields for an object) and Controls. A control gets Overview and
Protects. Clicking through an asset's Controls tab opens that control's panel,
and a Protects row opens that asset's, so the graph is walkable in both
directions the way the product walks it.

### Shell and navigation

The page is laid out as the app shell, not as a standalone document:

| Element | Source | Rule |
|---|---|---|
| Layout | `_auth.module.css` `.shellSplit` | A transparent app background with the rail OUTSIDE the card and the page inside it. This split is what makes it read as the product rather than a page. |
| Card | `.railCard` | 16px radius, `lch(93 0.5 282)` border, `lch(98.94 0.5 282)` fill, the shell's two-layer shadow, 8px margin. |
| Page header | `PageHeader.tsx` | 62px min-height, 24px gutter, gray[2] hairline, title left and context right. |
| Rail | `TriageRail.module.css` | 200px wide, 10/12/12/16 padding, 14px block gap. Rows 30px at 13px/400 `#4B3E68`, hover `#EBE7F3`, selected `#E4DFEE` at 600 in `#2D1266`, counts in DM Mono 11.5px. |
| Scope well | `.scopeWell` | 10px radius on `#EBE7F1`, holding the asset types. A scope above the filters, not another filter. |
| Empty rows | `.rowDead` | A count of 0 dims to `#B4AEC4` and stops being clickable rather than disappearing. Learning a dead end without entering it is what a facet count is for. |
| Section labels | `.sectionLabel` | 10.5px/700, 0.08em tracking, uppercase, `#A197BC`. |
| Active nav | `_auth.module.css` `.navlink.current` | frenchPink[7] `#FF7397`, **not** purple, with -0.28px tracking. |

**Asset type is a destination, not a filter.** `InventoryAssetRail` lists
Repositories, Manifest Files, Endpoints, Data objects, Controls and Knowledge as
things you navigate to. This page follows that: Endpoints, Data objects, Fields,
Code assets, Controls and URLs are rail destinations, and only coverage and
security property remain filters. An earlier draft had top tabs plus a kind
facet, which was the same choice offered twice in two different idioms.

It lands on Endpoints whenever the repository exposes any, since that is where
the findings live, and otherwise on the densest type that has rows, so the first
screen is never an empty table.

Two known departures, both deliberate. "No control linked" has no counterpart in
the dashboard because the hosted graph does not produce it; it borrows the
absent/gray.5 treatment so it never reads as a failure. And the `ambiguous`
marker has no counterpart at all — `MappedAsset` cannot represent it — so it uses
the dashed `borders.dashed` token rather than inventing a colour.

## What is deliberately not here

The page shows the inventory the product shows, and no more. Everything below
was built at some point and then removed, because the main product UI has no
counterpart for it. Each is a real loss, recorded so nobody re-adds it by
accident or removes the note without knowing the cost.

| Removed | Why it is gone | What it cost |
|---|---|---|
| Run banner (status, commit, languages, counts, duration, spend) | The product has no such bar; `PageHeader` carries a title and a scope chip. | Provenance is no longer visible, including `git.dirty`, which said the map described no commit exactly. |
| "How much of this is verified" strip | No counterpart in the product. | The reader is no longer told that ~79% of assets have no control linked and ~90% are flagged `ambiguous`. |
| `ambiguous` marker on rows | `MappedAsset` cannot represent it, so the product never shows it. | The engine's own uncertainty signal is invisible; identification now reads as fact. |
| URLs view and the catch-all marking | Not an asset type in the product. | Searching a literal path no longer finds the group covering it, which was the customer's second complaint. The token→route join still runs, feeding the route preview and the Endpoints count. |
| Code assets destination | `SECURITY_GRAPH_ASSET_TYPES` lists endpoints, data objects and controls only. | 10-15 code assets per baseline are unreachable. |
| Rail counts, icons, coverage and property filters | `InventoryAssetRail` has none: a "Filters" header and a plain "Asset type" list. | No facet counts, and no way to filter to the gaps. |
| Panel: identity block, match patterns, raw source | `AssetOverview` shows what it is, its location, its endpoints and its controls. | Token patterns and asset ids are no longer inspectable. |
| Keyboard hint footer | Not in the product. | The `j`/`k`/`/`/`Esc` bindings still work, they are just undocumented on screen. |

If any of these should come back, the argument is the same one that put them
there: the artifact is thinner than a table makes it look, and the hosted graph
does not carry the caveats this one does.

## Security## Security

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
