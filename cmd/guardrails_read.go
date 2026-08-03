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
	Short: "List the repositories you have scanned",
	Long: `List the repositories you have scanned.

One row per repository and branch, with how much of the route surface each scan
covers and whether its rules have been approved. Repositories that were skipped are listed
after the table, so a short list is never mistaken for full coverage.

Exit codes: 0 success, 1 general error, 4 auth failed`,
	Example: `  konvu guardrails list

  # Machine-readable
  konvu guardrails list --output json`,
	RunE: runGuardrailsList,
}

var guardrailsShowCmd = &cobra.Command{
	Use:   "show <repo>",
	Short: "Show a repository's access rules",
	Long: `Show a repository's access rules.

Reports how many routes the scan analyzed, how many restrict who may reach them,
and the access rules pull requests are checked against. Pass --rules-only to print
just the rules table.

The repo is the id the scan was recorded under, usually owner/name.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu guardrails show acme/web

  # A specific branch, rules table only
  konvu guardrails show acme/web --branch release-2.3 --rules-only`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGuardrailsShow,
}

func init() {
	lf := guardrailsListCmd.Flags()
	lf.StringP("output", "o", "", "output format: table, json, or csv")
	lf.Bool("quiet", false, "print only repo names")

	sf := guardrailsShowCmd.Flags()
	sf.String("branch", "", "branch to act on (default: the repository's default branch, resolved by the server)")
	sf.Bool("rules-only", false, "print just the rules table, no summary")
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
	// Repositories a baseline was attempted for and not recorded. They hold no baseline, so no row
	// above mentions them, and dropping them left a repository that failed looking exactly like one
	// nobody ever asked about — the same silence the skipped list exists to break.
	onboarding := getSlice(data, "onboarding")

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
		// Unfiltered, unlike the table below: a program reading this wants what the server said,
		// not this command's view of which rows are worth a line.
		fmt.Println(output.FormatJSON(map[string]any{
			"baselines":  baselines,
			"skipped":    skipped,
			"onboarding": onboarding,
		}))
		return nil
	}

	rows := make([]any, 0, len(baselines))
	for _, b := range baselines {
		m, ok := b.(map[string]any)
		if !ok {
			continue
		}
		ratified, _ := getBool(m, "ratified")
		enabled, _ := getBool(m, "enabled")
		rows = append(rows, map[string]any{
			"repository": getStr(m, "repo"),
			"branch":     getStr(m, "branch"),
			"routes":     countDisplay(m["n_paths"]),
			"restricted": countDisplay(m["n_guarded"]),
			"approved":   yesNo(ratified),
			// Approved and checked are different questions: rules can be approved on a
			// repository whose pull requests nothing is looking at yet.
			"checking": yesNo(enabled),
			"scanned":  dateDisplay(getStr(m, "created_at")),
		})
	}
	columns := []string{
		"repository", "branch", "routes", "restricted", "approved", "checking", "scanned",
	}

	if format == output.CSV {
		fmt.Print(output.FormatCSV(map[string]any{"baselines": rows}, columns, "baselines"))
		return nil
	}
	if len(rows) == 0 {
		fmt.Println("No repositories scanned yet. Run 'konvu guardrails scan' in a repository.")
	} else {
		fmt.Println(output.FormatTable(map[string]any{"baselines": rows}, columns, "baselines", nil))
	}
	// Silence about these would read as full coverage. One line each with its own reason: the
	// reasons call for opposite responses, and one deleted on purpose must not read as
	// "nothing found here".
	reasons := getMap(data, "skipped_reasons")
	// Still switched on despite having no scan: it keeps opening a check on every pull request,
	// one that can only report it has nothing to judge against. Calling that "not watching" is the
	// opposite of what its authors see happen.
	stillOn := map[string]bool{}
	for _, name := range strList(getSlice(data, "skipped_enabled")) {
		stillOn[name] = true
	}
	for _, name := range strList(skipped) {
		code, _ := reasons[name].(string)
		if stillOn[name] {
			fmt.Printf("%s has no scan (%s) but is still switched on, so its pull requests\n",
				name, notWatchedReason(code))
			fmt.Printf("  get a check that cannot judge them. Scan it again, or: konvu guardrails enable --off %s\n", name)
			continue
		}
		fmt.Printf("Not watching %s: %s\n", name, notWatchedReason(code))
	}
	if rows := withoutABaseline(onboarding, baselines); len(rows) > 0 {
		fmt.Println("No scan recorded:")
		for _, m := range rows {
			line := fmt.Sprintf("  %s — %s", getStr(m, "repo"), onboardingState(m))
			if reason := getStr(m, "error"); reason != "" {
				line += ": " + reason
			}
			fmt.Println(line)
		}
	}
	return nil
}

// withoutABaseline keeps the onboarding rows for repositories the table does not already carry.
// A repository with a baseline is a row up there, and naming it again below invites reading the
// second mention as a problem with the first.
func withoutABaseline(onboarding, baselines []any) []map[string]any {
	recorded := make(map[string]bool, len(baselines))
	for _, b := range baselines {
		if m, ok := b.(map[string]any); ok {
			recorded[getStr(m, "repo")] = true
		}
	}
	rows := make([]map[string]any, 0, len(onboarding))
	for _, o := range onboarding {
		m, ok := o.(map[string]any)
		if !ok || recorded[getStr(m, "repo")] {
			continue
		}
		rows = append(rows, m)
	}
	return rows
}

// onboardingState is how far a repository got, in the server's own words rather than a vocabulary
// this command would have to keep in step with. The one thing it does interpret is the flag for
// "waiting on you", which is the difference between a row to wait out and a row to act on.
func onboardingState(m map[string]any) string {
	state := getStr(m, "outcome")
	if state == "" {
		state = getStr(m, "status")
	}
	if state == "" {
		state = "unknown"
	}
	if actionRequired, _ := getBool(m, "action_required"); actionRequired {
		state += " (needs attention)"
	}
	return state
}

// notWatchedReason turns a reason code into something a reader can act on. An unrecognised code
// travels through: an unfamiliar explanation beats a blank one.
func notWatchedReason(code string) string {
	switch {
	case code == "deleted-by-owner":
		return "you deleted it; scan the repository again when you want it back"
	case strings.HasPrefix(code, "abstain:"):
		// Not the same as having nothing to check; reporting it as that would hide a gap of ours.
		return "Konvu could not read it - tell us, this one is on our side"
	case code == "no-authz-surface" || code == "":
		return "no access checks found in the code, so there is nothing to judge"
	}
	return code
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
	rulesOnly, _ := cmd.Flags().GetBool("rules-only")
	outputFlag, _ := cmd.Flags().GetString("output")
	format := output.DetectOutputFormat(outputFlag)

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: specify a repository, e.g. 'konvu guardrails show owner/name'")
		os.Exit(clierrors.ExitUsageError)
	}
	repo := args[0]
	branch := requestedBranch(cmd)

	client := api.NewClient("", "")
	defer client.Close()

	// The route matches on a path, so the repo's slash is part of it and must not be escaped.
	data, err := client.Get(
		guardrailsAPI+"/dashboard/repos/"+repo+"/baseline",
		branchParam(branch),
	)
	if err != nil {
		return err
	}

	if format == output.JSON {
		if rulesOnly {
			fmt.Println(output.FormatJSON(map[string]any{"policy": getSlice(data, "policy")}))
			return nil
		}
		fmt.Println(output.FormatJSON(data))
		return nil
	}

	policy := getSlice(data, "policy")
	rows := make([]any, 0, len(policy))
	var drafted []string
	for _, p := range policy {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		approved, _ := getBool(m, "ratified")
		rows = append(rows, map[string]any{
			"who":      getStr(m, "role"),
			"may":      getStr(m, "action"),
			"on":       getStr(m, "resource"),
			"where":    orAnyRoute(getStr(m, "route")),
			"when":     conditionDisplay(getStr(m, "condition")),
			"approved": yesNo(approved),
		})
		if key := getStr(m, "key"); !approved && key != "" {
			drafted = append(drafted, key)
		}
	}
	// `approved` per row, now that rules can be approved one at a time.
	columns := []string{"who", "may", "on", "where", "when", "approved"}

	if format == output.CSV {
		fmt.Print(output.FormatCSV(map[string]any{"policy": rows}, columns, "policy"))
		return nil
	}

	if !rulesOnly {
		ratified, _ := getBool(data, "ratified")
		approval := "not approved yet"
		if ratified {
			approval = "approved"
		}
		fmt.Printf("%s @ %s — %s\n", getStr(data, "repo"), getStr(data, "branch"), approval)
		fmt.Printf("  scan id: %s\n", getStr(data, "fingerprint"))
		fmt.Printf("  routes:  %s analyzed, %s restricted, %s open to anyone\n",
			countDisplay(data["n_paths"]),
			countDisplay(data["n_guarded"]),
			countDisplay(data["n_unguarded"]))
		fmt.Println()
	}
	if len(rows) == 0 {
		fmt.Println("No access rules drafted for this repository yet.")
		return nil
	}
	fmt.Printf("ACCESS RULES (%d)\n", len(rows))
	fmt.Println(output.FormatTable(map[string]any{"policy": rows}, columns, "policy", nil))
	printDraftedRules(repo, drafted)
	return nil
}

// printDraftedRules lists the keys still awaiting approval, one per line and untruncated. A block
// rather than a table column, like `review` prints its rows: a table would truncate the one value
// the reader has to copy back.
func printDraftedRules(repo string, keys []string) {
	if len(keys) == 0 {
		return
	}
	fmt.Println()
	fmt.Printf("%d rule(s) not approved yet, so pull request checks stay paused.\n", len(keys))
	fmt.Println("Approve one by passing its key:")
	fmt.Println()
	for _, k := range keys {
		fmt.Printf("  %s\n", k)
	}
	fmt.Println()
	fmt.Printf("  konvu guardrails approve %s --rule %q\n", repo, keys[0])
}

// orAnyRoute renders a rule's route scope. Empty means every route reaching that resource, which is
// wider rather than narrower, so it must not read as missing data.
func orAnyRoute(route string) string {
	if route == "" {
		return "(any)"
	}
	return route
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

// conditionDisplay translates the always-true condition only; the rest is passed through.
func conditionDisplay(s string) string {
	if s == "true" {
		return "always"
	}
	return s
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
