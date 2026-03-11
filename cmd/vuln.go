package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/KonvuTeam/konvu-cli/internal/api"
	"github.com/KonvuTeam/konvu-cli/internal/mapping"
	"github.com/KonvuTeam/konvu-cli/internal/output"
	"github.com/spf13/cobra"
)

var vulnCmd = &cobra.Command{
	Use:   "vuln",
	Short: "Vulnerability lookup",
}

var vulnGetCmd = &cobra.Command{
	Use:   "get [vuln-id]",
	Short: "Get detailed information about a vulnerability",
	Long: `Get detailed information about a vulnerability.

Examples:
  konvu vuln get CVE-2024-1234
  konvu vuln get GHSA-xxxx --include remediation --output json

Exit codes:
  0  Success
  1  General error
  3  Vulnerability not found
  4  Authentication failed`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		vulnID := args[0]
		includeFlag, _ := cmd.Flags().GetStringSlice("include")
		outputFlag, _ := cmd.Flags().GetString("output")

		if len(includeFlag) == 0 {
			includeFlag = []string{"summary", "affected"}
		}

		format := output.DetectOutputFormat(outputFlag)

		client := api.NewClient("", "")
		defer client.Close()

		fmt.Fprintf(os.Stderr, "Looking up %s...\n", vulnID)

		// Query /sca_issues with cve filter
		issuesData, err := client.Get("/sca_issues", map[string]any{
			"cve":      []string{vulnID},
			"per_page": "100",
		})
		if err != nil {
			if _, ok := err.(*api.AuthenticationError); ok {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				os.Exit(4)
			}
			fmt.Fprintf(os.Stderr, "API Error: %s\n", err)
			os.Exit(1)
		}

		items, _ := issuesData["items"].([]any)
		if len(items) == 0 {
			fmt.Fprintf(os.Stderr, "Vulnerability %s not found or you are not affected.\n", vulnID)
			os.Exit(1)
		}

		// Vulnerability info from first issue
		firstItem, _ := items[0].(map[string]any)
		vulnInfo, _ := firstItem["vulnerability"].(map[string]any)
		if vulnInfo == nil {
			vulnInfo = map[string]any{}
		}

		// Build vulnerability section
		vulnID2, _ := vulnInfo["id"].(string)
		if vulnID2 == "" {
			vulnID2 = vulnID
		}
		aliases, _ := vulnInfo["aliases"].([]any)
		aliasStrs := make([]string, 0, len(aliases))
		for _, a := range aliases {
			if s, ok := a.(string); ok {
				aliasStrs = append(aliasStrs, s)
			}
		}
		severity, _ := vulnInfo["severity"].(string)
		if severity == "" {
			severity = "unknown"
		}
		severity = strings.ToLower(severity)
		summary, _ := vulnInfo["summary"].(string)

		vulnSection := map[string]any{
			"id":       vulnID2,
			"aliases":  aliasStrs,
			"severity": severity,
			"summary":  summary,
			"scoring": map[string]any{
				"cvss": map[string]any{
					"score":  vulnInfo["cvss"],
					"vector": nil,
				},
				"epss": map[string]any{
					"score":      vulnInfo["epss"],
					"percentile": nil,
				},
			},
		}

		// Include remediation if requested
		for _, inc := range includeFlag {
			if inc == "remediation" {
				fixed := vulnInfo["fixed"]
				vulnSection["remediation"] = map[string]any{
					"fix_available": fixed != nil,
					"fixed_in":      fixed,
				}
				break
			}
		}

		// Fetch findings — single source of truth for counts & details
		findingsData, err := client.Get("/sca_findings", map[string]any{
			"cve":      []string{vulnID},
			"per_page": "100",
		})
		if err != nil {
			if _, ok := err.(*api.AuthenticationError); ok {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				os.Exit(4)
			}
			fmt.Fprintf(os.Stderr, "API Error: %s\n", err)
			os.Exit(1)
		}

		findingItems, _ := findingsData["items"].([]any)

		// Get dependency name from first finding
		if len(findingItems) > 0 {
			firstFinding, _ := findingItems[0].(map[string]any)
			if firstFinding != nil {
				dep, _ := firstFinding["dependency"].(map[string]any)
				if dep != nil {
					depName, _ := dep["name"].(string)
					if depName != "" {
						vulnSection["dependency"] = depName
					}
				}
			}
		}

		byAssessment := map[string]int{}
		repositories := map[string]struct{}{}
		var findingsList []map[string]any

		for _, fi := range findingItems {
			finding, _ := fi.(map[string]any)
			if finding == nil {
				continue
			}
			fDep, _ := finding["dependency"].(map[string]any)
			if fDep == nil {
				fDep = map[string]any{}
			}
			fML, _ := finding["manifest_location"].(map[string]any)
			if fML == nil {
				fML = map[string]any{}
			}
			fSource, _ := finding["source"].(map[string]any)
			if fSource == nil {
				fSource = map[string]any{}
			}

			fRec, _ := finding["calculated_recommendation"].(string)
			fAssessment := mapping.RecommendationToAssessment(fRec)

			fAnalyses, _ := finding["analyses"].(map[string]any)
			var fSummaryStr string
			if fAnalyses != nil {
				fSummaryStr, _ = fAnalyses["qualification_summary"].(string)
			}
			if fSummaryStr == "" {
				fSummaryStr, _ = mapping.GetAssessmentSummary(fAssessment)
			}

			byAssessment[string(fAssessment)]++

			repo, _ := fML["vcs_repository_url"].(string)
			if repo != "" {
				repositories[repo] = struct{}{}
			}

			fDepName, _ := fDep["name"].(string)
			fScanner, _ := fSource["source_name"].(string)
			fSourceID, _ := fSource["identifier"].(string)
			fID, _ := finding["id"].(string)

			findingsList = append(findingsList, map[string]any{
				"id":                 fID,
				"dependency":         fDepName,
				"repository":         repo,
				"scanner":            fScanner,
				"source_id":          fSourceID,
				"assessment":         string(fAssessment),
				"assessment_summary": fSummaryStr,
			})
		}

		// Overall assessment
		var overall, overallSummary, nextSteps string
		if byAssessment["exploitable"] > 0 {
			overall = "exploitable"
			count := byAssessment["exploitable"]
			overallSummary = fmt.Sprintf("You have %d exploitable instance(s) of this vulnerability.", count)
			nextSteps = "Prioritize remediation."
		} else if byAssessment["false-positive"] > 0 {
			overall = "false-positive"
			overallSummary = "Not exploitable in your context."
			nextSteps = "You may deprioritize remediation."
		} else {
			overall = "inconclusive"
			overallSummary = "Unable to determine exploitability."
			nextSteps = "Review manually."
		}

		// Build sorted repos list
		repoList := make([]string, 0, len(repositories))
		for r := range repositories {
			repoList = append(repoList, r)
		}
		sort.Strings(repoList)

		result := map[string]any{
			"vulnerability": vulnSection,
			"assessment": map[string]any{
				"status":       overall,
				"summary":      overallSummary,
				"next_steps":   nextSteps,
				"breakdown":    byAssessment,
				"total":        len(findingsList),
				"repositories": repoList,
				"findings":     findingsList,
			},
		}

		if format == output.JSON {
			fmt.Println(output.FormatJSON(result))
		} else {
			// --- Vulnerability table ---
			v := vulnSection
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "\033[1mVulnerability\033[0m")

			tw := tabwriter.NewWriter(os.Stderr, 0, 4, 2, ' ', 0)
			fmt.Fprintf(tw, "\033[2mID\033[0m\t\033[1m%s\033[0m\n", v["id"])
			if len(aliasStrs) > 0 {
				fmt.Fprintf(tw, "\033[2mAliases\033[0m\t%s\n", strings.Join(aliasStrs, ", "))
			}
			fmt.Fprintf(tw, "\033[2mSeverity\033[0m\t%s\n", strings.ToUpper(severity))
			if depName, ok := v["dependency"].(string); ok && depName != "" {
				fmt.Fprintf(tw, "\033[2mDependency\033[0m\t%s\n", depName)
			}
			if cvssScore := vulnInfo["cvss"]; cvssScore != nil {
				fmt.Fprintf(tw, "\033[2mCVSS\033[0m\t%v\n", cvssScore)
			}
			summaryText, _ := v["summary"].(string)
			if summaryText == "" {
				summaryText = "No summary available."
			}
			fmt.Fprintf(tw, "\033[2mSummary\033[0m\t%s\n", summaryText)
			tw.Flush()

			// --- Konvu Assessment ---
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "\033[1mKonvu Assessment\033[0m")

			total := len(findingsList)
			repoCount := len(repoList)

			// Build breakdown line: "N findings across M repositories: X exploitable · Y false-positive"
			line := fmt.Sprintf("%d findings across %d repositories: ", total, repoCount)

			// Sort breakdown keys for consistent output
			breakdownKeys := make([]string, 0, len(byAssessment))
			for k := range byAssessment {
				breakdownKeys = append(breakdownKeys, k)
			}
			sort.Strings(breakdownKeys)

			parts := make([]string, 0, len(breakdownKeys))
			for _, status := range breakdownKeys {
				cnt := byAssessment[status]
				colored := mapping.Colorize(fmt.Sprintf("%d %s", cnt, status), mapping.AssessmentStatus(status))
				parts = append(parts, colored)
			}
			line += strings.Join(parts, " · ")
			fmt.Fprintln(os.Stderr, line)

			// Findings table
			if len(findingsList) > 0 {
				fmt.Fprintln(os.Stderr)
				ftw := tabwriter.NewWriter(os.Stderr, 0, 4, 2, ' ', 0)
				fmt.Fprintln(ftw, "Repository\tAssessment\tSummary")
				for _, f := range findingsList {
					repo, _ := f["repository"].(string)
					status, _ := f["assessment"].(string)
					fSummary, _ := f["assessment_summary"].(string)
					coloredStatus := mapping.Colorize(strings.ToUpper(status), mapping.AssessmentStatus(status))
					fmt.Fprintf(ftw, "%s\t%s\t%s\n", repo, coloredStatus, fSummary)
				}
				ftw.Flush()
			}
		}

		return nil
	},
}

func init() {
	vulnGetCmd.Flags().StringSliceP("include", "i", nil, "Data to include: summary,technical,exploitability,remediation,references,affected")
	vulnGetCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	vulnCmd.AddCommand(vulnGetCmd)

	// Make `konvu vuln <id>` work as alias for `konvu vuln get <id>`
	vulnCmd.Args = cobra.ArbitraryArgs
	vulnCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return vulnGetCmd.RunE(cmd, args)
		}
		return cmd.Help()
	}
	// Copy flags from vulnGetCmd to vulnCmd so they work on the alias
	vulnCmd.Flags().StringSliceP("include", "i", nil, "Data to include: summary,technical,exploitability,remediation,references,affected")
	vulnCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	rootCmd.AddCommand(vulnCmd)
}
