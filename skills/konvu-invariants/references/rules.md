# What a security rule is, and how to rank them

## Definition

A **security rule** ("rule" below) is one sentence about the user's product that must always be true, written in the business's words, and that their code can be checked against. It lives inside the direction the user picked.

It must be:
- **A promise, not a mechanism.** "A customer never sees another customer's invoices", not "every query filters on org_id". If it names a function, a column or a library, it's a check, not a rule.
- **About this company.** It names what they hold (invoices, tokens, findings, payouts) and who must never get it. A rule that fits any company ("inputs are validated") is out.
- **True as written.** "Data is deleted on request" is false while backups keep it 30 days. State only what you checked: if a detail varies (who hosts a component, where data lives) or you aren't sure of it, leave it out of the sentence.
- **Checkable.** There must be code or configuration where it holds or breaks. It usually starts in the chosen repo; if it really lives elsewhere (infrastructure code, cloud permissions, another service), look there too, read-only, and base the rule on what you find.
- **Short and plain.** Under 110 characters: one subject, one verb, one condition. Use the words the team uses for its own things (the token, the analysis, the PR), never a metaphor ("the keys that open"), a made-up role ("agents doing the customer's work") or a vague catch-all ("anything it writes"). The test: a teammate reads it cold and understands it at once. If they'd ask "what does that mean?", rewrite it.

## The right level of detail

A rule sits one level above the code you found: it states the **outcome** that must hold for a real part of this product (what is protected, where, and who may see or do it), not the **mechanism** that makes it hold today. The user should read it and think "yes, that's exactly the thing that would hurt", and believe it can be checked.

Three levels, from a made-up invoicing product:
- Too broad: "Customer data is always protected." Fits any company, names no part of the product, can't be checked as one thing.
- Right: "A company's invoices are visible only to its own users and to support staff." One part of the product, what is protected, and exactly who may see it.
- Too low: "Support staff can switch into a customer's account after approval, and then only that account's invoices show." That's how it works today: a sequence of steps, a control.

Checks before you keep a candidate:
- **The rewrite test.** If the team rebuilt this feature a different way, would the rule still read the same? If not, it describes a mechanism: lift it to the outcome that mechanism serves.
- **The paste test.** If it could be pasted into another company's list unchanged, it's too broad: name the part of the product you found.
- **No steps.** No "and then", no "after", no sequence: one condition that is always true.
- **Who, not how.** Say who may see or do it (its own users, support staff, the repo owner), not how they're checked (a switch, a token, a filter).
- **What's protected comes first.** Make the protected thing the subject and list everyone allowed: "A company's invoices are visible only to its own users and to support staff", not "Only support staff can open another company's invoices". The second covers one actor and leaves everyone else unsaid.

When what you found in the code is a mechanism, keep it as the evidence behind the rule, and write the rule as the outcome it protects.

## Sentence shape

The clearest rules share one shape: the concrete thing that's protected, with its owner, as the subject; a simple verb of where it may go or what may happen to it; then the named places or people. Lead with the fact.
- "A customer's integration tokens never show up in Sentry, PostHog or our logs."
- "The user's login token goes only to the provider that issued it."
- "A customer's tests are never removed or skipped by a fix PR."

Avoid what reads backwards or vague:
- an abstract subject: "Access to a company's code is never within reach of…" → name the thing ("A company's repo token…")
- a chain of conditions: "passes only once each finding is fixed or accepted by someone with…" → one condition
- unclear actors: "the repo's code", "its packages", "a viewer" when two kinds exist → say whose and which

## Does the product need the opposite?

Before keeping a candidate, ask whether the product legitimately has to do what the rule forbids to work at all (a build step that needs a registry token, a fix job that must push, a support tool that must read customer data). If it does, the rule is false as written: narrow it to what must truly never happen, or drop it.

## What makes a strong one

The strongest rules pair something the team recognizes instantly (a tool, a feature, an artifact of their product: the analytics service, the fix PR, the default branch, the user's own settings) with a concrete harm that would be embarrassing if it happened: "never reaches", "never weakens", "only when their team merges". Reading it, the user should think "that would be a disaster, and it could actually happen".

A generic tenancy rule ("a company's data is seen only by its own users") is true and important, but it reads as boring because every SaaS has it. Use it only when nothing sharper and more specific to this product exists in the direction.

## Ranking

Draft your candidates, then pick in this order:

1. **Importance, first and by far.** Does it protect the promise people buy or install this product for, the thing that would be the headline if it broke? A flow with many code paths, or the one the user touched last week, is not important for that reason alone: recent work helps choose a direction, never the rule.
2. **Likelihood**, to break ties: how easily a normal change breaks it (many entry points beat one guarded place).
3. **Evidence**, to break ties: the code shows it, and a public promise or a team discussion names it.

Drop anything inferred only, anything that contradicts a fact (a feature the product doesn't have), and anything a teammate would read as minor. Keep the top one: the onboarding is about a single, very good security rule. Two rules that share a why are one rule.

Prefer rules about **what customers trust the company with** over internal plumbing (the company's own API keys, its build pipeline): a rule a customer would care about if they read it. Don't restate the "worst outcomes" as rules word for word: a rule is the promise that prevents an outcome, phrased as something that's always true.

## Fields

Save `invariants.json` as `{"invariants": [ ... ]}`, one object per security rule, with the fields Konvu stores:
- `statement`: the rule, at most 200 characters
- `why_it_matters`: one or two plain sentences, at most 500 characters: what breaking it costs the business
- `property`: the one that fits best: `authentication`, `authorization`, `confidentiality`, `integrity`, `availability` or `non_repudiation`
- `protected_assets` (optional): `[{"kind": "...", "name": "..."}]`, the things it protects in plain words ("credential" / "Claude login token")

And, for you only (konvu-upload-invariants leaves them out): `based_on`, one sentence on what it comes from, and `evidence`, 2 or 3 short sources.
