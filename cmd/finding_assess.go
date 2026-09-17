package cmd

import (
	"fmt"
	"time"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

func newFindingAssessmentCommand(kind string, trigger bool) *cobra.Command {
	name, short := "status", "Show assessment execution progress"
	if trigger {
		name, short = "assess", "Request an assessment of one finding"
	}
	cmd := &cobra.Command{
		Use: name + " <finding-ref>", Short: short, Args: cobra.ExactArgs(1),
		Long: short + `. States are queued, assessing, done, failed, or not_started.

SCA accepts a Konvu finding ID or the Dependabot references supported by finding get.
SAST accepts a detection ID, stable SAST finding ID, or investigation ID. Use
detection_id from 'finding sast list' for a finding that has never been assessed.
Queued retries remain queued. The assessment verdict is separate from execution status.
Normal assessment coverage and credit limits apply. --watch polls until done or failed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFindingAssessment(cmd, args[0], kind, trigger)
		},
	}
	cmd.Flags().StringP("output", "o", "", "Output format: json, table")
	cmd.Flags().Bool("watch", false, "Follow execution until done or failed")
	cmd.Flags().Duration("timeout", 15*time.Minute, "Maximum time to watch")
	cmd.Flags().Duration("interval", 3*time.Second, "Polling interval when watching")
	return cmd
}

func runFindingAssessment(cmd *cobra.Command, reference, kind string, trigger bool) error {
	formatFlag, _ := cmd.Flags().GetString("output")
	if formatFlag != "" && formatFlag != "json" && formatFlag != "table" {
		return &clierrors.CLIError{Code: "INVALID_OUTPUT", Message: "Unsupported output format", Suggestion: "Use -o json or -o table.", ExitCode: clierrors.ExitUsageError}
	}
	format := output.DetectOutputFormat(formatFlag)
	watch, _ := cmd.Flags().GetBool("watch")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	interval, _ := cmd.Flags().GetDuration("interval")
	if timeout <= 0 || interval <= 0 {
		return &clierrors.CLIError{Code: "INVALID_DURATION", Message: "Timeout and interval must be positive", Suggestion: "Use --timeout 15m --interval 3s.", ExitCode: clierrors.ExitUsageError}
	}
	// Runtime failures must not append Cobra usage to an already-written JSON
	// document (in particular, a failed assessment or a watch timeout).
	cmd.SilenceUsage = true
	client := api.NewClient("", "")
	defer client.Close()
	id := reference
	if !isFindingID(id) {
		if kind == "sast" {
			return &clierrors.CLIError{Code: "INVALID_FINDING", Message: "SAST requires a Konvu ID", Suggestion: "Use detection_id or id from 'konvu finding sast list -o json'.", ExitCode: clierrors.ExitUsageError}
		}
		var err error
		id, err = resolveFindingReference(client, reference)
		if err != nil {
			return mapAssessmentError(err)
		}
	}
	resource := "/sca_findings/"
	if kind == "sast" {
		resource = "/detections/"
	}
	path := resource + id
	var state map[string]any
	var err error
	if trigger {
		state, err = client.Post(path+"/trigger_assessment", nil)
		if apiErr, ok := err.(*api.APIError); ok && apiErr.StatusCode == 409 {
			// An existing queued/running assessment is an idempotent success.
			state, err = client.Get(path+"/assessment_execution", nil)
			if err == nil && getStr(state, "status") != "queued" && getStr(state, "status") != "assessing" {
				return mapAssessmentError(apiErr)
			}
		}
		if err == nil && getStr(state, "status") == "triggered" {
			state = getMap(state, "execution")
			if len(state) == 0 {
				state, err = client.Get(path+"/assessment_execution", nil)
			}
		}
	} else {
		state, err = client.Get(path+"/assessment_execution", nil)
	}
	if err != nil {
		return mapAssessmentError(err)
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	previous := ""
	for {
		status := getStr(state, "status")
		switch status {
		case "not_started", "queued", "assessing", "done", "failed":
		default:
			return &clierrors.CLIError{Code: "INVALID_RESPONSE", Message: "Server returned no recognized assessment state", Suggestion: "Check the finding in the dashboard and update the CLI/server."}
		}
		state["finding_id"], state["kind"] = id, kind
		terminal := status == "done" || status == "failed" || status == "not_started"
		// JSON remains one parseable document, even with --watch.
		if format == output.JSON {
			if !watch || terminal {
				if err := output.WriteString(cmd.OutOrStdout(), output.FormatJSON(state)+"\n"); err != nil {
					return err
				}
			}
		} else if line := fmt.Sprintf("%s: %s\n", status, getStr(state, "message")); line != previous {
			if err := output.WriteString(cmd.OutOrStdout(), line); err != nil {
				return err
			}
			previous = line
		}
		if !watch || terminal {
			if watch && status == "failed" {
				return &clierrors.CLIError{Code: "ASSESSMENT_FAILED", Message: "Assessment failed", Suggestion: "Inspect the finding, then request another assessment when ready."}
			}
			return nil
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-deadline.C:
			if format == output.JSON {
				_ = output.WriteString(cmd.OutOrStdout(), output.FormatJSON(state)+"\n")
			}
			return &clierrors.CLIError{Code: "ASSESSMENT_TIMEOUT", Message: "Stopped watching; the assessment continues", Suggestion: fmt.Sprintf("Run 'konvu finding %s status %s'.", kind, id), Retryable: true}
		case <-ticker.C:
		}
		state, err = client.Get(path+"/assessment_execution", nil)
		if err != nil {
			return mapAssessmentError(err)
		}
	}
}

func mapAssessmentError(err error) error {
	if _, ok := err.(*api.AuthenticationError); ok {
		return clierrors.NewAuthError(err.Error())
	}
	if _, ok := err.(*clierrors.CLIError); ok {
		return err
	}
	if apiErr, ok := err.(*api.APIError); ok {
		suggestion := "Check the finding in the dashboard."
		switch apiErr.StatusCode {
		case 402:
			suggestion = "Check your organization's available assessment credits."
		case 403:
			suggestion = "Ask your organization administrator for assessment access."
		case 404:
			suggestion = "Use a finding ID from 'konvu finding list' or 'konvu finding sast list'."
		case 422:
			suggestion = "Check that the finding is open and its repository is enabled for assessment."
		}
		return &clierrors.CLIError{Code: "ASSESSMENT_REQUEST_FAILED", Message: extractAPIErrorDetail(apiErr.Message), Suggestion: suggestion, Retryable: apiErr.StatusCode >= 500, ExitCode: clierrors.ExitGeneralError}
	}
	return clierrors.NewAPIError(err.Error())
}

func init() {
	for _, trigger := range []bool{true, false} {
		scaCmd.AddCommand(newFindingAssessmentCommand("sca", trigger))
		sastCmd.AddCommand(newFindingAssessmentCommand("sast", trigger))
		findingCmd.AddCommand(newFindingAssessmentCommand("sca", trigger))
	}
}
