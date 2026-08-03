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

var guardrailsReviewCmd = &cobra.Command{
	Use:   "review <repo>",
	Short: "Review new access a pull request asks for, and allow or deny it",
	Long: `Review new access a pull request asks for, and allow or deny it.

A change that reaches a resource in a way no approved rule covers is held for review
rather than passed or failed. This lists what is waiting, and --allow or --deny
records your decision on it.

Decisions apply to your access rules when the pull request merges, so a decision
recorded here can still be changed with --clear beforehand.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu guardrails review acme/web --pr 412

  # Allow one, deny another
  konvu guardrails review acme/web --pr 412 --allow "USER|read|Document|GET /docs/{id}"
  konvu guardrails review acme/web --pr 412 --deny "ANON|read|Document|GET /docs/{id}"`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGuardrailsReview,
}

func init() {
	f := guardrailsReviewCmd.Flags()
	f.Int("pr", 0, "pull request number — required")
	f.StringSlice("allow", nil, "new access to allow, exactly as printed (repeatable)")
	f.StringSlice("deny", nil, "new access to deny, exactly as printed (repeatable)")
	f.StringSlice("clear", nil, "new access whose decision to remove (repeatable)")
	f.StringP("output", "o", "", "output format: table, json, or csv")
	_ = guardrailsReviewCmd.MarkFlagRequired("pr")
}

func runGuardrailsReview(cmd *cobra.Command, args []string) error {
	if err := reviewFlow(cmd, args); err != nil {
		handleGuardrailsError(err, output.DetectOutputFormat(mustGuardrailsOutput(cmd)))
	}
	return nil
}

func reviewFlow(cmd *cobra.Command, args []string) error {
	pr, _ := cmd.Flags().GetInt("pr")
	allow, _ := cmd.Flags().GetStringSlice("allow")
	deny, _ := cmd.Flags().GetStringSlice("deny")
	clear, _ := cmd.Flags().GetStringSlice("clear")
	format := output.DetectOutputFormat(mustGuardrailsOutput(cmd))

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: specify a repository, e.g. 'konvu guardrails review owner/name --pr 412'")
		os.Exit(clierrors.ExitUsageError)
	}
	repo := args[0]
	if pr <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --pr must be a pull request number")
		os.Exit(clierrors.ExitUsageError)
	}

	client := api.NewClient("", "")
	defer client.Close()

	// The route matches on a path, so the repo's slash is part of it and must not be escaped.
	path := guardrailsAPI + "/dashboard/repos/" + repo + "/review"
	decisions, err := buildDecisions(allow, deny, clear)
	if err != nil {
		return &clierrors.CLIError{
			Code:     "CONFLICTING_DECISIONS",
			Message:  err.Error(),
			ExitCode: clierrors.ExitUsageError,
		}
	}

	var data map[string]any
	if len(decisions) == 0 {
		data, err = client.Get(path, map[string]any{"pr": pr})
	} else {
		// pr is a query parameter on the write too, so it goes in the path rather than the body.
		data, err = client.Post(
			fmt.Sprintf("%s?pr=%d", path, pr),
			map[string]any{"decisions": decisions},
		)
	}
	if err != nil {
		return err
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(data))
		return nil
	}
	printReview(data, len(decisions) > 0)
	return nil
}

// buildDecisions turns the flags into the wire shape, and refuses a capability named under two
// verbs. The server applies decisions in order, so allowing and denying the same capability in
// one command would let argument order settle an authorization question silently. That is worth
// an error rather than a winner.
func buildDecisions(allow, deny, clear []string) ([]map[string]any, error) {
	out := []map[string]any{}
	verb := map[string]string{}
	for _, spec := range [](struct {
		keys     []string
		decision string
	}){{allow, "allow"}, {deny, "deny"}, {clear, "clear"}} {
		for _, k := range spec.keys {
			if k = strings.TrimSpace(k); k == "" {
				continue
			}
			if prev, seen := verb[k]; seen && prev != spec.decision {
				return nil, fmt.Errorf(
					"%q was passed to both --%s and --%s; pass it once", k, prev, spec.decision)
			}
			if _, seen := verb[k]; seen {
				continue // same verb twice is harmless; do not send it twice
			}
			verb[k] = spec.decision
			out = append(out, map[string]any{"capability_key": k, "decision": spec.decision})
		}
	}
	return out, nil
}

func printReview(data map[string]any, wrote bool) {
	pr, _ := data["pr"].(float64)
	fmt.Printf("%s#%d  %s\n", getStr(data, "repo"), int(pr), getStr(data, "pr_title"))
	if a := getStr(data, "pr_author"); a != "" {
		fmt.Printf("  by %s on %s\n", a, getStr(data, "branch"))
	}
	if u := getStr(data, "gh_url"); u != "" {
		fmt.Printf("  %s\n", u)
	}
	fmt.Println()

	pending, _ := getBool(data, "pending")
	if !pending {
		fmt.Println("No new access to review on this pull request.")
		return
	}

	open := getSlice(data, "open_rows")
	scoped := getSlice(data, "scoped_rows")

	if len(open) > 0 {
		fmt.Printf("NEW ACCESS — WAITING FOR YOU (%d)\n\n", len(open))
		printCapabilities(open)
	}
	if len(scoped) > 0 {
		fmt.Printf("NEW ACCESS — ALREADY COVERED BY A RULE (%d)\n\n", len(scoped))
		printCapabilities(scoped)
	}

	if canDecide, _ := getBool(data, "can_decide"); !canDecide {
		fmt.Println("You can view this but not decide it; that needs an owner.")
		return
	}
	if wrote {
		fmt.Println("Recorded. Your decisions apply to your access rules when the pull request merges.")
		return
	}
	if len(open) > 0 {
		fmt.Println("Allow or deny with --allow, --deny or --clear, pasting a line exactly as printed.")
	}
}

// printCapabilities prints one block per capability rather than a table row. The capability is
// the string the reader has to pass back to --allow, and a table truncates it to fit the width,
// which makes the one value they need uncopyable.
func printCapabilities(rows []any) {
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		fmt.Printf("  %s\n", getStr(m, "key"))
		route := strings.TrimSpace(getStr(m, "method") + " " + getStr(m, "path"))
		fmt.Printf("    %s\n", route)
		fmt.Printf("    %s may %s %s\n",
			getStr(m, "role"), getStr(m, "action"), getStr(m, "resource"))
		fmt.Printf("    code checks:     %s\n", orNone(getStr(m, "guard")))
		if src := getStr(m, "source"); src != "" {
			fmt.Printf("    at:              %s\n", src)
		}
		if hint := getStr(m, "sibling_hint"); hint != "" {
			fmt.Printf("    %s\n", hint)
		}
		if d := getStr(m, "decision"); d != "" {
			fmt.Printf("    decided: %s\n", d)
		}
		fmt.Println()
	}
}
