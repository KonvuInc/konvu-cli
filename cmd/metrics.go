package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/KonvuTeam/konvu-cli/pkg/api"
	"github.com/KonvuTeam/konvu-cli/pkg/mapping"
	"github.com/KonvuTeam/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Security metrics and reporting",
}

var metricsShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show security posture metrics",
	Long: `Show security posture metrics.

Examples:
  konvu metrics show
  konvu metrics show --include top_cves,new_vs_closed
  konvu metrics show --include summary --output json

Exit codes:
  0  Success
  1  General error
  4  Authentication failed`,
	RunE: func(cmd *cobra.Command, args []string) error {
		since, _ := cmd.Flags().GetString("since")
		until, _ := cmd.Flags().GetString("until")
		interval, _ := cmd.Flags().GetString("interval")
		include, _ := cmd.Flags().GetStringSlice("include")
		outputFlag, _ := cmd.Flags().GetString("output")

		// Default include
		if len(include) == 0 {
			include = []string{"summary", "trends"}
		}

		// Build set for fast lookup
		includeSet := make(map[string]bool, len(include))
		for _, s := range include {
			includeSet[s] = true
		}

		// Map interval to API period parameter
		period := "day"
		if interval == "week" {
			period = "week"
		} else if interval == "month" {
			period = "month"
		}

		client := api.NewClient("", "")
		defer client.Close()

		// Build output map
		result := map[string]any{
			"period": map[string]any{
				"since":    since,
				"until":    until,
				"interval": interval,
			},
		}

		var backlog, toFix, toDismiss []any

		if includeSet["summary"] || includeSet["trends"] {
			var err error
			backlog, err = client.GetList("/overview/backlog", map[string]any{"period": period})
			if err != nil {
				handleMetricsError(err)
				return nil
			}
			toFix, err = client.GetList("/overview/backlog_to_fix", map[string]any{"period": period})
			if err != nil {
				handleMetricsError(err)
				return nil
			}
			toDismiss, err = client.GetList("/overview/backlog_to_dismiss", map[string]any{"period": period})
			if err != nil {
				handleMetricsError(err)
				return nil
			}

			if len(backlog) > 0 {
				latest := toMap(backlog[len(backlog)-1])
				latestFix := map[string]any{}
				latestDismiss := map[string]any{}
				if len(toFix) > 0 {
					latestFix = toMap(toFix[len(toFix)-1])
				}
				if len(toDismiss) > 0 {
					latestDismiss = toMap(toDismiss[len(toDismiss)-1])
				}

				result["summary"] = map[string]any{
					"total_open":     getInt(latest, "open_issues"),
					"exploitable":    getInt(latestFix, "open_to_fix"),
					"false_positive": getInt(latestDismiss, "open_to_dismiss"),
				}
			}

			if includeSet["trends"] {
				result["trends"] = map[string]any{
					"backlog":    backlog,
					"to_fix":     toFix,
					"to_dismiss": toDismiss,
				}
			}
		}

		if includeSet["top_cves"] {
			topCVEs, err := client.GetList("/overview/top_cves_to_prioritize", nil)
			if err != nil {
				handleMetricsError(err)
				return nil
			}
			cves := make([]map[string]any, 0, len(topCVEs))
			for _, item := range topCVEs {
				m := toMap(item)
				aliases := []string{}
				if raw, ok := m["aliases"]; ok {
					if rawSlice, ok := raw.([]any); ok {
						for _, a := range rawSlice {
							if s, ok := a.(string); ok {
								aliases = append(aliases, s)
							}
						}
					}
				}
				cves = append(cves, map[string]any{
					"vulnerability_id": m["vulnerability_id"],
					"aliases":          aliases,
					"recommendation":   m["recommendation"],
				})
			}
			result["top_cves"] = cves
		}

		if includeSet["new_vs_closed"] {
			newVsClosed, err := client.GetList("/overview/new_vs_closed", map[string]any{"period": period})
			if err != nil {
				handleMetricsError(err)
				return nil
			}
			result["new_vs_closed"] = newVsClosed
		}

		// Output
		format := output.DetectOutputFormat(outputFlag)

		if format == output.JSON {
			fmt.Println(output.FormatJSON(result))
			return nil
		}

		// Human-readable table output (to stderr, matching Python's Console(stderr=True))
		w := tabwriter.NewWriter(os.Stderr, 0, 0, 2, ' ', 0)

		// --- Summary ---
		summaryRaw, hasSummary := result["summary"]
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Security Posture")

		if hasSummary {
			summary := toMap(summaryRaw)
			totalOpen := fmt.Sprintf("%v", summary["total_open"])
			exploitable := fmt.Sprintf("%v", summary["exploitable"])
			falsePos := fmt.Sprintf("%v", summary["false_positive"])

			fmt.Fprintf(w, "  Total Open\t%s\n", totalOpen)
			fmt.Fprintf(w, "  Exploitable\t%s%s%s\n",
				mapping.GetAssessmentColor("exploitable"), exploitable, mapping.ColorReset())
			fmt.Fprintf(w, "  False Positive\t%s%s%s\n",
				mapping.GetAssessmentColor("false-positive"), falsePos, mapping.ColorReset())
			w.Flush()
		}

		// --- Trends ---
		if trendsRaw, ok := result["trends"]; ok {
			trends := toMap(trendsRaw)
			backlogPts := toSlice(trends["backlog"])
			fixPts := toSlice(trends["to_fix"])
			dismissPts := toSlice(trends["to_dismiss"])

			// Index to_fix and to_dismiss by date
			fixByDate := make(map[string]map[string]any, len(fixPts))
			for _, pt := range fixPts {
				m := toMap(pt)
				date := getDateKey(m)
				fixByDate[date] = m
			}
			dismissByDate := make(map[string]map[string]any, len(dismissPts))
			for _, pt := range dismissPts {
				m := toMap(pt)
				date := getDateKey(m)
				dismissByDate[date] = m
			}

			if len(backlogPts) > 0 {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintf(os.Stderr, "Trend (%sly)\n", interval)

				tw := tabwriter.NewWriter(os.Stderr, 0, 0, 2, ' ', 0)
				fmt.Fprintf(tw, "  Period\tTotal\tExploitable\tFalse Positive\n")
				fmt.Fprintf(tw, "  ------\t-----\t-----------\t--------------\n")

				for _, ptRaw := range backlogPts {
					pt := toMap(ptRaw)
					rawDate := getDateKey(pt)

					label := rawDate
					if len(rawDate) >= 10 {
						label = rawDate[:10]
					}
					if interval == "month" && len(label) >= 7 {
						label = label[:7]
					} else if interval == "week" && len(label) >= 10 {
						label = "w/o " + label
					}

					total := fmt.Sprintf("%v", getInt(pt, "open_issues"))

					fixPt := fixByDate[rawDate]
					dismissPt := dismissByDate[rawDate]
					exploitable := fmt.Sprintf("%v", getInt(fixPt, "open_to_fix"))
					falsePos := fmt.Sprintf("%v", getInt(dismissPt, "open_to_dismiss"))

					fmt.Fprintf(tw, "  %s\t%s\t%s%s%s\t%s%s%s\n",
						label,
						total,
						mapping.GetAssessmentColor("exploitable"), exploitable, mapping.ColorReset(),
						mapping.GetAssessmentColor("false-positive"), falsePos, mapping.ColorReset(),
					)
				}
				tw.Flush()
			}
		}

		// --- Top CVEs ---
		if topCVEsRaw, ok := result["top_cves"]; ok {
			topCVEs, _ := topCVEsRaw.([]map[string]any)
			if len(topCVEs) > 0 {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, "Top CVEs to Prioritize")

				cw := tabwriter.NewWriter(os.Stderr, 0, 0, 2, ' ', 0)
				fmt.Fprintf(cw, "  #\tCVE\n")

				limit := 5
				if len(topCVEs) < limit {
					limit = len(topCVEs)
				}
				for i, cve := range topCVEs[:limit] {
					aliases, _ := cve["aliases"].([]string)
					cveID := ""
					if len(aliases) > 0 {
						cveID = aliases[0]
					} else if id, ok := cve["vulnerability_id"].(string); ok {
						cveID = id
					} else {
						cveID = "Unknown"
					}
					fmt.Fprintf(cw, "  %d\t%s\n", i+1, cveID)
				}
				cw.Flush()
			}
		}

		// --- New vs Closed ---
		if nvcRaw, ok := result["new_vs_closed"]; ok {
			nvcSlice := toSlice(nvcRaw)
			if len(nvcSlice) > 0 {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, "New vs Closed")

				nw := tabwriter.NewWriter(os.Stderr, 0, 0, 2, ' ', 0)
				fmt.Fprintf(nw, "  Date\tNew\tClosed\n")
				fmt.Fprintf(nw, "  ----\t---\t------\n")

				// Show last 5 points
				start := 0
				if len(nvcSlice) > 5 {
					start = len(nvcSlice) - 5
				}
				for _, ptRaw := range nvcSlice[start:] {
					pt := toMap(ptRaw)
					date := getString(pt, "date", "?")
					newCount := fmt.Sprintf("%v", getInt(pt, "new"))
					closedCount := fmt.Sprintf("%v", getInt(pt, "closed"))
					fmt.Fprintf(nw, "  %s\t+%s\t-%s\n", date, newCount, closedCount)
				}
				nw.Flush()
			}
		}

		fmt.Fprintln(os.Stderr)
		return nil
	},
}

func handleMetricsError(err error) {
	if _, ok := err.(*api.AuthenticationError); ok {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(4)
	}
	fmt.Fprintln(os.Stderr, "API Error:", err)
	os.Exit(1)
}

// toMap safely converts any to map[string]any.
func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// toSlice safely converts any to []any.
func toSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

// getInt returns an integer value from a map, defaulting to 0.
func getInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	}
	return 0
}

// getString returns a string value from a map with a fallback.
func getString(m map[string]any, key, fallback string) string {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok {
		return fallback
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}

// getDateKey extracts a date string from a data point map (checks "date" then "period_start").
func getDateKey(m map[string]any) string {
	if d, ok := m["date"].(string); ok && d != "" {
		return d
	}
	if d, ok := m["period_start"].(string); ok && d != "" {
		return d
	}
	return ""
}

func init() {
	metricsShowCmd.Flags().String("since", "30d", "Start date: '30d', '90d', or ISO date")
	metricsShowCmd.Flags().String("until", "now", "End date: 'now' or ISO date")
	metricsShowCmd.Flags().String("interval", "week", "Aggregation interval: day, week, month")
	metricsShowCmd.Flags().StringSliceP("include", "i", nil, "Data to include: summary,trends,breakdown,top_cves,new_vs_closed")
	metricsShowCmd.Flags().String("compare", "", "Compare to: previous_period, 30d_ago, 90d_ago")
	metricsShowCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	metricsCmd.AddCommand(metricsShowCmd)

	// Top-level convenience alias: `konvu metrics` runs show by default
	metricsCmd.Args = cobra.ArbitraryArgs
	metricsCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			// Copy flag values to metricsShowCmd and run it
			since, _ := cmd.Flags().GetString("since")
			until, _ := cmd.Flags().GetString("until")
			interval, _ := cmd.Flags().GetString("interval")
			include, _ := cmd.Flags().GetStringSlice("include")
			compare, _ := cmd.Flags().GetString("compare")
			outputFlag, _ := cmd.Flags().GetString("output")

			if cmd.Flags().Changed("since") {
				metricsShowCmd.Flags().Set("since", since)
			}
			if cmd.Flags().Changed("until") {
				metricsShowCmd.Flags().Set("until", until)
			}
			if cmd.Flags().Changed("interval") {
				metricsShowCmd.Flags().Set("interval", interval)
			}
			if cmd.Flags().Changed("include") {
				metricsShowCmd.Flags().Set("include", strings.Join(include, ","))
			}
			if cmd.Flags().Changed("compare") {
				metricsShowCmd.Flags().Set("compare", compare)
			}
			if cmd.Flags().Changed("output") {
				metricsShowCmd.Flags().Set("output", outputFlag)
			}

			return metricsShowCmd.RunE(metricsShowCmd, args)
		}
		return cmd.Help()
	}

	// Copy flags to parent so they work on the alias
	metricsCmd.Flags().String("since", "30d", "Start date: '30d', '90d', or ISO date")
	metricsCmd.Flags().String("until", "now", "End date: 'now' or ISO date")
	metricsCmd.Flags().String("interval", "week", "Aggregation interval: day, week, month")
	metricsCmd.Flags().StringSliceP("include", "i", nil, "Data to include: summary,trends,breakdown,top_cves,new_vs_closed")
	metricsCmd.Flags().String("compare", "", "Compare to: previous_period, 30d_ago, 90d_ago")
	metricsCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	rootCmd.AddCommand(metricsCmd)
}
