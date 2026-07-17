package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/KonvuInc/konvu-cli/skills"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

// printCmdHelp recursively prints usage for a command and its subcommands.
func printCmdHelp(cmd *cobra.Command, prefix string) {
	if cmd.Hidden {
		return
	}

	if cmd.Runnable() {
		fmt.Printf("  %s%s\n", prefix, cmd.Use)
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Hidden {
				return
			}
			short := ""
			if f.Shorthand != "" {
				short = fmt.Sprintf("-%s, ", f.Shorthand)
			}
			def := ""
			if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "[]" {
				def = fmt.Sprintf(" [default: %s]", f.DefValue)
			}
			fmt.Printf("    %s--%s    %s%s\n", short, f.Name, f.Usage, def)
		})
		fmt.Println()
	}

	for _, sub := range cmd.Commands() {
		printCmdHelp(sub, prefix+cmd.Name()+" ")
	}
}

const helpAllFooter = `EXAMPLES
  konvu finding list --since 7d --assessment exploitable
  konvu finding list --severity critical --sort first_seen_at -o json
  konvu finding list --assessment exploitable --group-by repository
  konvu finding list --assessment not-assessed --count
  konvu finding list --source snyk
  konvu finding get abc-123 --include evidence --include logs
  konvu finding rate abc-123 agree --comment "Confirmed exploitable"
  konvu finding submit --repo github:org/repo --file snyk-findings.json
  konvu finding counts --group-by severity
  konvu vuln CVE-2024-1234 --include technical
  konvu metrics --since 90d --include trends --compare previous_period
  konvu dismiss --assessment false-positive --repo org/repo --dry-run
  konvu remediate abc-123 --wait --timeout 15m
  konvu remediate status abc-123

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

func printHelpAll() {
	fmt.Println("konvu — Security vulnerability management")
	fmt.Println()
	for _, cmd := range rootCmd.Commands() {
		if cmd.Hidden {
			continue
		}
		fmt.Println(strings.ToUpper(cmd.Name()))
		printCmdHelp(cmd, "konvu ")
	}
	fmt.Println(helpAllFooter)
}

var helpAllCmd = &cobra.Command{
	Use:    "help-all",
	Short:  "Print full CLI reference",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		printHelpAll()
	},
}

func init() {
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// Skip warning for skills commands (they handle their own logic)
		if cmd.Parent() != nil && cmd.Parent().Name() == "skills" {
			return
		}
		// Skip for help/version/completion commands
		if cmd.Name() == "help-all" || cmd.Name() == "help" || cmd.Name() == "version" {
			return
		}

		if skills.NeedsUpdate() && skills.InstalledHash() != "" {
			fmt.Fprintln(os.Stderr, "Skills update available — run 'konvu skills install' to update.")
		}
	}

	rootCmd.AddCommand(helpAllCmd)

	// Check for --help-all in os.Args since cobra's flag parsing
	// treats --help-all as --help due to prefix matching.
	for _, arg := range os.Args[1:] {
		if arg == "--help-all" {
			printHelpAll()
			os.Exit(0)
		}
	}
}
