package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/gitbundle"
	"github.com/spf13/cobra"
)

var (
	blPolicy  string
	blBranch  string
	blRepo    string
	blTimeout time.Duration
)

// Bundles are large, so the upload gets a longer budget than an API call.
const baselineUploadTimeout = 15 * time.Minute

var guardrailsBaselineCmd = &cobra.Command{
	Use:   "baseline [repo-path]",
	Short: "Scan a repo and freeze an authorization baseline",
	Long: `Scan a repo and freeze an authorization baseline.

Packages the current commit, uploads it, and records the authorization your code
enforces. Later checks compare against this baseline, so drift is reported as a
change rather than re-derived from scratch.

The repo id defaults to owner/name from your 'origin' remote.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 4 auth failed`,
	Example: `  # Baseline the repo you are in
  konvu guardrails baseline --policy policy.yaml

  # Baseline another checkout, on a named branch
  konvu guardrails baseline ../web --policy policy.yaml --branch release-2.3`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGuardrailsBaseline,
}

func init() {
	f := guardrailsBaselineCmd.Flags()
	f.StringVarP(&blPolicy, "policy", "p", "", "policy file describing intended authorization — required")
	f.StringVar(&blBranch, "branch", "main", "branch this baseline applies to")
	f.StringVar(&blRepo, "repo", "", "repo id (default: inferred from origin)")
	f.DurationVar(&blTimeout, "timeout", 30*time.Minute, "how long to wait for the baseline to build")
	_ = guardrailsBaselineCmd.MarkFlagRequired("policy")
}

func runGuardrailsBaseline(cmd *cobra.Command, args []string) error {
	repoPath := "."
	if len(args) == 1 {
		repoPath = args[0]
	}

	policy, err := os.ReadFile(blPolicy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: read policy: %v\n", err)
		os.Exit(clierrors.ExitUsageError)
	}

	head, err := gitbundle.Head(repoPath, "HEAD")
	if err != nil {
		return err
	}
	repoID := blRepo
	if repoID == "" {
		repoID = gitbundle.RepoSlug(repoPath)
	}

	bundlePath, cleanup, err := gitbundle.Create(repoPath, head)
	if err != nil {
		return err
	}
	defer cleanup()

	client := api.NewClient("", "")
	defer client.Close()

	fields := url.Values{
		"repo":   {repoID},
		"branch": {blBranch},
		"policy": {string(policy)},
	}

	// Upload out of band when the server offers it, so the bundle does not travel through
	// the API. A server that cannot issue an upload URL answers 501, and then the bundle is
	// sent inline instead — the only difference is where the bytes go.
	key, err := uploadBundle(client, bundlePath)
	if err != nil {
		return friendlyError(err)
	}
	if key == "" {
		fmt.Fprintln(os.Stderr, "Error: this server requires an inline bundle upload, which this command does not support yet")
		os.Exit(clierrors.ExitGeneralError)
	}
	fields.Set("bundle_key", key)

	job, err := client.PostForm(guardrailsAPI+"/baselines", fields)
	if err != nil {
		return friendlyError(err)
	}
	jobID, _ := job["job_id"].(string)
	if jobID == "" {
		return fmt.Errorf("server did not return a job id")
	}
	fmt.Printf("Baseline queued for %s@%s — building…\n", repoID, blBranch)

	return waitForBaseline(client, jobID)
}

// friendlyError rewrites a refusal into something the reader can act on. A 403 means the
// account reached guardrails and was turned away; echoing the raw JSON body leaves the
// reader with nothing to do about it.
func friendlyError(err error) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		detail := api.ServerDetail([]byte(strings.TrimPrefix(apiErr.Message, "API error: ")))
		return fmt.Errorf("guardrails is not available for this account yet: %s", detail)
	}
	return err
}

// uploadBundle asks for an upload URL and PUTs the bundle to it. Returns "" when the
// server does not offer out-of-band upload.
func uploadBundle(client *api.Client, bundlePath string) (string, error) {
	st, err := os.Stat(bundlePath)
	if err != nil {
		return "", err
	}

	resp, err := client.PostForm(guardrailsAPI+"/baselines/upload-url", url.Values{
		"size_bytes": {strconv.FormatInt(st.Size(), 10)},
	})
	if err != nil {
		// 501 means this server keeps bundles locally and cannot issue an upload URL.
		// Anything else — over-size, auth, service not enabled — is a real refusal.
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 501 {
			return "", nil
		}
		return "", err
	}

	target, _ := resp["url"].(string)
	key, _ := resp["bundle_key"].(string)
	if target == "" || key == "" {
		return "", fmt.Errorf("server did not return an upload url")
	}
	if err := client.PutPresigned(target, bundlePath, st.Size(), baselineUploadTimeout); err != nil {
		return "", err
	}
	return key, nil
}

func waitForBaseline(client *api.Client, jobID string) error {
	deadline := time.Now().Add(blTimeout)
	for {
		st, err := client.Get(guardrailsAPI+"/baselines/jobs/"+jobID, nil)
		if err != nil {
			return friendlyError(err)
		}
		switch status, _ := st["status"].(string); status {
		case "done":
			printBaselineResult(st)
			return nil
		case "error":
			msg, _ := st["error"].(string)
			if msg == "" {
				msg = "baseline failed"
			}
			fmt.Fprintln(os.Stderr, "Error:", msg)
			os.Exit(clierrors.ExitGeneralError)
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "Error: still building after %s — check back with 'konvu guardrails baseline' later\n", blTimeout)
			os.Exit(clierrors.ExitGeneralError)
		}
		time.Sleep(2 * time.Second)
	}
}

func printBaselineResult(st map[string]any) {
	bl, _ := st["baseline"].(map[string]any)
	if bl == nil {
		fmt.Println("Baseline recorded.")
		return
	}
	repo, _ := bl["repo"].(string)
	branch, _ := bl["branch"].(string)
	fmt.Printf("Baseline recorded for %s@%s\n", repo, branch)
	if paths, ok := bl["access_paths"].(float64); ok {
		fmt.Printf("  routes modelled: %d\n", int(paths))
	}
	if inv, ok := bl["invariants"].(float64); ok {
		fmt.Printf("  invariants:      %d\n", int(inv))
	}
	if notes, ok := st["grounding_notes"].([]any); ok && len(notes) > 0 {
		fmt.Println("  notes:")
		for _, n := range notes {
			if s, ok := n.(string); ok {
				fmt.Printf("    - %s\n", s)
			}
		}
	}
}
