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
	scanPolicy  string
	scanBranch  string
	scanRepo    string
	scanTimeout time.Duration
)

// Bundles are large, so the upload gets a longer budget than an API call.
const scanUploadTimeout = 15 * time.Minute

var guardrailsScanCmd = &cobra.Command{
	Use:   "scan [repo-path]",
	Short: "Scan a repository and draft its access rules",
	Long: `Scan a repository and draft its access rules.

Packages the current commit, uploads it, and reads the access your code enforces —
who may do what. Later pull requests are checked against this scan, so a change
that breaks a rule is reported as a change rather than re-derived from scratch.

To do several repositories at once, or one you have not cloned, pass --all or --remote:
Konvu fetches the code itself, so no checkout is needed. Only repositories Konvu can
see are eligible, and it reports the ones it cannot.

The rules are drafted from the scan; approve them with
'konvu guardrails approve <repo>'. This command no longer takes any.

The repo id defaults to owner/name from your 'origin' remote.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 4 auth failed`,
	Example: `  # Scan the repo you are in
  konvu guardrails scan

  # Scan another checkout, on a named branch
  konvu guardrails scan ../web --branch release-2.3

  # Several at once, no checkout needed
  konvu guardrails scan --remote acme/web --remote acme/api

  # Every repository Konvu can see, and wait for them
  konvu guardrails scan --all --wait`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGuardrailsScan,
}

func init() {
	f := guardrailsScanCmd.Flags()
	// Kept so existing invocations do not fail on an unknown flag, but the server retired
	// client-supplied policies: it proposes one from the baseline and you ratify it.
	f.StringVarP(&scanPolicy, "policy", "p", "", "retired; rules are drafted by the scan and approved with 'approve'")
	_ = f.MarkDeprecated("policy", "rules are drafted from the scan and approved with 'konvu guardrails approve'")
	f.StringVar(&scanBranch, "branch", "", "branch to act on (default: the repository's default branch, resolved by the server)")
	f.StringVar(&scanRepo, "repo", "", "repo id (default: inferred from origin)")
	f.DurationVar(&scanTimeout, "timeout", 30*time.Minute, "how long to wait for the scan")
	f.StringP("output", "o", "", "output format: table or json")
}

// runGuardrailsScan splits the work into an inner function that returns, because
// handleGuardrailsError calls os.Exit and os.Exit does not run deferred functions — exiting
// from inside the flow would leave the staged refs and the temp bundle in the user's repo.
func runGuardrailsScan(cmd *cobra.Command, args []string) error {
	if err := scanFlow(cmd, args); err != nil {
		handleGuardrailsError(err, output.DetectOutputFormat(mustGuardrailsOutput(cmd)))
	}
	return nil
}

func scanFlow(cmd *cobra.Command, args []string) error {
	// --all/--remote hand the fetching to Konvu instead of bundling a checkout.
	if bulkRequested(cmd) {
		return bulkScanFlow(cmd, args)
	}
	repoPath := "."
	if len(args) == 1 {
		repoPath = args[0]
	}

	head, err := gitbundle.Head(repoPath, "HEAD")
	if err != nil {
		return err
	}
	repoID := scanRepo
	if repoID == "" {
		repoID = gitbundle.RepoSlug(repoPath)
	}
	// A local, not scanBranch: that is the flag's own storage, so assigning back to it would make
	// the flag read as explicitly set on any later read in the same process.
	branch := requestedBranch(cmd)

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

	// Out of band by preference, so a large bundle does not travel through the API at all. A
	// server that cannot issue an upload URL answers 501, and then the bundle goes in the request
	// itself — the same endpoint takes it either way, so there is nothing to refuse here. Refusing
	// instead gave up on a fallback the endpoint already offers, which made the command unusable
	// against such a server rather than slower.
	//
	// Exactly one of the two, never both: the endpoint rejects a request carrying a key and a
	// bundle together rather than picking one.
	key, err := uploadBundle(client, bundlePath)
	if err != nil {
		return err
	}
	var job map[string]any
	if key == "" {
		job, err = client.PostMultipart(
			guardrailsAPI+"/baselines", fields, "bundle", bundlePath, scanUploadTimeout)
	} else {
		fields.Set("bundle_key", key)
		job, err = client.PostForm(guardrailsAPI+"/baselines", fields)
	}
	if err != nil {
		return err
	}
	jobID, _ := job["job_id"].(string)
	if jobID == "" {
		return fmt.Errorf("server did not return a job id")
	}
	// No label when the branch was not stated: "acme/web@ — building" reads as a bug, and inventing
	// one here would name a branch the server may not have resolved to. The finished result prints
	// the branch the server actually used.
	if branch == "" {
		fmt.Printf("Scan queued for %s — reading your code…\n", repoID)
	} else {
		fmt.Printf("Scan queued for %s@%s — reading your code…\n", repoID, branch)
	}

	return waitForScan(client, jobID)
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
				Suggestion: "Run 'konvu guardrails list' to see the repositories you have scanned.",
				ExitCode:   clierrors.ExitNotFound,
			}
		case http.StatusUnprocessableEntity:
			// The caller's own request is wrong and the server's detail says which. The default arm
			// below suggests checking your session, the wrong remedy for a mistyped argument.
			return &clierrors.CLIError{
				Code:     "INVALID_REQUEST",
				Message:  detail,
				ExitCode: clierrors.ExitUsageError,
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
	if err := client.PutPresigned(target, bundlePath, st.Size(), scanUploadTimeout); err != nil {
		return "", err
	}
	return key, nil
}

func waitForScan(client *api.Client, jobID string) error {
	deadline := time.Now().Add(scanTimeout)
	for {
		st, err := client.Get(guardrailsAPI+"/baselines/jobs/"+jobID, nil)
		if err != nil {
			return err
		}
		switch status, _ := st["status"].(string); status {
		case "done":
			printScanResult(st)
			return nil
		case "error":
			msg, _ := st["error"].(string)
			if msg == "" {
				msg = "the scan could not be completed"
			}
			return &clierrors.CLIError{
				Code:     "SCAN_FAILED",
				Message:  msg,
				ExitCode: clierrors.ExitGeneralError,
			}
		}
		if time.Now().After(deadline) {
			return &clierrors.CLIError{
				Code:       "STILL_RUNNING",
				Message:    fmt.Sprintf("the scan was still running after %s", scanTimeout),
				Suggestion: "It may finish on its own; check with 'konvu guardrails list'.",
				ExitCode:   clierrors.ExitGeneralError,
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func printScanResult(st map[string]any) {
	bl, _ := st["baseline"].(map[string]any)
	if bl == nil {
		fmt.Println("Scan complete.")
		return
	}
	repo, _ := bl["repo"].(string)
	branch, _ := bl["branch"].(string)
	fmt.Printf("Scan complete for %s@%s\n", repo, branch)
	if paths, ok := bl["access_paths"].(float64); ok {
		fmt.Printf("  routes analyzed: %d\n", int(paths))
	}
	if inv, ok := bl["invariants"].(float64); ok {
		fmt.Printf("  rules drafted:   %d\n", int(inv))
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
