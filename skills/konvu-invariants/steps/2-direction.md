The user picked **{repo}** ({repo_path}).

Start your message with one line: you're digging into {repo}, a couple of minutes. Then do the deep pass, read-only, in parallel if you can:
- **the company**: what it sells, to whom, who its customers are, what they trust it with, what it promises them, what happens to the business if that breaks (the web, what you already know)
- **the repo**: what it does for the company, its main flows, what data and credentials it touches, how customers are kept apart, what it writes to or sends outside
- **what's on the team's mind**: recent work and discussions on this repo (git history, connected tools like Slack, Notion, Linear, GitHub): incidents, risky changes, things that broke or almost did

Save `{state}/{repo}/context.json`: `{"company", "trust": one plain sentence on who trusts the company with what, "worst": [the 3 worst outcomes, plain words], "facts": [key facts with sources]}`.

From that, pick 3 **directions**: areas of {repo} where security rules would protect something that matters to this business and to this user. Each is a concrete area of the product (a flow, a feature, a service in a monorepo), not a security category: "how customer tokens reach our brokers", not "access control".

Rank them by importance, first and by far: which one protects the promise people buy or install this product for, the thing that would be the headline if it broke (customer credentials leaking, code pushed where it shouldn't be, private data leaving the machine)? Put that one first. What the user worked on recently, or what fills their memory and notes, tells you what they know, not what matters most: never rank a direction first for that reason alone. A secondary feature, however busy in the code, comes after the product's core promises.

Name each direction with the words the team uses: the real tools, services and features in this code (PostHog, the Claude token, session limits, the GitHub App), and say concretely what happens. Never abstract system words. For example:
- "What we send to PostHog", not "when the analytics client is allowed to send"
- "How we keep the Claude token we use to show session limits", not "how Claude credentials move through the system"
- "The lines we add to your Claude and Codex settings when you install", not "what setup writes into config files"
Named tools and features are good; file, function and variable names are not.

Your message, in this order: the progress bar from `python3 {skill_dir}/scripts/show.py progress 1`, pasted unchanged with its code fences; a bold title on its own line, **Where should we start?**; one line on what this repo does for the business and what users trust it with; the 3 directions as **1**, **2**, **3**, each a bold name in plain words followed by two short sentences at most: why it matters (what goes wrong for the business or its users if it breaks) and why you picked it (their recent work, an incident, a promise they make). Business words only: no file, function or variable names, no PR numbers, no implementation detail. Then one line on which you'd start with and why. End with the action in bold: **pick one, or tell me what's on your mind.** If their answer is vague, ask one short question to narrow it down.
