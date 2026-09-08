package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

// The threat-profile feature is repo-scoped, so it lives under the `inventory`
// noun (matching the dashboard's Inventory surface). These are the backend paths.
const threatProfileSummaryPath = "/threat_profile/summary"
const threatProfileRepoPath = "/threat_profile/repositories/"

// Headline boolean attributes surfaced by the backend rollup, in display order.
var inventoryHeadlineKeys = []struct{ key, label string }{
	{"internet_exposed", "Internet exposed"},
	{"customer_data", "Customer data"},
	{"cloud_credentials", "Cloud credentials"},
}

var inventoryCmd = &cobra.Command{
	Use:     "inventory",
	Aliases: []string{"inv"},
	Short:   "Understand repositories and their security context",
	Long: `Explore repository security context from local Security Context Graphs and
hosted repository Threat Profiles. Local mapping and exploration work without a
Konvu account.

In an interactive terminal, run without a subcommand to open the combined
repository browser. When piped, or when -o or -q is set, it prints the same
repository listing as 'inventory list'.

Use 'inventory map <local-path>' to create a Security Context Graph locally.`,
	Example: `  konvu inventory
  konvu inventory list -o json
  konvu inventory map .
  konvu inventory show .`,
	Args: cobra.NoArgs,
	RunE: runInventory,
}

func runInventory(cmd *cobra.Command, _ []string) error {
	quiet, _ := cmd.Flags().GetBool("quiet")
	if shouldOpenInventoryTUI(output.BaselineTerminalInteractive(), cmd.Flags().Changed("output"), quiet) {
		return runInventoryTUI(cmd)
	}
	format := output.DetectOutputFormat(mustOutputFlag(cmd))
	deps, err := defaultInventoryListDependencies()
	if err != nil {
		return inventoryLocalError(err)
	}
	return runInventoryList(cmd, deps, format, quiet)
}

func shouldOpenInventoryTUI(interactive, outputChanged, quiet bool) bool {
	return interactive && !outputChanged && !quiet
}

var inventoryShowCmd = &cobra.Command{
	Use:   "show <target>",
	Short: "Show the security context available for one repository",
	Long: `Show a local Security Context Graph by filesystem path, without requiring a
Konvu account. For a hosted repository URL or ID, show its Konvu Threat Profile:
score, tier, classification, summary, attributes, provenance, and evidence.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu inventory show .
  konvu inventory show github:org/repo
  konvu inventory show org/repo -o json
  konvu inventory show org/repo --fields threat_profile_score,threat_profile_tier -o json`,
	RunE: runInventoryShow,
}

func runInventoryOverview(cmd *cobra.Command, args []string) error {
	format := output.DetectOutputFormat(mustOutputFlag(cmd))
	quiet, _ := cmd.Flags().GetBool("quiet")

	client := api.NewClient("", "")
	defer client.Close()

	data, err := client.Get(threatProfileSummaryPath, nil)
	if err != nil {
		handleInventoryError(err, format)
	}

	if quiet {
		fmt.Println(inventoryQuietLines(getSlice(data, "top_repos")))
		return nil
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(data))
		return nil
	}

	renderInventoryOverview(data)
	return nil
}

// inventoryQuietLines renders the ranked repos as `vcs_repository_id<TAB>tier`,
// one per line, for piping into `xargs konvu inventory show`. The tier is the
// stable slug (or "unscored"), never the human label.
func inventoryQuietLines(top []any) string {
	lines := make([]string, 0, len(top))
	for _, r := range top {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		tier := getStr(m, "threat_profile_tier")
		if tier == "" {
			tier = "unscored"
		}
		lines = append(lines, getStr(m, "vcs_repository_id")+"\t"+tier)
	}
	return strings.Join(lines, "\n")
}

func renderInventoryOverview(data map[string]any) {
	total := intOf(data["total_profiled"])
	fmt.Printf("\nRepositories profiled: %d\n", total)
	if total == 0 {
		fmt.Println("\nNo threat profiles yet. Konvu builds them as it analyzes your repositories.")
		return
	}

	counts := getMap(data, "counts")
	fmt.Println("\nHeadline attributes:")
	for _, h := range inventoryHeadlineKeys {
		fmt.Printf("  %-18s %d\n", h.label+":", intOf(counts[h.key]))
	}

	tiers := getMap(data, "tier_distribution")
	if len(tiers) > 0 {
		fmt.Println("\nTier distribution:")
		for _, slug := range orderedTierSlugs(tiers) {
			fmt.Printf("  %-14s %d\n", tierLabel(slug)+":", intOf(tiers[slug]))
		}
	}

	top := getSlice(data, "top_repos")
	if len(top) > 0 {
		fmt.Println("\nTop repositories:")
		rows := make([]any, 0, len(top))
		for _, r := range top {
			m, ok := r.(map[string]any)
			if !ok {
				continue
			}
			rows = append(rows, map[string]any{
				"repository":  getStr(m, "repo_name"),
				"score":       scoreDisplay(m["threat_profile_score"]),
				"tier":        repoTierText(m),
				"exposed":     boolDisplay(m["internet_exposed"]),
				"cust_data":   boolDisplay(m["customer_data"]),
				"cloud_creds": boolDisplay(m["cloud_credentials"]),
			})
		}
		columns := []string{"repository", "score", "tier", "exposed", "cust_data", "cloud_creds"}
		fmt.Println(output.FormatTable(map[string]any{"top_repos": rows}, columns, "top_repos", nil))
	}
}

func runInventoryShow(cmd *cobra.Command, args []string) error {
	format := output.DetectOutputFormat(mustOutputFlag(cmd))
	fields, _ := cmd.Flags().GetString("fields")

	if len(args) != 1 {
		handleInventoryError(usageError("Specify exactly one repository (URL or id)."), format)
	}

	// --fields selects raw JSON keys; it has no meaning for the fixed-layout
	// table view (and silently dropping fields there would render blank headings
	// and misleading "unscored" defaults), so require JSON explicitly. Checked
	// before any network call so a bad invocation fails fast.
	if fields != "" && format != output.JSON {
		handleInventoryError(usageError("--fields only applies to JSON output; re-run with -o json."), format)
	}
	if inventoryLooksLocalTarget(args[0]) {
		return runInventoryShowLocal(cmd, args[0], fields, format)
	}

	client := api.NewClient("", "")
	defer client.Close()

	repos, _ := fetchCoverage(client, format)
	ids := resolveRepoIDsOrExit(repos, args, format)

	data, err := client.Get(threatProfileRepoPath+ids[0], nil)
	if err != nil {
		handleInventoryError(err, format)
	}

	if format == output.JSON {
		if fields != "" {
			data = output.FilterFields(data, splitFields(fields))
		}
		fmt.Println(output.FormatJSON(data))
		return nil
	}

	renderInventoryShow(data)
	return nil
}

// splitFields parses a comma-separated --fields value, trimming blanks.
func splitFields(fields string) []string {
	var out []string
	for _, f := range strings.Split(fields, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func renderInventoryShow(data map[string]any) {
	name := orDefault(getStr(data, "repo_name"), getStr(data, "repo_url"))
	fmt.Printf("\nThreat Profile: %s\n", name)

	score := scoreDisplay(data["threat_profile_score"])
	tier := repoTierText(data)
	fmt.Printf("\nScore: %s / 100  (%s)\n", score, tier)

	if cat := getStr(data, "classification_category"); cat != "" {
		line := fmt.Sprintf("Classification: %s", cat)
		if conf, ok := getFloat(data, "classification_confidence"); ok {
			line += fmt.Sprintf(" (confidence %s)", confidenceDisplay(conf))
		}
		fmt.Println(line)
	}
	if prod, ok := getBool(data, "is_production"); ok {
		fmt.Printf("Production: %s\n", yesNo(prod))
	}
	if summary := getStr(data, "threat_profile_summary"); summary != "" {
		fmt.Printf("\n%s\n", summary)
	}

	if surface := getStr(data, "surface"); surface != "" {
		fmt.Printf("\nSurface: %s\n", surface)
	}
	if domains := getSlice(data, "domains"); len(domains) > 0 {
		fmt.Printf("Domains: %s\n", strings.Join(stringsOf(domains), ", "))
	}

	renderInventoryAttributes(getMap(data, "attributes"))
	renderInventoryFactors(getMap(data, "threat_profile_factors"))

	if src := getStr(data, "source"); src != "" {
		fmt.Printf("\nSource: %s", src)
		if updated := getStr(data, "updated_at"); updated != "" {
			fmt.Printf("   Updated: %s", updated)
		}
		fmt.Println()
	}
}

func renderInventoryAttributes(attributes map[string]any) {
	if len(attributes) == 0 {
		return
	}
	fmt.Println("\n--- Attributes ---")
	for _, key := range sortedAnyKeys(attributes) {
		attr, ok := attributes[key].(map[string]any)
		if !ok {
			continue
		}
		line := fmt.Sprintf("  %s: %s", key, valueDisplay(attr["value"]))
		var meta []string
		if prov := getStr(attr, "provenance"); prov != "" {
			meta = append(meta, prov)
		}
		if conf, ok := getFloat(attr, "confidence"); ok {
			meta = append(meta, "confidence "+confidenceDisplay(conf))
		}
		if len(meta) > 0 {
			line += fmt.Sprintf("  (%s)", strings.Join(meta, ", "))
		}
		fmt.Println(line)
		for _, ev := range getSlice(attr, "evidence") {
			if s, ok := ev.(string); ok && s != "" {
				fmt.Printf("    - %s\n", s)
			}
		}
	}
}

func renderInventoryFactors(factors map[string]any) {
	if len(factors) == 0 {
		return
	}
	fmt.Println("\n--- Score factors ---")
	for _, key := range sortedAnyKeys(factors) {
		fmt.Printf("  %s: %s\n", key, valueDisplay(factors[key]))
	}
}

// repoTierText prefers the server-provided human label, falling back to a
// client-side titleization of the tier slug, then to "unscored".
func repoTierText(m map[string]any) string {
	if label := getStr(m, "threat_profile_tier_label"); label != "" {
		return label
	}
	if slug := getStr(m, "threat_profile_tier"); slug != "" {
		return tierLabel(slug)
	}
	return "unscored"
}

// canonicalTierOrder lists tiers high-to-low so the overview reads top-down;
// unknown slugs are appended alphabetically after these.
var canonicalTierOrder = []string{"crown_jewel", "key_asset", "standard", "peripheral"}

func orderedTierSlugs(tiers map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	for _, slug := range canonicalTierOrder {
		if _, ok := tiers[slug]; ok {
			out = append(out, slug)
			seen[slug] = true
		}
	}
	var rest []string
	for slug := range tiers {
		if !seen[slug] {
			rest = append(rest, slug)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// tierLabel titleizes a tier slug ("crown_jewel" -> "Crown jewel").
func tierLabel(slug string) string {
	if slug == "" {
		return ""
	}
	s := strings.ReplaceAll(slug, "_", " ")
	return strings.ToUpper(s[:1]) + s[1:]
}

func scoreDisplay(v any) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%d", intOf(v))
}

func confidenceDisplay(c float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", c), "0"), ".")
}

func boolDisplay(v any) string {
	b, ok := v.(bool)
	if !ok {
		return "—"
	}
	return yesNo(b)
}

// valueDisplay renders an attribute/factor value compactly for text output.
func valueDisplay(v any) string {
	switch t := v.(type) {
	case nil:
		return "—"
	case bool:
		return yesNo(t)
	case string:
		return t
	case []any:
		return strings.Join(stringsOf(t), ", ")
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func stringsOf(items []any) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		} else {
			out = append(out, fmt.Sprintf("%v", it))
		}
	}
	return out
}

func sortedAnyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// intOf coerces a JSON-decoded number (float64) or int to int, tolerating nil.
func intOf(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	}
	return 0
}

func handleInventoryError(err error, format output.OutputFormat) {
	var cliErr *clierrors.CLIError
	switch e := err.(type) {
	case *clierrors.CLIError:
		cliErr = e
	case *api.AuthenticationError:
		cliErr = clierrors.NewAuthError(e.Error())
	case *api.APIError:
		if inventoryIsNoThreatProfileError(e) {
			cliErr = inventoryNoThreatProfileError()
			break
		}
		switch e.StatusCode {
		case 404:
			cliErr = inventoryNoThreatProfileError()
		default:
			cliErr = clierrors.NewAPIError(e.Error())
		}
	default:
		cliErr = clierrors.NewAPIError(err.Error())
	}

	if format == output.JSON {
		fmt.Println(clierrors.FormatErrorJSON(cliErr))
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", cliErr.Message)
		if cliErr.Suggestion != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", cliErr.Suggestion)
		}
	}
	os.Exit(cliErr.ExitCode)
}

func init() {
	inventoryShowCmd.Flags().StringP("output", "o", "", "Output format: json, table")
	inventoryShowCmd.Flags().String("fields", "", "Comma-separated top-level fields to include in JSON output")

	inventoryCmd.AddCommand(inventoryShowCmd, newInventoryListCmd(), newInventoryMapCmd())

	inventoryCmd.Flags().StringP("output", "o", "", "Output format for the repository listing: json, table")
	inventoryCmd.Flags().BoolP("quiet", "q", false, "Print only repository selectors")

	rootCmd.AddCommand(inventoryCmd)
}
