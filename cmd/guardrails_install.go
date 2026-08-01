package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	"github.com/KonvuInc/konvu-cli/pkg/auth"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/gitbundle"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var inOrg string

const (
	repoPollEvery = 3 * time.Second
	// Long enough to find the page, tick a checkbox and save; short enough that a terminal left
	// open overnight is not doing this.
	repoWait = 5 * time.Minute
)

var guardrailsInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Connect your GitHub organization to Konvu Guardrails",
	Long: `Connect your GitHub organization to Konvu Guardrails.

Guardrails watches pull requests through a GitHub App. This connects the App's
installation on your GitHub organization to your Konvu account, which is what lets a
baseline you record here be the one checks read on your pull requests.

If the App is not installed yet, this prints the link to install it; run the command
again once you have. Once connected it prints the link to your repository selection,
so run it again whenever you want to add a repository. Re-running is always safe: it
reports the current state rather than changing it.

Connecting an organization is not the same as giving Konvu a repository. A repository
that is not in the selection is one Konvu cannot see.

The organization defaults to the owner of your 'origin' remote.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 4 auth failed`,
	Example: `  # Connect the organization owning the repo you are in
  konvu guardrails install

  # Name it explicitly, outside a checkout
  konvu guardrails install --org acme`,
	Args: cobra.NoArgs,
	RunE: runGuardrailsInstall,
}

func init() {
	f := guardrailsInstallCmd.Flags()
	f.StringVar(&inOrg, "org", "", "GitHub organization (default: owner of your origin remote)")
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

	// The repo you are standing in, so the answer can be about it and not only the organization.
	// Empty outside a checkout, which just means the organization-level answer.
	repo := gitbundle.RepoSlug(".")
	if !strings.Contains(repo, "/") {
		repo = ""
	}

	client := api.NewClient("", "")
	defer client.Close()

	data, err := askInstall(client, account, repo)
	if err != nil {
		return err
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(data))
		return nil
	}
	if linked, _ := getBool(data, "linked"); !linked {
		if url := getStr(data, "install_url"); url != "" {
			fmt.Printf("Konvu Guardrails is not installed on %s yet. Install it here:\n\n  %s\n\n", account, url)
			fmt.Println("Then run 'konvu guardrails install' again.")
			return nil
		}
		// No link came back, so print what did rather than inventing a URL to send people to.
		fmt.Println(getStr(data, "detail"))
		return nil
	}
	fmt.Printf("Connected %s to your Konvu account.\n", getStr(data, "account"))
	manage := getStr(data, "manage_url")

	// repo_visible is null when no repo was named, or when GitHub could not be reached. Neither is
	// "your repo is missing", so neither sends anyone off to change a selection that may be fine.
	if visible, known := getBool(data, "repo_visible"); known && !visible && manage != "" {
		return waitForRepo(client, account, repo, manage)
	}

	// Printed every time, not only on the first connect. Connecting an organization is separate
	// from choosing which of its repositories Konvu can see, and that choice is changed on the
	// same page, so re-running this command is how you add a repository later.
	if manage != "" {
		fmt.Printf("  Choose which repositories Konvu can see:\n    %s\n", manage)
	}
	fmt.Println("  Run 'konvu guardrails baseline' in a repository to record its authorization.")
	return nil
}

func askInstall(client *api.Client, account, repo string) (map[string]any, error) {
	body := map[string]any{"account": account}
	if repo != "" {
		body["repo"] = repo
	}
	return client.Post(guardrailsAPI+"/dashboard/install", body)
}

// waitForRepo opens the selection page and waits for the repo to appear in it. Unlike waiting for
// someone to say they are done, this ends on a fact Konvu checked with GitHub.
func waitForRepo(client *api.Client, account, repo, manage string) error {
	// The link is printed as well as opened: OpenBrowser is best-effort and silently does nothing
	// over SSH or in a container, and "opening that page" with no page and no URL is a dead end.
	fmt.Printf("\nKonvu cannot see %s yet. Add it here:\n\n    %s\n\n", repo, manage)
	auth.OpenBrowser(manage)
	fmt.Println("Opening that page in your browser. Waiting for you to save (Ctrl-C to stop)...")

	deadline := time.Now().Add(repoWait)
	for {
		time.Sleep(repoPollEvery)
		data, err := askInstall(client, account, repo)
		if err != nil {
			return err
		}
		if visible, known := getBool(data, "repo_visible"); known && visible {
			fmt.Printf("\n%s is connected.\n", repo)
			fmt.Println("  Run 'konvu guardrails baseline' to record its authorization.")
			return nil
		}
		if time.Now().After(deadline) {
			fmt.Printf("\nStill cannot see %s after %s.\n", repo, repoWait)
			fmt.Println("Add it at the link above, then run 'konvu guardrails install' again.")
			return nil
		}
	}
}
