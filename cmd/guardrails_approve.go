package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var guardrailsApproveCmd = &cobra.Command{
	Use:   "approve <repo>",
	Short: "Approve the access rules drafted for a repository",
	Long: `Approve the access rules drafted for a repository.

Drafted rules start unapproved, and until you approve them a pull request check
reports that it could not judge the change rather than passing or failing it.
Approving says these rules are what you intend, which is what lets a later change
be flagged for breaking one.

By default this approves every drafted rule. Pass --rule to approve only the ones you
name, using the keys 'konvu guardrails show <repo>' prints, so a long list can be worked
through in passes.

For a repository you are approving from scratch, checks start once every rule is
approved: a rule nobody has approved yet counts as access you have forbidden, so
starting earlier would fail pull requests over rules still being read.

Where Konvu already drafted and started checking a repository for you, approving one of
its outstanding rules answers that question without changing anything else.

Read them first with 'konvu guardrails show <repo>'.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu guardrails approve acme/web

  # Approve two rules only
  konvu guardrails approve acme/web --rule "USER|read|Document||02d8a853"

  # A specific branch
  konvu guardrails approve acme/web --branch release-2.3`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGuardrailsApprove,
}

var guardrailsUnapproveCmd = &cobra.Command{
	Use:   "unapprove <repo>",
	Short: "Take back your approval, in whole or rule by rule",
	Long: `Take back your approval, in whole or rule by rule.

Taking back one rule leaves the others in force: the rule you took back stops counting
as access you intend, so a pull request relying on it is flagged.

Taking back all of them returns the repository to a draft, and checks go back to
reporting that they could not judge a change.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  # Take back one rule
  konvu guardrails unapprove acme/web --rule "USER|write|Document||a6466bf8"

  # Take back all of them
  konvu guardrails unapprove acme/web`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGuardrailsUnapprove,
}

var guardrailsExplainCmd = &cobra.Command{
	Use:   "explain <token>",
	Short: "Explain the findings on a pull request",
	Long: `Explain the findings on a pull request.

The token comes from the check's own comment on the pull request. For each finding
this prints what the code checks today, what comparable routes check, and the access
rule covering the resource.

Pass --intent to record what the route should check instead. Nothing is applied to
your rules here: the check re-runs on your next push and clears itself if the code
matches.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu guardrails explain kb-36-9f3a1c

  # Say what the route is supposed to check
  konvu guardrails explain kb-36-9f3a1c --intent "only the owner may read a document"`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGuardrailsExplain,
}

func init() {
	rf := guardrailsApproveCmd.Flags()
	rf.String("branch", "", "branch to act on (default: the repository's default branch, resolved by the server)")
	rf.StringArray("rule", nil, "approve only this rule, as printed by 'show' (repeatable)")
	rf.StringP("output", "o", "", "output format: table, json, or csv")

	uf := guardrailsUnapproveCmd.Flags()
	uf.String("branch", "", "branch to act on (default: the repository's default branch, resolved by the server)")
	uf.StringArray("rule", nil, "take back only this rule, as printed by 'show' (repeatable)")
	uf.StringP("output", "o", "", "output format: table, json, or csv")

	ef := guardrailsExplainCmd.Flags()
	ef.String("intent", "", "what this route should check, in your own words")
	ef.StringP("output", "o", "", "output format: table, json, or csv")
}

func runGuardrailsApprove(cmd *cobra.Command, args []string) error {
	if err := approveFlow(cmd, args); err != nil {
		handleGuardrailsError(err, output.DetectOutputFormat(mustGuardrailsOutput(cmd)))
	}
	return nil
}

func approveFlow(cmd *cobra.Command, args []string) error {
	format := output.DetectOutputFormat(mustGuardrailsOutput(cmd))

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: specify a repository, e.g. 'konvu guardrails approve owner/name'")
		os.Exit(clierrors.ExitUsageError)
	}
	repo := args[0]
	body, err := rulesBody(cmd)
	if err != nil {
		return err
	}

	client := api.NewClient("", "")
	defer client.Close()

	// The route matches on a path, so the repo's slash is part of it and must not be escaped.
	data, err := client.Post(guardrailsAPI+"/dashboard/repos/"+repo+"/ratify", body)
	if err != nil {
		return err
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(data))
		return nil
	}
	fmt.Printf("Approved the access rules for %s@%s\n", getStr(data, "repo"), getStr(data, "branch"))
	printRulesStanding(data)
	return nil
}

// rulesBody is the request approve and unapprove share, so the same --rule value cannot address
// different things depending on the verb.
func rulesBody(cmd *cobra.Command) (map[string]any, error) {
	body := branchParam(requestedBranch(cmd))
	rules, _ := cmd.Flags().GetStringArray("rule")
	named := make([]any, 0, len(rules))
	for _, r := range rules {
		if r = strings.TrimSpace(r); r != "" {
			named = append(named, r)
		}
	}
	// Omitted, not empty: an omitted field asks for every rule, [] for none.
	if len(named) > 0 {
		body["clauses"] = named
		return body, nil
	}
	if len(rules) > 0 {
		// Falling through to an omitted field would turn a mistyped selection into approval of
		// every rule.
		return nil, &clierrors.CLIError{
			Code:       "NO_RULE_NAMED",
			Message:    "--rule was given but named no rule",
			Suggestion: "Copy a rule key from 'konvu guardrails show <repo>', or drop --rule to act on all of them.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	return body, nil
}

func runGuardrailsUnapprove(cmd *cobra.Command, args []string) error {
	if err := unapproveFlow(cmd, args); err != nil {
		handleGuardrailsError(err, output.DetectOutputFormat(mustGuardrailsOutput(cmd)))
	}
	return nil
}

func unapproveFlow(cmd *cobra.Command, args []string) error {
	format := output.DetectOutputFormat(mustGuardrailsOutput(cmd))

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: specify a repository, e.g. 'konvu guardrails unapprove owner/name'")
		os.Exit(clierrors.ExitUsageError)
	}
	repo := args[0]
	body, err := rulesBody(cmd)
	if err != nil {
		return err
	}

	client := api.NewClient("", "")
	defer client.Close()

	data, err := client.Post(guardrailsAPI+"/dashboard/repos/"+repo+"/unratify", body)
	if err != nil {
		return err
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(data))
		return nil
	}
	fmt.Printf("Took back %s rule(s) on %s@%s\n",
		countDisplay(data["withdrawn"]), getStr(data, "repo"), getStr(data, "branch"))
	// The consequence, not the field: "false" alone leaves the reader to work out the cost.
	if stillChecked, _ := getBool(data, "ratified"); !stillChecked {
		fmt.Println("  Pull request checks are paused: they run only while every rule is approved.")
		fmt.Printf("  Approve again with 'konvu guardrails approve %s'.\n", repo)
		if left := countDisplay(data["n_ratified"]); left != "0" && left != "N/A" {
			fmt.Printf("  The %s rule(s) you kept are still on record.\n", left)
		}
		return nil
	}
	printRulesStanding(data)
	return nil
}

// printRulesStanding says how much is approved. A bare "approved" would read as "all of it" once a
// subset is possible.
func printRulesStanding(data map[string]any) {
	pending, ok := data["n_proposals"].(float64)
	if !ok {
		fmt.Println("  Pull requests are now checked against them.")
		return
	}
	fmt.Printf("  %s rule(s) approved, %s still drafted.\n",
		countDisplay(data["n_ratified"]), countDisplay(data["n_proposals"]))
	if pending == 0 {
		fmt.Println("  Pull requests are now checked against them.")
		return
	}
	// Keyed on whether checks actually run, not on the count: a repository Konvu drafted for you is
	// checked with rules still drafted, while one you are approving from scratch is not.
	if checking, _ := getBool(data, "ratified"); checking {
		fmt.Println("  Pull requests are checked. Access the drafted rules would allow counts as")
		fmt.Println("  unintended until you approve them - 'show' lists which.")
		return
	}
	fmt.Println("  Pull request checks stay paused until every rule is approved -")
	fmt.Println("  'show' lists the ones still drafted.")
}

func runGuardrailsExplain(cmd *cobra.Command, args []string) error {
	if err := explainFlow(cmd, args); err != nil {
		handleGuardrailsError(err, output.DetectOutputFormat(mustGuardrailsOutput(cmd)))
	}
	return nil
}

func explainFlow(cmd *cobra.Command, args []string) error {
	intent, _ := cmd.Flags().GetString("intent")
	format := output.DetectOutputFormat(mustGuardrailsOutput(cmd))

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: specify the token from the check's comment on the pull request")
		os.Exit(clierrors.ExitUsageError)
	}

	body := map[string]any{"ratify_token": args[0]}
	if intent != "" {
		body["intent"] = intent
	}

	client := api.NewClient("", "")
	defer client.Close()

	data, err := client.Post(guardrailsAPI+"/ratify", body)
	if err != nil {
		return err
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(data))
		return nil
	}
	printExplain(data)
	return nil
}

func printExplain(data map[string]any) {
	pr, _ := data["pr_number"].(float64)
	fmt.Printf("%s#%d  %s\n", getStr(data, "repo"), int(pr), getStr(data, "pr_title"))
	fmt.Printf("  branch %s at %s\n\n", getStr(data, "branch"), shortSha(getStr(data, "head_sha")))

	flagged := getSlice(data, "flagged")
	if len(flagged) == 0 {
		fmt.Println("No findings on this pull request.")
		return
	}
	for _, f := range flagged {
		m, ok := f.(map[string]any)
		if !ok {
			continue
		}
		// `route` already carries the method here, unlike the review rows which split method
		// and path. Only prepend when it does not.
		route, method := getStr(m, "route"), getStr(m, "method")
		if method != "" && !strings.HasPrefix(route, method+" ") {
			route = method + " " + route
		}
		fmt.Printf("%s\n", route)
		fmt.Printf("  %s may %s %s\n", getStr(m, "role"), getStr(m, "action"), getStr(m, "resource"))
		fmt.Printf("  code checks:     %s\n", orNone(getStr(m, "current_guard")))
		if src := getStr(m, "source"); src != "" {
			fmt.Printf("  at:              %s\n", src)
		}
		if reason := getStr(m, "reason"); reason != "" {
			fmt.Printf("  why:             %s\n", reason)
		}
		if sib := strList(m["sibling_guards"]); len(sib) > 0 {
			fmt.Printf("  similar routes:  %s\n", strings.Join(sib, ", "))
		}
		if cl := strList(m["ratified_clauses"]); len(cl) > 0 {
			fmt.Printf("  rule broken:     %s\n", strings.Join(cl, ", "))
		}
		fmt.Println()
	}

	// intent_status distinguishes "we recorded it" from "we could not express it", and the second
	// is not a failure the reader should have to infer from silence.
	switch getStr(data, "intent_status") {
	case "recorded":
		fmt.Println("Your intent was recorded.")
	case "not_formalized":
		fmt.Println("Your intent was not recorded: it could not be expressed as an access rule.")
		if d := getStr(data, "intent_detail"); d != "" {
			fmt.Printf("  %s\n", d)
		}
	}
	if inst := getStr(data, "instruction"); inst != "" {
		fmt.Printf("\nNext: %s\n", inst)
	}
}

func orNone(s string) string {
	if s == "" {
		return "nothing"
	}
	return s
}

func shortSha(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	if s == "" {
		return "N/A"
	}
	return s
}

func strList(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, i := range items {
		if s, ok := i.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
