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
	"github.com/KonvuInc/konvu-cli/pkg/output"
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

The policy is proposed from the recorded baseline; agree to it with
'konvu guardrails ratify <repo>'. This command no longer takes one.

The repo id defaults to owner/name from your 'origin' remote.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 4 auth failed`,
	Example: `  # Baseline the repo you are in
  konvu guardrails baseline

  # Baseline another checkout, on a named branch
  konvu guardrails baseline ../web --branch release-2.3`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGuardrailsBaseline,
}

func init() {
	f := guardrailsBaselineCmd.Flags()
	// Kept so existing invocations do not fail on an unknown flag, but the server retired
	// client-supplied policies: it proposes one from the baseline and you ratify it.
	f.StringVarP(&blPolicy, "policy", "p", "", "retired; the policy is proposed and ratified in the dashboard")
	_ = f.MarkDeprecated("policy", "the policy is proposed from the baseline and ratified in the dashboard")
	f.StringVar(&blBranch, "branch", "", "branch to act on (default: the repository's default branch)")
	f.StringVar(&blRepo, "repo", "", "repo id (default: inferred from origin)")
	f.DurationVar(&blTimeout, "timeout", 30*time.Minute, "how long to wait for the baseline to build")
}

// runGuardrailsBaseline splits the work into an inner function that returns, because
// handleGuardrailsError calls os.Exit and os.Exit does not run deferred functions — exiting
// from inside the flow would leave the staged refs and the temp bundle in the user's repo.
func runGuardrailsBaseline(cmd *cobra.Command, args []string) error {
	if err := baselineFlow(cmd, args); err != nil {
		handleGuardrailsError(err, output.DetectOutputFormat(""))
	}
	return nil
}

func baselineFlow(cmd *cobra.Command, args []string) error {
	repoPath := "."
	if len(args) == 1 {
		repoPath = args[0]
	}

	head, err := gitbundle.Head(repoPath, "HEAD")
	if err != nil {
		return err
	}
	repoID := blRepo
	if repoID == "" {
		repoID = gitbundle.RepoSlug(repoPath)
	}
	// A local, not blBranch: that is the flag's own storage, so assigning back to it would make
	// the flag read as explicitly set on any later resolution in the same process.
	branch := resolveBranch(cmd, repoID, repoPath)

	bundlePath, cleanup, err := gitbundle.Create(repoPath, head)
	if err != nil {
		return err
	}
	defer cleanup()

	client := api.NewClient("", "")
	defer client.Close()

	// No policy field: the server rejects one outright now, rather than ignoring it.
	fields := url.Values{"repo": {repoID}}
	// Omitted when unknown, so the server resolves the repository's default branch rather than
	// receiving a guess it cannot tell apart from a deliberate choice.
	if branch != "" {
		fields.Set("branch", branch)
	}

	// Upload out of band, so the bundle does not travel through the API. A server that cannot
	// issue an upload URL answers 501; this command has no inline path, so that is an error
	// rather than a fallback.
	key, err := uploadBundle(client, bundlePath)
	if err != nil {
		return err
	}
	if key == "" {
		return &clierrors.CLIError{
			Code:       "UPLOAD_UNAVAILABLE",
			Message:    "this server cannot issue an upload URL for the bundle",
			Suggestion: "Update the CLI, or ask whoever runs the service.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	fields.Set("bundle_key", key)

	job, err := client.PostForm(guardrailsAPI+"/baselines", fields)
	if err != nil {
		return err
	}
	jobID, _ := job["job_id"].(string)
	if jobID == "" {
		return fmt.Errorf("server did not return a job id")
	}
	fmt.Printf("Baseline queued for %s@%s — building…\n", repoID, branch)

	return waitForBaseline(client, jobID)
}

// guardrailsCLIError classifies a failure into the shared CLI error shape: a real exit code
// and a suggestion. Split from the printing so the classification can be tested.
//
// These endpoints answer with a readable "detail", and echoing it inside `API error: {...}`
// buries the one part the reader can act on.
func guardrailsCLIError(err error) *clierrors.CLIError {
	switch e := err.(type) {
	case *clierrors.CLIError:
		return e
	case *api.AuthenticationError:
		return clierrors.NewAuthError(e.Error())
	case *api.APIError:
		detail := api.ServerDetail([]byte(strings.TrimPrefix(e.Message, "API error: ")))
		if detail == "" {
			detail = e.Error()
		}
		switch e.StatusCode {
		case http.StatusForbidden:
			return &clierrors.CLIError{
				Code:       "NOT_AVAILABLE",
				Message:    detail,
				Suggestion: "Guardrails is not available for this account yet.",
				ExitCode:   clierrors.ExitGeneralError,
			}
		case http.StatusNotFound:
			return &clierrors.CLIError{
				Code:       "NOT_FOUND",
				Message:    detail,
				Suggestion: "Run 'konvu guardrails list' to see recorded baselines.",
				ExitCode:   clierrors.ExitNotFound,
			}
		case http.StatusConflict:
			// 409 covers more than a stale baseline (a draft that cannot be ratified answers it
			// too), and the server's detail already says which. Naming one remedy for all of
			// them sends readers somewhere that cannot help, so let the detail speak.
			return &clierrors.CLIError{
				Code:     "CONFLICT",
				Message:  detail,
				ExitCode: clierrors.ExitGeneralError,
			}
		case http.StatusServiceUnavailable:
			// The default below suggests checking your session, which is wrong here and sends
			// people to re-authenticate over something that has nothing to do with them.
			return &clierrors.CLIError{
				Code:       "UNAVAILABLE",
				Message:    detail,
				Suggestion: "Try again shortly, or contact Konvu support if it persists.",
				Retryable:  true,
				ExitCode:   clierrors.ExitGeneralError,
			}
		default:
			return clierrors.NewAPIError(detail)
		}
	default:
		return clierrors.NewAPIError(err.Error())
	}
}

// handleGuardrailsError prints and exits, matching how the other command groups report a
// runtime failure — returning the error from RunE would make cobra dump the usage block.
func handleGuardrailsError(err error, format output.OutputFormat) {
	cliErr := guardrailsCLIError(err)
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
			return err
		}
		switch status, _ := st["status"].(string); status {
		case "done":
			printBaselineResult(st)
			return nil
		case "error":
			msg, _ := st["error"].(string)
			if msg == "" {
				msg = "the baseline could not be built"
			}
			return &clierrors.CLIError{
				Code:     "BASELINE_FAILED",
				Message:  msg,
				ExitCode: clierrors.ExitGeneralError,
			}
		}
		if time.Now().After(deadline) {
			return &clierrors.CLIError{
				Code:       "STILL_BUILDING",
				Message:    fmt.Sprintf("the baseline was still building after %s", blTimeout),
				Suggestion: "It may finish on its own; check with 'konvu guardrails list'.",
				ExitCode:   clierrors.ExitGeneralError,
			}
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
