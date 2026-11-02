# One security rule, and its fields

`konvu invariants guide` defines a security rule, the right level of detail and how to rank candidates. This file only adds what is specific to this skill.

## One rule

Keep the top one: the onboarding is about a single, very good security rule. Two candidates that share a why are one rule. Don't restate the worst outcomes in `context.json` word for word: a rule is the promise that prevents one, phrased as something that's always true.

## Fields

Save `invariants.json` as `{"invariants": [ ... ]}`, one object per security rule, with the fields Konvu stores:
- `statement`: the rule, at most 200 characters
- `why_it_matters`: one or two plain sentences, at most 500 characters: what breaking it costs the business
- `property`: the one that fits best: `authentication`, `authorization`, `confidentiality`, `integrity`, `availability` or `non_repudiation`
- `protected_assets` (optional): `[{"kind": "...", "name": "..."}]`, the things it protects in plain words ("credential" / "Claude login token")

And, for you only (konvu-upload-invariants leaves them out): `based_on`, one sentence on what it comes from, and `evidence`, 2 or 3 short sources.
