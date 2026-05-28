package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	"github.com/KonvuInc/konvu-cli/pkg/config"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

// scmType identifies the source control system for a finding.
type scmType string

const (
	scmGitHub  scmType = "github"
	scmGitLab  scmType = "gitlab"
	scmUnknown scmType = ""
)

// label returns a human-readable SCM name; "your SCM" when unknown.
func (s scmType) label() string {
	switch s {
	case scmGitHub:
		return "GitHub"
	case scmGitLab:
		return "GitLab"
	default:
		return "your SCM"
	}
}

var remediateCmd = &cobra.Command{
	Use:     "remediate [finding-id]",
	Aliases: []string{"autofix"},
	Short:   "Trigger vulnerability remediation",
	Long: `Trigger Konvu's remediation engine to open a fix PR for a finding.

Remediation runs asynchronously inside the customer's on-prem controller
(patcheus engine). Use 'konvu remediate status' or '--wait' to track progress.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  # Trigger remediation for a finding (top-level alias)
  konvu remediate abc-123

  # Same, with the explicit run subcommand
  konvu remediate run abc-123

  # Trigger and wait until the job reaches a terminal state
  konvu remediate abc-123 --wait --timeout 15m

  # Check status without triggering
  konvu remediate status abc-123`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		return runRemediateTrigger(cmd, args)
	},
}

// Terminal remediation job statuses — polling stops once reached.
var remediateTerminalStatuses = map[string]bool{
	"succeeded": true,
	"failed":    true,
	"merged":    true,
	"closed":    true,
}

// resolveFindingTarget fetches a finding and extracts the
// (manifest_location_id, vulnerability_id) pair the remediation endpoints
// address, plus the detected SCM so error suggestions can be SCM-specific.
func resolveFindingTarget(client *api.Client, findingID string) (mlID, vulnID string, scm scmType, err error) {
	detail, err := client.Get(fmt.Sprintf("/sca_findings/%s", findingID), nil)
	if err != nil {
		if apiErr, ok := err.(*api.APIError); ok && apiErr.StatusCode == 404 {
			return "", "", scmUnknown, &clierrors.CLIError{
				Code:       "FINDING_NOT_FOUND",
				Message:    fmt.Sprintf("Finding '%s' not found", findingID),
				Suggestion: "Run 'konvu finding list' to see available findings.",
				ExitCode:   clierrors.ExitNotFound,
			}
		}
		return "", "", scmUnknown, err
	}
	ml := getMap(detail, "manifest_location")
	mlID = getStr(detail, "manifest_location_id")
	vulnID = getStr(detail, "vulnerability_id")
	if mlID == "" {
		mlID = getStr(ml, "id")
	}
	if vulnID == "" {
		vulnID = getStr(getMap(detail, "vulnerability"), "id")
	}
	if mlID == "" || vulnID == "" {
		return "", "", scmUnknown, &clierrors.CLIError{
			Code:     "FINDING_INCOMPLETE",
			Message:  fmt.Sprintf("Finding '%s' is missing manifest_location_id or vulnerability_id", findingID),
			ExitCode: clierrors.ExitGeneralError,
		}
	}
	return mlID, vulnID, detectSCM(ml), nil
}

// detectSCM derives the source control system from a finding's
// manifest_location. Prefers the typed `vcs_source` enum; falls back to the
// repository URL host.
func detectSCM(manifestLocation map[string]any) scmType {
	source := strings.ToLower(getStr(manifestLocation, "vcs_source"))
	switch {
	case strings.HasPrefix(source, "github"):
		return scmGitHub
	case strings.HasPrefix(source, "gitlab"):
		return scmGitLab
	}
	for _, key := range []string{"vcs_base_url", "vcs_repository_url"} {
		u := strings.ToLower(getStr(manifestLocation, key))
		switch {
		case strings.Contains(u, "github"):
			return scmGitHub
		case strings.Contains(u, "gitlab"):
			return scmGitLab
		}
	}
	return scmUnknown
}

// integrationsURL returns the dashboard's integrations page, where the user
// installs/configures both GitHub Autofix and GitLab remediation integrations.
func integrationsURL() string {
	return config.GetDashboardURL() + "/configuration/integrations"
}

// mapRemediateAPIError turns backend 422 detail codes into actionable
// CLIErrors. `scm` is the SCM detected from the finding so suggestions can
// be SCM-specific; pass scmUnknown when it isn't known yet.
func mapRemediateAPIError(err error, findingID string, scm scmType) *clierrors.CLIError {
	if cliErr, ok := err.(*clierrors.CLIError); ok {
		return cliErr
	}
	if _, ok := err.(*api.AuthenticationError); ok {
		return clierrors.NewAuthError(err.Error())
	}
	apiErr, ok := err.(*api.APIError)
	if !ok {
		return clierrors.NewAPIError(err.Error())
	}
	detail := extractAPIErrorDetail(apiErr.Message)

	// The gitlab-specific detail tells us SCM even when the finding didn't.
	if detail == "autofix_repo_not_covered_gitlab" {
		scm = scmGitLab
	} else if detail == "autofix_repo_not_covered" {
		scm = scmGitHub
	}

	switch {
	case apiErr.StatusCode == 404:
		return &clierrors.CLIError{
			Code:       "REMEDIATE_NO_INTEGRATION_ISSUE",
			Message:    fmt.Sprintf("No integration issue found for finding '%s'", findingID),
			Suggestion: "The finding has no linked VCS issue. Confirm the scanner has reported it via a Konvu-connected repository.",
			ExitCode:   clierrors.ExitNotFound,
		}
	case detail == "autofix_integration_missing":
		return &clierrors.CLIError{
			Code:       "REMEDIATE_INTEGRATION_MISSING",
			Message:    fmt.Sprintf("No remediation integration is installed for %s", scm.label()),
			Suggestion: installSuggestion(scm),
			ExitCode:   clierrors.ExitGeneralError,
		}
	case detail == "autofix_repo_not_covered" || detail == "autofix_repo_not_covered_gitlab":
		return &clierrors.CLIError{
			Code:       "REMEDIATE_REPO_NOT_COVERED",
			Message:    fmt.Sprintf("%s remediation integration is installed but does not cover this repository", scm.label()),
			Suggestion: coverageSuggestion(scm),
			ExitCode:   clierrors.ExitGeneralError,
		}
	default:
		return clierrors.NewAPIError(apiErr.Message)
	}
}

// installSuggestion returns the install-link text shown when no remediation
// integration is configured for the company.
func installSuggestion(scm scmType) string {
	url := integrationsURL()
	switch scm {
	case scmGitHub:
		return fmt.Sprintf("Install the Konvu Autofix GitHub App from %s, then retry.", url)
	case scmGitLab:
		return fmt.Sprintf("Install the Konvu GitLab remediation integration from %s, then retry.", url)
	default:
		return fmt.Sprintf("Install a remediation integration from %s (GitHub Autofix App or GitLab integration), then retry.", url)
	}
}

// coverageSuggestion returns the link text shown when the integration exists
// but doesn't cover the repository this finding lives in.
func coverageSuggestion(scm scmType) string {
	url := integrationsURL()
	switch scm {
	case scmGitHub:
		return fmt.Sprintf("Grant the Konvu Autofix GitHub App access to this repository (manage at %s), then retry.", url)
	case scmGitLab:
		return fmt.Sprintf("Add this repository to the Konvu GitLab remediation integration (manage at %s), then retry.", url)
	default:
		return fmt.Sprintf("Grant the remediation integration access to this repository (manage at %s), then retry.", url)
	}
}

// extractAPIErrorDetail pulls FastAPI's `detail` field out of an APIError
// message. APIError.Message looks like `API error: {"detail":"..."}`.
func extractAPIErrorDetail(msg string) string {
	idx := strings.Index(msg, "{")
	if idx < 0 {
		return ""
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(msg[idx:]), &body); err != nil {
		return ""
	}
	d, _ := body["detail"].(string)
	return d
}

func reportRemediateError(err *clierrors.CLIError, format output.OutputFormat) {
	if format == output.JSON {
		fmt.Println(clierrors.FormatErrorJSON(err))
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Message)
		if err.Suggestion != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", err.Suggestion)
		}
	}
	os.Exit(err.ExitCode)
}

// --- remediate run ---

var remediateRunCmd = &cobra.Command{
	Use:   "run [finding-id]",
	Short: "Trigger a remediation PR for a vulnerability finding",
	Long: `Trigger Konvu's remediation engine to open a remediation PR for a finding.

Remediation runs asynchronously inside the customer's on-prem controller
(patcheus engine). Use 'konvu remediate status' or '--wait' to track progress.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  # Trigger remediation for a finding
  konvu remediate run abc-123

  # Trigger and wait until the job reaches a terminal state
  konvu remediate run abc-123 --wait --timeout 15m

  # Include a source URL in the PR description (e.g. ticket link)
  konvu remediate run abc-123 --source-url https://linear.app/konvu/issue/SEC-42`,
	Args: cobra.ExactArgs(1),
	RunE: runRemediateTrigger,
}

func runRemediateTrigger(cmd *cobra.Command, args []string) error {
	findingID := args[0]
	sourceURL, _ := cmd.Flags().GetString("source-url")
	wait, _ := cmd.Flags().GetBool("wait")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	interval, _ := cmd.Flags().GetDuration("poll-interval")
	outputFlag, _ := cmd.Flags().GetString("output")
	format := output.DetectOutputFormat(outputFlag)

	client := api.NewClient("", "")
	defer client.Close()

	mlID, vulnID, scm, err := resolveFindingTarget(client, findingID)
	if err != nil {
		reportRemediateError(mapRemediateAPIError(err, findingID, scm), format)
		return nil
	}

	if format != output.JSON {
		fmt.Fprintf(os.Stderr, "Triggering remediation for %s (%s)...\n", findingID, vulnID)
	}

	params := url.Values{}
	params.Set("manifest_location_id", mlID)
	params.Set("vulnerability_id", vulnID)
	if sourceURL != "" {
		params.Set("source_url", sourceURL)
	}
	triggerPath := "/remediations/trigger?" + params.Encode()

	resp, err := client.Post(triggerPath, nil)
	if err != nil {
		reportRemediateError(mapRemediateAPIError(err, findingID, scm), format)
		return nil
	}

	success, _ := resp["success"].(bool)
	message := getStr(resp, "message")

	result := map[string]any{
		"finding_id":           findingID,
		"manifest_location_id": mlID,
		"vulnerability_id":     vulnID,
		"triggered":            success,
		"message":              message,
	}

	if wait {
		statusParams := url.Values{}
		statusParams.Set("manifest_location_id", mlID)
		statusParams.Set("vulnerability_id", vulnID)
		final, waitErr := pollRemediateStatus(client, statusParams, timeout, interval, format)
		if waitErr != nil {
			reportRemediateError(mapRemediateAPIError(waitErr, findingID, scm), format)
			return nil
		}
		result["status"] = final
	}

	if format == output.JSON {
		fmt.Println(output.FormatJSON(result))
	} else {
		if success {
			fmt.Printf("Remediation triggered: %s\n", message)
		} else {
			fmt.Printf("Remediation request rejected: %s\n", message)
		}
		if final, ok := result["status"].(map[string]any); ok {
			printRemediateStatusTable(final)
		}
	}
	return nil
}

// --- remediate status ---

var remediateStatusCmd = &cobra.Command{
	Use:   "status [finding-id]",
	Short: "Show the latest remediation job for a finding",
	Long: `Show the latest remediation job for a finding: lifecycle status, PR link, error.

Exit codes: 0 success, 1 general error, 2 invalid arguments, 3 not found, 4 auth failed`,
	Example: `  konvu remediate status abc-123
  konvu remediate status abc-123 --output json
  konvu remediate status abc-123 --wait`,
	Args: cobra.ExactArgs(1),
	RunE: runRemediateStatus,
}

func runRemediateStatus(cmd *cobra.Command, args []string) error {
	findingID := args[0]
	wait, _ := cmd.Flags().GetBool("wait")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	interval, _ := cmd.Flags().GetDuration("poll-interval")
	outputFlag, _ := cmd.Flags().GetString("output")
	format := output.DetectOutputFormat(outputFlag)

	client := api.NewClient("", "")
	defer client.Close()

	mlID, vulnID, scm, err := resolveFindingTarget(client, findingID)
	if err != nil {
		reportRemediateError(mapRemediateAPIError(err, findingID, scm), format)
		return nil
	}

	params := url.Values{}
	params.Set("manifest_location_id", mlID)
	params.Set("vulnerability_id", vulnID)

	var status map[string]any
	if wait {
		status, err = pollRemediateStatus(client, params, timeout, interval, format)
	} else {
		status, err = fetchRemediateStatus(client, params)
	}
	if err != nil {
		reportRemediateError(mapRemediateAPIError(err, findingID, scm), format)
		return nil
	}

	if status == nil {
		// No remediation job has ever been triggered for this finding.
		empty := map[string]any{
			"finding_id":           findingID,
			"manifest_location_id": mlID,
			"vulnerability_id":     vulnID,
			"status":               nil,
		}
		if format == output.JSON {
			fmt.Println(output.FormatJSON(empty))
		} else {
			fmt.Printf("No remediation job exists for finding %s.\n", findingID)
			fmt.Println("Run 'konvu remediate run' to trigger one.")
		}
		return nil
	}

	result := map[string]any{
		"finding_id":           findingID,
		"manifest_location_id": mlID,
		"vulnerability_id":     vulnID,
		"status":               status,
	}
	if format == output.JSON {
		fmt.Println(output.FormatJSON(result))
	} else {
		printRemediateStatusTable(status)
	}
	return nil
}

// fetchRemediateStatus calls /remediations/status. The endpoint returns 200
// with a JSON null body when no job exists; we surface that as nil.
func fetchRemediateStatus(client *api.Client, params url.Values) (map[string]any, error) {
	p := map[string]any{
		"manifest_location_id": params.Get("manifest_location_id"),
		"vulnerability_id":     params.Get("vulnerability_id"),
	}
	resp, err := client.Get("/remediations/status", p)
	if err != nil {
		// The API returns a literal `null` body when no job exists, which
		// the JSON decoder can't unmarshal into map[string]any — surface
		// this as "no status" rather than an error.
		if strings.Contains(err.Error(), "cannot unmarshal") {
			return nil, nil
		}
		return nil, err
	}
	if len(resp) == 0 {
		return nil, nil
	}
	return resp, nil
}

func pollRemediateStatus(client *api.Client, params url.Values, timeout, interval time.Duration, format output.OutputFormat) (map[string]any, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		status, err := fetchRemediateStatus(client, params)
		if err != nil {
			return nil, err
		}
		if status != nil {
			s := getStr(status, "status")
			if format != output.JSON {
				fmt.Fprintf(os.Stderr, "  status: %s\n", s)
			}
			if remediateTerminalStatuses[s] {
				return status, nil
			}
		} else if format != output.JSON {
			fmt.Fprintln(os.Stderr, "  status: not yet visible")
		}
		if time.Now().After(deadline) {
			return status, &clierrors.CLIError{
				Code:       "REMEDIATE_WAIT_TIMEOUT",
				Message:    fmt.Sprintf("Timed out after %s waiting for remediation to reach a terminal status", timeout),
				Suggestion: "Re-run 'konvu remediate status' later, or increase --timeout.",
				ExitCode:   clierrors.ExitGeneralError,
			}
		}
		time.Sleep(interval)
	}
}

func printRemediateStatusTable(status map[string]any) {
	fmt.Println()
	fmt.Println("Remediation Job")
	fmt.Println(strings.Repeat("=", 40))
	fmt.Printf("  status:     %s\n", getStr(status, "status"))
	if pr := getStr(status, "pr_link"); pr != "" {
		fmt.Printf("  pr_link:    %s\n", pr)
	} else if pr := getStr(status, "pr_url"); pr != "" {
		fmt.Printf("  pr_link:    %s\n", pr)
	}
	if created := getStr(status, "created_at"); created != "" {
		fmt.Printf("  created_at: %s\n", created)
	}
	if msg := getStr(status, "error_message"); msg != "" {
		fmt.Printf("  error:      %s\n", msg)
	}
}

func addRemediateTriggerFlags(cmd *cobra.Command) {
	cmd.Flags().String("source-url", "", "URL to include in the PR description (e.g., ticket link)")
	cmd.Flags().Bool("wait", false, "Poll until the remediation job reaches a terminal state")
	cmd.Flags().Duration("timeout", 10*time.Minute, "Maximum time to wait when --wait is set")
	cmd.Flags().Duration("poll-interval", 5*time.Second, "Polling interval when --wait is set")
	cmd.Flags().StringP("output", "o", "", "Output format: json, table")
}

func init() {
	addRemediateTriggerFlags(remediateRunCmd)
	addRemediateTriggerFlags(remediateCmd) // shared by the top-level alias `konvu remediate <id>`

	remediateStatusCmd.Flags().Bool("wait", false, "Poll until the remediation job reaches a terminal state")
	remediateStatusCmd.Flags().Duration("timeout", 10*time.Minute, "Maximum time to wait when --wait is set")
	remediateStatusCmd.Flags().Duration("poll-interval", 5*time.Second, "Polling interval when --wait is set")
	remediateStatusCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	remediateCmd.AddCommand(remediateRunCmd)
	remediateCmd.AddCommand(remediateStatusCmd)
	rootCmd.AddCommand(remediateCmd)
}
