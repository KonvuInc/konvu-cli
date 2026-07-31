package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/gitbundle"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var (
	inOrg  string
	inWait time.Duration
)

// Long enough for someone to open the link, sign in, pick repositories and finish, without
// hanging a terminal forever if they wander off. --wait 0 skips waiting entirely.
const installPollEvery = 5 * time.Second

var guardrailsInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Connect your GitHub organization to Konvu Guardrails",
	Long: `Connect your GitHub organization to Konvu Guardrails.

Guardrails watches pull requests through a GitHub App. This connects the App's
installation on your GitHub organization to your Konvu account, which is what lets a
baseline you record here be the one checks read on your pull requests.

If the App is not installed yet, this prints the link to install it and then waits.
Safe to run again at any time: it reports the current state rather than changing it.

The organization defaults to the owner of your 'origin' remote.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 4 auth failed`,
	Example: `  # Connect the organization owning the repo you are in
  konvu guardrails install

  # Name it explicitly, outside a checkout
  konvu guardrails install --org acme

  # Print the link and exit instead of waiting (CI, scripts)
  konvu guardrails install --org acme --wait 0`,
	Args: cobra.NoArgs,
	RunE: runGuardrailsInstall,
}

func init() {
	f := guardrailsInstallCmd.Flags()
	f.StringVar(&inOrg, "org", "", "GitHub organization (default: owner of your origin remote)")
	f.DurationVar(&inWait, "wait", 10*time.Minute, "how long to wait for the installation; 0 to not wait")
	f.StringP("output", "o", "", "output format: table or json")
}

func runGuardrailsInstall(cmd *cobra.Command, args []string) error {
	if err := installFlow(cmd, args); err != nil {
		handleGuardrailsError(err, output.DetectOutputFormat(mustGuardrailsOutput(cmd)))
	}
	return nil
}

// githubOwner is the account half of "owner/name". RepoSlug falls back to the directory name
// when there is no origin remote, and a local directory name is not a GitHub organization, so
// return nothing and let the caller ask rather than send a guess.
func githubOwner(dir string) string {
	slug := gitbundle.RepoSlug(dir)
	owner, _, found := strings.Cut(slug, "/")
	if !found {
		return ""
	}
	return owner
}

func installFlow(cmd *cobra.Command, _ []string) error {
	format := output.DetectOutputFormat(mustGuardrailsOutput(cmd))

	account := inOrg
	if account == "" {
		account = githubOwner(".")
	}
	if account == "" {
		fmt.Fprintln(os.Stderr,
			"Error: no 'origin' remote to read the organization from; pass --org, e.g. 'konvu guardrails install --org acme'")
		os.Exit(clierrors.ExitUsageError)
	}

	client := api.NewClient("", "")
	defer client.Close()

	data, err := client.Post(guardrailsAPI+"/dashboard/install", map[string]any{"account": account})
	if err != nil {
		return err
	}

	linked, _ := getBool(data, "linked")
	if !linked && inWait > 0 {
		printInstallPrompt(data, account, format)
		if data, err = waitForInstall(client, account); err != nil {
			return err
		}
		linked, _ = getBool(data, "linked")
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(data))
		return nil
	}
	if !linked {
		printInstallPrompt(data, account, format)
		return nil
	}
	fmt.Printf("Connected %s to your Konvu account.\n", getStr(data, "account"))
	fmt.Println("  Run 'konvu guardrails baseline' in a repository to record its authorization.")
	return nil
}

func printInstallPrompt(data map[string]any, account string, format output.OutputFormat) {
	if format == output.JSON {
		return
	}
	if url := getStr(data, "install_url"); url != "" {
		fmt.Printf("Konvu Guardrails is not installed on %s yet. Install it here:\n\n  %s\n\n", account, url)
		return
	}
	// No link came back, so print what did rather than inventing a URL to send people to.
	fmt.Println(getStr(data, "detail"))
}

// waitForInstall re-asks until the installation shows up, so the command can be left running
// while someone completes the install in a browser.
func waitForInstall(client *api.Client, account string) (map[string]any, error) {
	fmt.Printf("Waiting for the installation (up to %s; Ctrl-C to stop)...\n", inWait)
	deadline := time.Now().Add(inWait)
	for {
		time.Sleep(installPollEvery)
		data, err := client.Post(guardrailsAPI+"/dashboard/install", map[string]any{"account": account})
		if err != nil {
			return nil, err
		}
		if linked, _ := getBool(data, "linked"); linked {
			return data, nil
		}
		if time.Now().After(deadline) {
			return data, &clierrors.CLIError{
				Code:       "TIMEOUT",
				Message:    fmt.Sprintf("no installation on %s after %s", account, inWait),
				Suggestion: "Finish installing the app in your browser, then run 'konvu guardrails install' again.",
				ExitCode:   clierrors.ExitGeneralError,
			}
		}
	}
}
