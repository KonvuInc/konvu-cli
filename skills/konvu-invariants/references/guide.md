<!-- Copy of KonvuTeam/konvu-guardrails backend/services/invariant_proposal/guide.md (main @ 79b3b0c): change both together. Everything below this line is that file, unchanged. -->
## What a security rule is

A security rule is one sentence about the product that must always be true, written in the
business's words, that the code can be checked against. Konvu's proposal engine calls it an
invariant. It must be:
- **A promise, not a mechanism.** "A customer never sees another customer's invoices", not "every
  query filters on org_id". If it names a function, a column or a library, it's a check, not a
  rule.
- **About this company.** It names what they hold (invoices, tokens, findings, payouts) and who must
  never get it. One that fits any company ("inputs are validated") is out.
- **True as written.** "Data is deleted on request" is false while backups keep it 30 days. State
  only what you checked: if a detail varies (who hosts a component, where data lives) or you aren't
  sure of it, leave it out of the sentence. Scope it to what you checked: if the code also sends the
  thing elsewhere for a legitimate reason (code to GitHub, a message to Slack), say the channel the
  promise is about ("to a model only through…") instead of "only to".
- **Checkable.** There is code or configuration where it holds or breaks.
- **Security, not correctness.** When it breaks, someone gets access, data, credentials or control
  they should not have: another customer, an outsider, an agent acting on untrusted input. A wrong
  verdict, a double charge, a stale cache, a size limit or an incompatible version is a bug, not a
  security rule, even under the `integrity` property. Drop it. The same goes for one whose
  breaking only makes the product stricter or stop working (a stale rule that still blocks, a
  revoked credential still applied as a restriction): that fails safe.
- **Short and plain.** Under 110 characters: one subject, one verb, one condition. Use the words the
  team uses for its own things (the token, the analysis, the PR), never a metaphor ("the keys that
  open"), a made-up role ("agents doing the customer's work") or a vague catch-all ("anything it
  writes"). Name the specific credential or destination when you know it. A teammate reads it cold
  and understands it at once.
- **The code's own names.** Name components, credentials and destinations exactly as the code and
  its docs name them (the broker, the controller, the SCA agent, the patcher, the dependency proxy,
  the Azure OpenAI deployment), never a stand-in such as "the analysis agent", "Konvu services",
  "its configured provider" or "the credentials that run it". The sentence says who could do what
  harm: "X belongs only to Y" says nothing; say what the product may do to X, and for whom. When
  it is about what a component must never do, make that component the subject, in the active voice
  ("the dependency proxy attaches…", "the patcher pushes…"), not a passive or an object ("files are
  read by…", "its checkout"). Name exactly who may act, each role the code allows ("a BB8
  superuser or the company's Core admin"), never a blanket term ("Konvu administrators"), and say
  who sets anything "configured" ("the endpoint the company set in its AI settings"). If a code
  name means nothing outside the team (steering, an inherited OTLP endpoint), add what it is in a
  few words. Check that a role exists in this code before you name it.
- **Plain statement, exact details below it.** The statement may name components and roles as the
  team does (the patcher, the graph mapper, a BB8 superuser), but environment variables, function
  names, scopes, headers and paths go in `why_it_matters`, never in the statement. No exceptions in
  the sentence either ("outside development", "non-superuser"): state what must hold, and name the
  exception as the risk in `why_it_matters`.

Baseline hygiene the platform already guarantees (password hashing, lockout, audit logs) is not a
top concern unless a reported vulnerability or the threat model shows it breaking. Neither is what
a third-party platform enforces for you (the identity provider refusing a deactivated login, GitHub
signing its webhooks) unless this code can bypass it: state the part this code owns instead (live
sessions and API keys of a removed member, the signature check on every webhook route).

## The right level of detail

State the **outcome** that must hold for a real part of this product (what is protected, where, and
who may see or do it), not the **mechanism** that makes it hold today. From a made-up invoicing
product:
- Too broad: "Customer data is always protected." Fits any company, can't be checked as one thing.
- Right: "A company's invoices are visible only to its own users and to support staff."
- Too low: "Support staff can switch into a customer's account after approval, and then only that
  account's invoices show." That's how it works today: a sequence of steps.

Checks before you keep a candidate:
- **The rewrite test.** If the team rebuilt this feature another way, would it still read the same?
  If not, lift it to the outcome that mechanism serves.
- **The paste test.** If it could be pasted into another company's list unchanged, it's too broad.
- **No steps.** No "and then", no "after": one condition that is always true.
- **Who, not how.** Say who may see or do it, not how they're checked (a switch, a token, a filter).
- **What's protected comes first.** Make the protected thing the subject and list everyone allowed.
- **The class test.** If the same harm can happen through sibling paths (every secret verifier,
  every registry client, every webhook route), write it for all of them, not the one you read.

When what you found is a mechanism, keep it as the evidence behind the rule and write the rule as
the outcome it protects.

## Sentence shape

The protected thing with its owner as the subject, a simple verb of where it may go or what may
happen to it, then the named places or people. Lead with the fact:
- "A customer's integration tokens never show up in Sentry, PostHog or our logs."
- "The user's login token goes only to the provider that issued it."
- "A customer's tests are never removed or skipped by a fix PR."

Never an abstract subject ("Access to a company's code is never within reach of…"), a chain of
conditions ("passes only once each finding is fixed or accepted by someone with…") or an unclear
actor ("the repo's code", "its packages", "a viewer" when two kinds exist).

## Does the product need the opposite?

If the product legitimately has to do what the rule forbids to work at all (a build step that needs
a registry token, a fix job that must push, a support tool that must read customer data), it is
false as written: narrow it to what must truly never happen, or drop it.

## What makes a strong one

Something the team recognises instantly (a tool, a feature, an artifact of their product) paired
with a concrete harm that would be embarrassing if it happened: "never reaches", "never weakens",
"only when their team merges". At equal attacker gain, prefer what customers trust the company with
over internal plumbing. A generic tenancy rule ("a company's data is seen only by its own users") is
true but bland. Never drop it for a narrower one that protects less: name the data and the part of
this product it lives in instead.

## Risks that are easy to miss

Check these wherever the product has them:
- who inside a customer's account may administer it or change its security settings (a role, not
  mere membership);
- access that must end when an install, a connection, a key or a member is removed;
- customer code, secrets or data sent to a model provider, a trace service or another third party:
  name the destinations allowed.

## How to rank

Rank by **what an attacker gains when the rule breaks, first and by far**: credentials and secrets,
another customer's data, admin rights, code execution or an escape from a sandbox come before
anything that only harms one record or one run. A company-owned key whose misuse only costs the
company money ranks below anything that exposes a customer's data, code or credentials. Credentials
reaching untrusted code or another customer, and one customer reaching another, come before where
customer data may go. Within the same gain, rank by how thin today's defence is: a single check, a
guard in shadow or log-only mode, a fallback path, a default, or a prompt-only instruction ranks
above a fixed config pin or a hardcoded allowlist. Within that, prefer the promise people buy this
product for, the headline if it broke. Being specific to this product, or having many code paths,
does not raise a rank. Break ties by likelihood (how easily a normal change breaks it), then by
evidence.
