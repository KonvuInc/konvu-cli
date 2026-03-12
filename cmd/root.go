package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "konvu",
	Short: "Konvu CLI - Security vulnerability management",
	Long:  "Konvu CLI for security vulnerability management from your terminal.",
}

// RootCmd returns the root cobra command, allowing external modules
// (e.g. konvu-admin-cli) to import and extend the command tree.
func RootCmd() *cobra.Command {
	return rootCmd
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var helpAllText = `konvu-cli — Security vulnerability management

AUTHENTICATION
  konvu login                          Authenticate with Konvu (opens browser)
    -t, --timeout INT    Login timeout in seconds [default: 300]
    --api-key TEXT        Authenticate with an API key
  konvu logout                         Clear stored credentials
  konvu whoami [-o json]               Show current user and company

FINDINGS
  konvu finding list [OPTIONS]         List security findings
    --since TEXT          Start date: '7d', '30d', or ISO date
    --until TEXT          End date: 'now' or ISO date
    -s, --severity TEXT   Filter: critical, high, moderate, low
    -a, --assessment TEXT Filter: exploitable, false-positive, inconclusive, not-assessed
    --state TEXT          Filter: open, dismissed, fixed, muted
    --has-fix TEXT        Filter: fixed, no_fix
    -r, --repo TEXT       Filter by repository URL or name
    --cve TEXT            Filter by CVE ID
    -d, --dependency TEXT Filter by dependency name
    --source TEXT         Filter by scanner source: snyk, dependabot, etc.
    --source-id TEXT      Filter by external source identifier
    --sort TEXT           Sort: severity, recommendation, first_seen_at, updated_at, dependency_name, cve [default: recommendation]
    --order TEXT          Order: asc, desc [default: desc]
    -n, --limit INT       Max findings [default: 50]
    --offset INT          Skip N results [default: 0]
    -o, --output TEXT     Format: json, table, csv
    -q, --quiet           Output bare finding IDs only
    --count               Output only the total count
    -g, --group-by TEXT   Group by: repository, dependency, severity, assessment
    --fields TEXT         Comma-separated fields to include

  konvu finding get FINDING_ID [OPTIONS]  Get finding detail
    -i, --include TEXT    Include: evidence, logs
    -v, --verbose         Show all details for each check
    -o, --output TEXT     Format: json, table
    --fields TEXT         Comma-separated fields to include

  konvu finding rate FINDING_ID RATING   Rate a finding (agree/disagree)
    -c, --comment TEXT    Optional feedback comment
    --recommendation-id   Recommendation ID (skips extra API call)
    -o, --output TEXT     Format: json, table

  konvu finding counts [OPTIONS]       Assessment counts
    --since TEXT          Start date: '7d', '30d', or ISO date
    --until TEXT          End date: 'now' or ISO date
    -s, --severity TEXT   Filter: critical, high, moderate, low
    -r, --repo TEXT       Filter by repository
    --source TEXT         Filter by scanner source
    -g, --group-by TEXT   Break down by: severity, week, month
    -o, --output TEXT     Format: json, table

VULNERABILITY LOOKUP
  konvu vuln VULN_ID [OPTIONS]         Look up a CVE/GHSA
    -i, --include TEXT    Include: summary, technical, exploitability, remediation, references
    -o, --output TEXT     Format: json, table

METRICS
  konvu metrics [OPTIONS]              Security posture summary
    --since TEXT          Start date [default: 30d]
    --until TEXT          End date [default: now]
    --interval TEXT       Aggregation: day, week, month [default: week]
    -i, --include TEXT    Include: summary, trends, breakdown, top_cves, new_vs_closed
    --compare TEXT        Compare to: previous_period, 30d_ago, 90d_ago
    -o, --output TEXT     Format: json, table

DISMISS
  konvu dismiss [OPTIONS]              Dismiss security issues
    --issues TEXT         Comma-separated issue IDs
    -a, --assessment TEXT Filter: dismiss all with this assessment
    -s, --severity TEXT   Filter by severity
    -r, --repo TEXT       Filter by repository
    --reason TEXT         Reason [default: "Dismissed via Konvu CLI"]
    --dry-run             Preview without executing
    -o, --output TEXT     Format: json, table

EXAMPLES
  konvu finding list --since 7d --assessment exploitable
  konvu finding list --severity critical --sort first_seen_at -o json
  konvu finding list --assessment exploitable --group-by repository
  konvu finding list --assessment not-assessed --count
  konvu finding list --source snyk
  konvu finding get abc-123 --include evidence --include logs
  konvu finding rate abc-123 agree --comment "Confirmed exploitable"
  konvu finding counts --group-by severity
  konvu vuln CVE-2024-1234 --include technical
  konvu metrics --since 90d --include trends --compare previous_period
  konvu dismiss --assessment false-positive --repo org/repo --dry-run

OUTPUT FORMATS
  Most commands support: -o json (structured), -o table (human), -o csv (finding list only)
  Default is json when piped, table when interactive.
  Use -q/--quiet on finding list for bare IDs (useful for piping).

EXIT CODES
  0  Success
  1  General error
  2  Invalid arguments
  3  Not found
  4  Authentication failed`

var helpAllCmd = &cobra.Command{
	Use:    "help-all",
	Short:  "Print full CLI reference",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(helpAllText)
	},
}

func init() {
	rootCmd.AddCommand(helpAllCmd)

	// Check for --help-all in os.Args since cobra's flag parsing
	// treats --help-all as --help due to prefix matching.
	for _, arg := range os.Args[1:] {
		if arg == "--help-all" {
			fmt.Println(helpAllText)
			os.Exit(0)
		}
	}
}
