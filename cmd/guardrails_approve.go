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
	Use:   "ratify <repo>",
	Short: "Agree to a repository's proposed authorization policy",
	Long: `Agree to a repository's proposed authorization policy.

A recorded baseline starts as a draft: checks report that a change was not evaluated
rather than passing or failing it. Ratifying is you agreeing the proposed policy is
the authorization you intend, which is what lets a later change be reported as a
breach of it.

Read it first with 'konvu guardrails show <repo>'.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu guardrails ratify acme/web

  # A specific branch
  konvu guardrails ratify acme/web --branch release-2.3`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGuardrailsApprove,
}

var guardrailsExplainCmd = &cobra.Command{
	Use:   "explain <token>",
	Short: "Explain what a check flagged on a pull request, and optionally state your intent",
	Long: `Explain what a check flagged on a pull request, and optionally state your intent.

The token comes from the check's own comment on the pull request. For each flagged
route this prints the guard the code enforces today, what comparable routes enforce,
and the policy already covering the resource.

Pass --intent to record the guard you actually want. Nothing is applied to the
policy here: the check re-runs on your next push and clears itself if the code
matches.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu guardrails explain kb-36-9f3a1c

  # Say what the route is supposed to enforce
  konvu guardrails explain kb-36-9f3a1c --intent "only the owner may read a document"`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGuardrailsExplain,
}

func init() {
	rf := guardrailsApproveCmd.Flags()
	rf.String("branch", "", "branch to act on (default: the repository's default branch, resolved by the server)")
	rf.StringP("output", "o", "", "output format: table, json, or csv")

	ef := guardrailsExplainCmd.Flags()
	ef.String("intent", "", "the guard this route is supposed to enforce, in your own words")
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
		fmt.Fprintln(os.Stderr, "Error: specify a repository, e.g. 'konvu guardrails ratify owner/name'")
		os.Exit(clierrors.ExitUsageError)
	}
	repo := args[0]
	branch := requestedBranch(cmd)

	client := api.NewClient("", "")
	defer client.Close()

	// The route matches on a path, so the repo's slash is part of it and must not be escaped.
	data, err := client.Post(guardrailsAPI+"/dashboard/repos/"+repo+"/ratify",
		branchParam(branch))
	if err != nil {
		return err
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(data))
		return nil
	}
	fmt.Printf("Ratified %s@%s\n", getStr(data, "repo"), getStr(data, "branch"))
	fmt.Println("  Changes are now reported against this policy.")
	return nil
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
		fmt.Println("Nothing flagged on this pull request.")
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
		fmt.Printf("  enforces now:  %s\n", orNone(getStr(m, "current_guard")))
		if src := getStr(m, "source"); src != "" {
			fmt.Printf("  at:            %s\n", src)
		}
		if reason := getStr(m, "reason"); reason != "" {
			fmt.Printf("  flagged since: %s\n", reason)
		}
		if sib := strList(m["sibling_guards"]); len(sib) > 0 {
			fmt.Printf("  siblings:      %s\n", strings.Join(sib, ", "))
		}
		if cl := strList(m["ratified_clauses"]); len(cl) > 0 {
			fmt.Printf("  policy covers: %s\n", strings.Join(cl, ", "))
		}
		fmt.Println()
	}

	// intent_status distinguishes "we recorded it" from "we could not express it", and the second
	// is not a failure the reader should have to infer from silence.
	switch getStr(data, "intent_status") {
	case "recorded":
		fmt.Println("Your intent was recorded.")
	case "not_formalized":
		fmt.Println("Your intent was not recorded: it could not be expressed as a guard.")
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
