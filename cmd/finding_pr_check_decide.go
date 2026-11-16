package cmd

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/findings"
	"github.com/spf13/cobra"
)

// The verdict endpoint the dashboard uses; the server re-checks the run is still the PR's latest.
const prCheckDecisionsPath = prCheckBase + "/pull-requests/%s/%d/findings/%s/decisions"

// The server refuses longer reasons.
const prCheckReasonMaxLength = 1000

// prCheckVerb is a verdict command, named as in the /konvu PR comment commands.
type prCheckVerb string

const (
	prCheckVerbDismiss    prCheckVerb = "dismiss"
	prCheckVerbAcceptRisk prCheckVerb = "accept-risk"
)

// What the server stores for each verdict command.
var prCheckDecisionActions = map[prCheckVerb]string{
	prCheckVerbDismiss:    "dismissed",
	prCheckVerbAcceptRisk: "risk_accepted",
}

var prCheckDecisionShorts = map[prCheckVerb]string{
	prCheckVerbDismiss:    "Dismiss a PR check finding: not a real flaw",
	prCheckVerbAcceptRisk: "Accept the risk of a PR check finding: real, but accepted",
}

func newPRCheckDecisionCommand(verb prCheckVerb) *cobra.Command {
	cmd := &cobra.Command{
		Use:   string(verb) + " <pr-url> <finding-id>",
		Short: prCheckDecisionShorts[verb],
		Long: prCheckDecisionShorts[verb] + `.

The finding id comes from 'konvu finding pr-check get <pr-url>'. The verdict is
recorded on the PR's latest review under your name and shows in the dashboard.
Later pushes to the PR do not report it again.`,
		Example: fmt.Sprintf(`  konvu finding pr-check %s https://github.com/acme/app/pull/42 run-1:2 \
    --reason "Admin-only endpoint; admins can already read every workspace"`, verb),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPRCheckDecision(cmd, verb, args[0], args[1])
		},
	}
	cmd.Flags().String("reason", "", fmt.Sprintf("Why (required, up to %d characters)", prCheckReasonMaxLength))
	cmd.Flags().Bool("dry-run", false, "Check the finding and show what would be sent, without sending")
	cmd.Flags().StringP("output", "o", "", "Output format: json (default)")
	return cmd
}

func runPRCheckDecision(cmd *cobra.Command, verb prCheckVerb, prURL, findingID string) error {
	if err := findings.RequireJSON(cmd, "pr-check "+string(verb)); err != nil {
		return err
	}

	// Validate locally first: the server answers a bad reason with a bare 422.
	reason, _ := cmd.Flags().GetString("reason")
	reason = strings.TrimSpace(reason)
	// Characters, not bytes, as the server counts them: "é" is one.
	if reason == "" || utf8.RuneCountInString(reason) > prCheckReasonMaxLength {
		return prCheckUsageError(
			fmt.Sprintf("--reason must be 1 to %d characters", prCheckReasonMaxLength),
			`Example: --reason "Admin-only endpoint"`,
		)
	}
	findingID = strings.TrimSpace(findingID)

	client := api.NewClient("", "")
	defer client.Close()

	// The verdict is pinned to the run the PR is judged on, so a newer push is never decided blind.
	review, repositoryID, err := prCheckLoad(client, prURL)
	if err != nil {
		return err
	}
	if !prCheckHasFinding(review, findingID) {
		return &clierrors.CLIError{
			Code:       "PR_CHECK_FINDING_NOT_FOUND",
			Message:    fmt.Sprintf("finding %q is not in the latest review of %s", findingID, prURL),
			Suggestion: "Copy a finding_id from 'konvu finding pr-check get " + prURL + "'.",
			ExitCode:   clierrors.ExitNotFound,
		}
	}

	pr, _ := review["pr"].(map[string]any)
	result := findings.Row{
		"pr":         getStr(pr, "url"),
		"finding_id": findingID,
		"action":     prCheckDecisionActions[verb],
		"reason":     reason,
		"run_id":     review["run_id"],
	}
	if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
		result["status"] = "preview"
		return findings.Render(cmd, []findings.Row{result}, nil)
	}

	// prCheckLoad already validated the URL.
	number, _ := strconv.Atoi(prURLRe.FindStringSubmatch(strings.TrimSpace(prURL))[2])
	path := fmt.Sprintf(prCheckDecisionsPath, repositoryID, number, findingID)
	saved, err := client.Post(path, map[string]any{
		"action": prCheckDecisionActions[verb],
		"reason": reason,
		"run_id": review["run_id"],
	})
	if err != nil {
		return prCheckDecisionError(err, prURL)
	}

	// The server answers with the stored decision: actor, time, and an earlier verdict if one existed.
	for key, value := range saved {
		result[key] = value
	}
	result["status"] = "recorded"
	return findings.Render(cmd, []findings.Row{result}, nil)
}

// prCheckHasFinding reports whether the review's judged run lists the finding.
func prCheckHasFinding(review findings.Row, findingID string) bool {
	flaws, _ := review["findings"].([]any)
	for _, f := range flaws {
		flaw, ok := f.(map[string]any)
		if !ok {
			continue
		}
		if getStr(flaw, "finding_id") == findingID {
			return true
		}
	}
	return false
}

// prCheckDecisionsByFinding indexes a run's recorded verdicts by finding id.
func prCheckDecisionsByFinding(run map[string]any) map[string]any {
	byFinding := map[string]any{}
	for _, d := range getSlice(run, "decisions") {
		decision, ok := d.(map[string]any)
		if !ok {
			continue
		}
		byFinding[getStr(decision, "finding_id")] = map[string]any{
			"action":     decision["action"],
			"reason":     decision["reason"],
			"actor":      decision["actor"],
			"created_at": decision["created_at"],
		}
	}
	return byFinding
}

// prCheckDecisionError turns the server's refusals into next steps.
func prCheckDecisionError(err error, prURL string) error {
	apiErr, ok := err.(*api.APIError)
	if !ok {
		return prCheckRequestError(err)
	}

	switch apiErr.StatusCode {
	case http.StatusConflict:
		// The PR got a new push or review since it was read.
		return &clierrors.CLIError{
			Code:       "PR_CHECK_CHANGED",
			Message:    fmt.Sprintf("the review of %s changed: %v", prURL, err),
			Suggestion: "Re-read it with 'konvu finding pr-check get " + prURL + "' and decide on the new findings.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	case http.StatusNotFound:
		return &clierrors.CLIError{
			Code:       "PR_CHECK_FINDING_NOT_FOUND",
			Message:    err.Error(),
			Suggestion: "Copy a finding_id from 'konvu finding pr-check get " + prURL + "'.",
			ExitCode:   clierrors.ExitNotFound,
		}
	case http.StatusForbidden:
		// Verdicts carry a person's name; API keys and service tokens have none.
		return &clierrors.CLIError{
			Code:       "PR_CHECK_DECISION_FORBIDDEN",
			Message:    err.Error(),
			Suggestion: "Unset KONVU_ACCESS_TOKEN if set, then log in as yourself with 'konvu login' (OAuth), not an API key.",
			ExitCode:   clierrors.ExitAuthFailed,
		}
	}
	return prCheckRequestError(err)
}
