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

var guardrailsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the authorization baselines recorded for your repositories",
	Long: `List the authorization baselines recorded for your repositories.

One row per repository and branch, with how much of the route surface each baseline
models and whether it has been ratified. Repositories that were skipped are listed
after the table, so a short list is never mistaken for full coverage.

Exit codes: 0 success, 1 general error, 4 auth failed`,
	Example: `  konvu guardrails list

  # Machine-readable
  konvu guardrails list --output json`,
	RunE: runGuardrailsList,
}

var guardrailsShowCmd = &cobra.Command{
	Use:   "show <repo>",
	Short: "Show a baseline and the authorization policy it is checked against",
	Long: `Show a baseline and the authorization policy it is checked against.

Reports how many routes the baseline models, how many enforce a guard, and the
policy rows drift is measured against. Pass --policy-only to print just the policy table.

The repo is the id the baseline was recorded under, usually owner/name.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu guardrails show acme/web

  # A specific branch, policy table only
  konvu guardrails show acme/web --branch release-2.3 --policy-only`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGuardrailsShow,
}

func init() {
	lf := guardrailsListCmd.Flags()
	lf.StringP("output", "o", "", "output format: table, json, or csv")
	lf.Bool("quiet", false, "print only repo names")

	sf := guardrailsShowCmd.Flags()
	sf.String("branch", "main", "branch the baseline was recorded for")
	sf.Bool("policy-only", false, "print just the policy table, no baseline summary")
	sf.StringP("output", "o", "", "output format: table, json, or csv")
}

func runGuardrailsList(cmd *cobra.Command, args []string) error {
	if err := listFlow(cmd, args); err != nil {
		handleGuardrailsError(err, output.DetectOutputFormat(mustGuardrailsOutput(cmd)))
	}
	return nil
}

func listFlow(cmd *cobra.Command, args []string) error {
	outputFlag, _ := cmd.Flags().GetString("output")
	quiet, _ := cmd.Flags().GetBool("quiet")
	format := output.DetectOutputFormat(outputFlag)

	client := api.NewClient("", "")
	defer client.Close()

	data, err := client.Get(guardrailsAPI+"/dashboard/baselines", nil)
	if err != nil {
		return err
	}
	baselines := getSlice(data, "baselines")
	skipped := getSlice(data, "skipped")

	if quiet {
		// One repository can hold a baseline per branch, so print each name once — the point
		// of --quiet is piping the list somewhere, and duplicates make that wrong.
		seen := map[string]bool{}
		items := make([]map[string]any, 0, len(baselines))
		for _, b := range baselines {
			m, ok := b.(map[string]any)
			if !ok {
				continue
			}
			if repo := getStr(m, "repo"); repo != "" && !seen[repo] {
				seen[repo] = true
				items = append(items, m)
			}
		}
		fmt.Println(output.FormatQuiet(items, "repo"))
		return nil
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(map[string]any{"baselines": baselines, "skipped": skipped}))
		return nil
	}

	rows := make([]any, 0, len(baselines))
	for _, b := range baselines {
		m, ok := b.(map[string]any)
		if !ok {
			continue
		}
		ratified, _ := getBool(m, "ratified")
		rows = append(rows, map[string]any{
			"repository": getStr(m, "repo"),
			"branch":     getStr(m, "branch"),
			"routes":     countDisplay(m["n_paths"]),
			"guarded":    countDisplay(m["n_guarded"]),
			"ratified":   yesNo(ratified),
			"recorded":   dateDisplay(getStr(m, "created_at")),
		})
	}
	columns := []string{"repository", "branch", "routes", "guarded", "ratified", "recorded"}

	if format == output.CSV {
		fmt.Print(output.FormatCSV(map[string]any{"baselines": rows}, columns, "baselines"))
		return nil
	}
	if len(rows) == 0 {
		fmt.Println("No baselines recorded yet. Run 'konvu guardrails baseline' in a repository.")
	} else {
		fmt.Println(output.FormatTable(map[string]any{"baselines": rows}, columns, "baselines", nil))
	}
	// Silence about skipped repositories would read as full coverage.
	if len(skipped) > 0 {
		names := make([]string, 0, len(skipped))
		for _, s := range skipped {
			if str, ok := s.(string); ok {
				names = append(names, str)
			}
		}
		fmt.Printf("Skipped: %s\n", strings.Join(names, ", "))
	}
	return nil
}

func runGuardrailsShow(cmd *cobra.Command, args []string) error {
	if err := showFlow(cmd, args); err != nil {
		handleGuardrailsError(err, output.DetectOutputFormat(mustGuardrailsOutput(cmd)))
	}
	return nil
}

func mustGuardrailsOutput(cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetString("output")
	return v
}

func showFlow(cmd *cobra.Command, args []string) error {
	branch, _ := cmd.Flags().GetString("branch")
	policyOnly, _ := cmd.Flags().GetBool("policy-only")
	outputFlag, _ := cmd.Flags().GetString("output")
	format := output.DetectOutputFormat(outputFlag)

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: specify a repository, e.g. 'konvu guardrails show owner/name'")
		os.Exit(clierrors.ExitUsageError)
	}
	repo := args[0]

	client := api.NewClient("", "")
	defer client.Close()

	// The route matches on a path, so the repo's slash is part of it and must not be escaped.
	data, err := client.Get(
		guardrailsAPI+"/dashboard/repos/"+repo+"/baseline",
		map[string]any{"branch": branch},
	)
	if err != nil {
		return err
	}

	if format == output.JSON {
		if policyOnly {
			fmt.Println(output.FormatJSON(map[string]any{"policy": getSlice(data, "policy")}))
			return nil
		}
		fmt.Println(output.FormatJSON(data))
		return nil
	}

	policy := getSlice(data, "policy")
	rows := make([]any, 0, len(policy))
	for _, p := range policy {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, map[string]any{
			"role":      getStr(m, "role"),
			"action":    getStr(m, "action"),
			"resource":  getStr(m, "resource"),
			"condition": getStr(m, "condition"),
		})
	}
	columns := []string{"role", "action", "resource", "condition"}

	if format == output.CSV {
		fmt.Print(output.FormatCSV(map[string]any{"policy": rows}, columns, "policy"))
		return nil
	}

	if !policyOnly {
		ratified, _ := getBool(data, "ratified")
		fmt.Printf("%s @ %s\n", getStr(data, "repo"), getStr(data, "branch"))
		fmt.Printf("  fingerprint: %s\n", getStr(data, "fingerprint"))
		fmt.Printf("  ratified:    %s\n", yesNo(ratified))
		fmt.Printf("  routes:      %s modelled, %s guarded, %s unguarded\n",
			countDisplay(data["n_paths"]),
			countDisplay(data["n_guarded"]),
			countDisplay(data["n_unguarded"]))
		fmt.Println()
	}
	if len(rows) == 0 {
		fmt.Println("No policy rows recorded for this baseline.")
		return nil
	}
	fmt.Println(output.FormatTable(map[string]any{"policy": rows}, columns, "policy", nil))
	return nil
}

// countDisplay renders a count the server may omit, so an absent number reads as unknown
// rather than as zero. ASCII on purpose: FormatTable measures byte length, so a multi-byte
// placeholder like an em dash pads short and skews the column.
func countDisplay(v any) string {
	switch n := v.(type) {
	case float64:
		return fmt.Sprintf("%d", int(n))
	case int:
		return fmt.Sprintf("%d", n)
	}
	return "N/A"
}

// dateDisplay keeps the date and drops the time, matching how other commands show one.
func dateDisplay(s string) string {
	if s == "" {
		return "N/A"
	}
	if i := strings.IndexAny(s, "T "); i > 0 {
		return s[:i]
	}
	return s
}
