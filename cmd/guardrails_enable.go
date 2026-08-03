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

var guardrailsEnableCmd = &cobra.Command{
	Use:   "enable [repo...]",
	Short: "Start checking pull requests on a repository",
	Long: `Start checking pull requests on a repository.

Connecting an organization does not start checking anything by itself. This chooses
which repositories get checked. With no arguments it enables every repository whose
access rules you have approved; name repositories to enable only those.

A repository needs approved rules first: without them every pull request would be
told only that there is nothing to check against.

This does not block merges. Whether a failing check stops a merge is your
repository's branch protection setting, which Konvu does not change.

Open pull requests are checked on their next push, not retroactively.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 4 auth failed`,
	Example: `  # Every repository whose rules you have approved
  konvu guardrails enable

  # Only these
  konvu guardrails enable acme/web acme/api

  # Stop checking one
  konvu guardrails enable --off acme/web`,
	RunE: runGuardrailsEnable,
}

func init() {
	f := guardrailsEnableCmd.Flags()
	f.Bool("off", false, "stop checking the named repositories")
	f.StringP("output", "o", "", "output format: table or json")
}

func runGuardrailsEnable(cmd *cobra.Command, args []string) error {
	if err := enableFlow(cmd, args); err != nil {
		handleGuardrailsError(err, output.DetectOutputFormat(mustGuardrailsOutput(cmd)))
	}
	return nil
}

func enableFlow(cmd *cobra.Command, args []string) error {
	off, _ := cmd.Flags().GetBool("off")
	format := output.DetectOutputFormat(mustGuardrailsOutput(cmd))

	// "Everything approved" is a fine default for switching on and a footgun for switching off.
	if off && len(args) == 0 {
		fmt.Fprintln(os.Stderr,
			"Error: name the repositories to stop checking, e.g. 'konvu guardrails enable --off acme/web'")
		os.Exit(clierrors.ExitUsageError)
	}

	body := map[string]any{"enabled": !off}
	// Omitted, not empty: an omitted list is what asks for every approved repository.
	if len(args) > 0 {
		repos := make([]any, len(args))
		for i, a := range args {
			repos[i] = a
		}
		body["repos"] = repos
	}

	client := api.NewClient("", "")
	defer client.Close()

	data, err := client.Post(guardrailsAPI+"/dashboard/enablement", body)
	if err != nil {
		return err
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(data))
		return nil
	}
	printEnablement(data, off)
	return nil
}

// printEnablement reports every repository the server answered for. A repository that could not be
// enabled is the one the reader has to act on, so a short list would read as "all done".
func printEnablement(data map[string]any, off bool) {
	var changed, already, blocked []string
	stranded := 0

	for _, r := range getSlice(data, "rows") {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		repo := getStr(m, "repo")
		switch getStr(m, "outcome") {
		case "enabled", "disabled":
			changed = append(changed, repo)
			if n, ok := m["stranded_approvals"].(float64); ok {
				stranded += int(n)
			}
		case "already":
			already = append(already, repo)
		case "no_ratified_baseline":
			blocked = append(blocked, repo)
		}
	}

	if len(changed) == 0 && len(already) == 0 && len(blocked) == 0 {
		fmt.Println("No repositories with approved rules yet.")
		fmt.Println("  Run 'konvu guardrails scan' in a repository, then approve its rules.")
		return
	}

	verb := "Checking"
	if off {
		verb = "No longer checking"
	}
	if len(changed) > 0 {
		fmt.Printf("%s pull requests on:\n", verb)
		for _, r := range changed {
			fmt.Printf("  %s\n", r)
		}
	}
	if len(already) > 0 {
		state := "already enabled"
		if off {
			state = "was not enabled"
		}
		fmt.Printf("Unchanged (%s): %s\n", state, strings.Join(already, ", "))
	}
	if len(blocked) > 0 {
		fmt.Println()
		fmt.Println("Not enabled — no approved rules yet:")
		for _, r := range blocked {
			fmt.Printf("  %s\n", r)
		}
		fmt.Println("  Run 'konvu guardrails scan', then approve the rules it drafts.")
	}

	if off {
		// Decisions already recorded on open pull requests stop being applied when they merge.
		if stranded > 0 {
			fmt.Printf("\n%d open pull request(s) carry decisions that will no longer be applied on merge.\n",
				stranded)
		}
		return
	}
	if len(changed) > 0 {
		fmt.Println()
		fmt.Println("Open pull requests are checked on their next push.")
		fmt.Println("Checks do not block merges; that is your branch protection setting.")
	}
}
