package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var guardrailsDeleteCmd = &cobra.Command{
	Use:   "delete <repo>",
	Short: "Delete a repository's scan and the rules approved on it",
	Long: `Delete a repository's scan and the rules approved on it.

Removes what Konvu read about a repository's access, along with the rules you approved
on it. Start over with 'konvu guardrails scan'.

One branch at a time by default; --all-branches removes every branch's.

The deletion sticks: Konvu scans this repository again only when you ask it to.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu guardrails delete acme/web

  # A specific branch
  konvu guardrails delete acme/web --branch release-2.3

  # Every branch
  konvu guardrails delete acme/web --all-branches`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGuardrailsDelete,
}

func init() {
	f := guardrailsDeleteCmd.Flags()
	f.String("branch", "", "branch to act on (default: the repository's default branch, resolved by the server)")
	f.Bool("all-branches", false, "delete every branch's, not just one")
	f.Bool("yes", false, "skip the confirmation prompt")
	f.StringP("output", "o", "", "output format: table or json")
}

func runGuardrailsDelete(cmd *cobra.Command, args []string) error {
	if err := deleteFlow(cmd, args); err != nil {
		handleGuardrailsError(err, output.DetectOutputFormat(mustGuardrailsOutput(cmd)))
	}
	return nil
}

func deleteFlow(cmd *cobra.Command, args []string) error {
	allBranches, _ := cmd.Flags().GetBool("all-branches")
	assumeYes, _ := cmd.Flags().GetBool("yes")
	format := output.DetectOutputFormat(mustGuardrailsOutput(cmd))

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: specify a repository, e.g. 'konvu guardrails delete owner/name'")
		os.Exit(clierrors.ExitUsageError)
	}
	repo := args[0]
	branch := requestedBranch(cmd)

	if allBranches && branch != "" {
		return &clierrors.CLIError{
			Code:       "CONFLICTING_SCOPE",
			Message:    "--branch and --all-branches ask for different things",
			Suggestion: "Pass one or the other.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}

	params := map[string]any{}
	if allBranches {
		params["all_branches"] = true
	} else if branch != "" {
		params["branch"] = branch
	}

	if !assumeYes {
		// Keyed on whether we can actually ask, not on the output format: DetectOutputFormat answers
		// JSON for any pipe, so keying on that dropped the confirmation for
		// `delete <repo> | tee log` -- piping is not consent. No terminal to ask at means refuse
		// rather than proceed, which is the only safe direction for a deletion.
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return &clierrors.CLIError{
				Code:       "CONFIRMATION_REQUIRED",
				Message:    "this deletes the scan and the rules approved on it, and cannot be asked about here",
				Suggestion: "Re-run with --yes if that is what you want.",
				ExitCode:   clierrors.ExitUsageError,
			}
		}
		if !confirmDelete(repo, branch, allBranches) {
			fmt.Println("Left it alone.")
			return nil
		}
	}

	client := api.NewClient("", "")
	defer client.Close()

	// The route matches on a path, so the repo's slash is part of it and must not be escaped.
	data, err := client.Delete(guardrailsAPI+"/dashboard/repos/"+repo+"/baseline", params)
	if err != nil {
		return err
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(data))
		return nil
	}

	fmt.Printf("Deleted the scan for %s (%s).\n",
		getStr(data, "repo"), strings.Join(strList(data["deleted"]), ", "))
	fmt.Printf("  Scan it again with 'konvu guardrails scan %s'.\n", repo)
	// Not switched off here: that would also discard decisions recorded on open pull requests.
	if stillOn, _ := getBool(data, "still_enabled"); stillOn {
		fmt.Println()
		fmt.Printf("%s is still switched on, so its pull request checks will report that there\n", repo)
		fmt.Println("is nothing to judge against. Scan it again, or stop checking it with")
		fmt.Printf("  konvu guardrails enable --off %s\n", repo)
	}
	return nil
}

// confirmDelete returns false on anything but "y", including a read error: a prompt guarding a
// deletion has to fail closed.
func confirmDelete(repo, branch string, allBranches bool) bool {
	scope := fmt.Sprintf("the %s branch", branch)
	if allBranches {
		scope = "every branch"
	} else if branch == "" {
		scope = "its default branch"
	}
	// stderr: the prompt is interactive UI, and on stdout it sits in front of the JSON document
	// and breaks any reader parsing it.
	fmt.Fprintf(os.Stderr, "Delete the scan and approved rules for %s (%s)?\n", repo, scope)
	fmt.Fprint(os.Stderr, "Type 'y' to continue: ")
	var answer string
	_, _ = fmt.Scanln(&answer)
	return strings.EqualFold(strings.TrimSpace(answer), "y")
}
