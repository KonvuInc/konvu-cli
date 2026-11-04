The user is happy with the security rule in `{dir}/invariants.json`. Saving it to Konvu is the only part that needs a Konvu account, so ask first. Your message is only this question, in bold, in your own words, along the lines of: **Want to save this security rule to Konvu? You'll create your free account (or sign in) in the browser.** End your message there and wait.

- On yes: load the **konvu-upload-invariants** skill (it sits next to this one) and follow it to add this security rule. They already agreed to the rule, so don't ask them to confirm it again: the skill logs them in if needed, then saves it.
- On no: tell them in one line the rule stays on this machine, in `{dir}/invariants.json`, and they can type "upload" anytime to save it. Stay on this step.
- When they type "upload" (after a no, or after a login that failed or timed out): follow konvu-upload-invariants again from its login step.

When it reports the rule was added, run the done command below in the same turn (don't wait for another reply), then follow what it prints.
