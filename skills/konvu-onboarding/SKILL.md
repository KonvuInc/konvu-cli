---
name: konvu-onboarding
version: 1.0.0
description: "Konvu Prevent onboarding in the coding agent: a welcome, then the user and their agent write their first security rule with konvu-invariants and save it to Konvu, then they see how Konvu enforces it. Use when the user says /konvu-onboarding, asks to start the Konvu onboarding, or right after installing Konvu."
metadata:
  requires:
    bins: ["konvu", "python3", "git"]
---

# Konvu onboarding

Three parts, in order: the welcome (this skill), the first security rule (the **konvu-invariants** skill, which saves it with **konvu-upload-invariants**), then the ending (this skill). `<invariants-dir>` below is the konvu-invariants skill's folder, next to this one.

## 1 · Welcome

First run `python3 <invariants-dir>/scripts/invariants.py prepare`: it ranks the user's repos in the background while they read. Then your message is the welcome below, exactly as is (code block included), and nothing else: no plan, no search. If the user's instruction files (`CLAUDE.md`, `AGENTS.md`, rules files) ask for very short or terse answers, add one line above it: "Heads up: your settings ask for short answers, so parts of this onboarding may come out trimmed."

```
 _  _____  _   ___     ___   _
| |/ / _ \| \ | \ \   / / | | |
| ' / | | |  \| |\ \ / /| | | |
| . \ |_| | |\  | \ V / | |_| |
|_|\_\___/|_| \_|  \_/   \___/   PREVENT

We catch the security breaks your scanners miss, in your code.
```

**Welcome to Konvu Prevent**

Scanners catch known bad patterns. The breaks that hurt are specific to your product and your business, and only you know which ones matter. Konvu lets you write them down as **security rules**, then enforces them in your code on every change, in CI and in your coding agent.

In this onboarding, we write your first security rule together, to see how it works. You can add as many as you want later: that's how you strengthen security across your repos, one security rule at a time. I'll do the digging and suggest where to start; you decide what matters.

**Let me know when you're ready, and we'll start.**

End your message there and wait.

## 2 · The first security rule

When they're ready, load the **konvu-invariants** skill and follow it from its first step (`invariants.py start`), all the way through saving the rule. It tells you when it's done and sends you back here.

## 3 · The ending

Your message, in this order:
1. the progress bar from `python3 <invariants-dir>/scripts/show.py progress 3`, pasted unchanged with its code fences
2. the congratulations as a bold title on its own line, **Congrats, we just wrote your first security rule together.**
3. the wow, in one or two lines, along the lines of: "Now you can make sure it's never broken again. Plug in the GitHub App, and Konvu guarantees it on every change, in CI and in your coding agent."
4. two or three plain lines on how: Konvu builds a security graph of their whole code and finds the controls that keep this security rule true (the concrete checks in their code); then any change that would break it is caught: a PR that breaks it is blocked in CI, and their coding agent is stopped as the code is written
5. the diagram from `python3 <invariants-dir>/scripts/show.py enforce <repo>`, pasted unchanged with its code fences (recommended)
6. one line, along the lines of: "That's one security rule. You can write as many as you want, here in your coding agent or anywhere else, and Konvu enforces them all."
7. the action in bold, in your own words for the moment, e.g. **Want this one guaranteed? Tell me and I'll open Konvu so you can plug in the GitHub App.**

When they want to go on, open Konvu for them: `python3 -m webbrowser https://app.konvu.com`. Then close in two short lines: Konvu is open at https://app.konvu.com to plug in the GitHub App, and they can write more security rules anytime with /konvu-invariants.
