package cmd

import (
	"fmt"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

func newFindingDismissCommand(kind, idName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "dismiss [" + idName + "]",
		Aliases: []string{"close"},
		Short:   "Dismiss a " + strings.ToUpper(kind) + " finding",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFindingDisposition(cmd, args[0], kind, "dismiss")
		},
	}
	cmd.Example = fmt.Sprintf(`  konvu finding %s dismiss <%s> \
    --reason "Tracked externally" \
    --comment "Handled by AppSec" \
    --external-reference "SEC-1234"`, kind, idName)
	cmd.Flags().String("reason", "Dismissed via Konvu CLI", "Reason for dismissal")
	cmd.Flags().StringP("comment", "c", "", "Additional context for the dismissal")
	cmd.Flags().String("external-reference", "", "External tracking reference, such as a Jira ticket")
	cmd.Flags().Bool("dry-run", false, "Show what would be sent, without sending")
	cmd.Flags().StringP("output", "o", "", "Output format: json, table")
	return cmd
}

func newFindingReopenCommand(kind, idName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reopen [" + idName + "]",
		Short: "Reopen a dismissed " + strings.ToUpper(kind) + " finding",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFindingDisposition(cmd, args[0], kind, "reopen")
		},
	}
	cmd.Example = fmt.Sprintf("  konvu finding %s reopen <%s>", kind, idName)
	cmd.Flags().Bool("dry-run", false, "Show what would be sent, without sending")
	cmd.Flags().StringP("output", "o", "", "Output format: json, table")
	return cmd
}

func runFindingDisposition(cmd *cobra.Command, id, kind, action string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return &clierrors.CLIError{
			Code:       "INVALID_ARGUMENTS",
			Message:    "finding ID cannot be blank",
			Suggestion: "Pass an ID from the corresponding finding list command.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}

	result := map[string]any{"type": kind, "id": id, "action": action}
	var payload map[string]any
	if action == "dismiss" {
		reason, _ := cmd.Flags().GetString("reason")
		comment, _ := cmd.Flags().GetString("comment")
		externalReference, _ := cmd.Flags().GetString("external-reference")
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return &clierrors.CLIError{
				Code:       "INVALID_ARGUMENTS",
				Message:    "--reason cannot be blank",
				Suggestion: "Explain why the finding is being dismissed.",
				ExitCode:   clierrors.ExitUsageError,
			}
		}
		payload = map[string]any{
			"dismissed_reason":        reason,
			"dismissed_comment":       strings.TrimSpace(comment),
			"external_reference_code": strings.TrimSpace(externalReference),
		}
		result["reason"] = payload["dismissed_reason"]
		result["comment"] = payload["dismissed_comment"]
		result["external_reference_code"] = payload["external_reference_code"]
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		result["dry_run"] = true
		result["status"] = "preview"
		return renderFindingDisposition(cmd, format, result)
	}

	client := api.NewClient("", "")
	defer client.Close()
	response, err := executeFindingDisposition(client, id, kind, action, payload)
	if err != nil {
		return findingDispositionError(kind, action, id, err)
	}
	for key, value := range response {
		result[key] = value
	}
	result["status"] = "completed"
	if getStr(response, "broker_task_id") != "" {
		result["status"] = "queued"
	}
	return renderFindingDisposition(cmd, format, result)
}

func executeFindingDisposition(
	client *api.Client, id, kind, action string, payload map[string]any,
) (map[string]any, error) {
	if kind == "sast" {
		return client.Post(fmt.Sprintf("/detections/%s/%s", id, action), payload)
	}
	if action == "dismiss" {
		body := map[string]any{
			"finding_ids":             []string{id},
			"dismissed_reason":        payload["dismissed_reason"],
			"dismissed_comment":       payload["dismissed_comment"],
			"external_reference_code": payload["external_reference_code"],
		}
		response, err := client.Post("/sca_findings/bulk_dismiss", body)
		if err != nil {
			return nil, err
		}
		if count, _ := getFloat(response, "dismissed_count"); count != 1 {
			return nil, &clierrors.CLIError{
				Code:       "SCA_DISMISS_FAILED",
				Message:    "the SCA finding was not dismissed",
				Suggestion: "Check that the finding is open and its scanner supports dismissal.",
				ExitCode:   clierrors.ExitGeneralError,
			}
		}
		return response, nil
	}

	detail, err := client.Get("/sca_findings/"+id, nil)
	if err != nil {
		return nil, err
	}
	source := getMap(detail, "source")
	integrationID, issueID := getStr(source, "integration_id"), getStr(source, "id")
	if integrationID == "" || issueID == "" {
		return nil, &clierrors.CLIError{
			Code:       "INVALID_API_RESPONSE",
			Message:    "the SCA finding is missing its source identifiers",
			Suggestion: "Retry after the finding has been synchronized from its scanner.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	response, err := client.Post(
		fmt.Sprintf("/integrations/%s/issue/%s/reopen", integrationID, issueID), nil,
	)
	if response == nil {
		response = map[string]any{}
	}
	response["integration_id"] = integrationID
	response["source_id"] = issueID
	return response, err
}

func findingDispositionOutputFormat(cmd *cobra.Command) (output.OutputFormat, error) {
	explicit, _ := cmd.Flags().GetString("output")
	explicit = strings.ToLower(strings.TrimSpace(explicit))
	if explicit != "" && explicit != "json" && explicit != "table" {
		return output.JSON, &clierrors.CLIError{
			Code:       "INVALID_ARGUMENTS",
			Message:    fmt.Sprintf("unsupported output format %q", explicit),
			Suggestion: "Use --output json or --output table.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	return output.DetectOutputFormat(explicit), nil
}

func findingDispositionError(kind, action, id string, err error) error {
	if cliErr, ok := err.(*clierrors.CLIError); ok {
		return cliErr
	}
	if _, ok := err.(*api.AuthenticationError); ok {
		return clierrors.NewAuthError(err.Error())
	}
	if apiErr, ok := err.(*api.APIError); ok && apiErr.StatusCode == 404 {
		return &clierrors.CLIError{
			Code:       strings.ToUpper(kind) + "_FINDING_NOT_FOUND",
			Message:    fmt.Sprintf("%s finding %q not found", strings.ToUpper(kind), id),
			Suggestion: "Run the corresponding finding list command to see available IDs.",
			ExitCode:   clierrors.ExitNotFound,
		}
	}
	return &clierrors.CLIError{
		Code:       strings.ToUpper(kind) + "_" + strings.ToUpper(action) + "_FAILED",
		Message:    fmt.Sprintf("could not %s the %s finding: %v", action, strings.ToUpper(kind), err),
		Suggestion: "Check the finding state and scanner dismissal support, then retry.",
		Retryable:  true,
		ExitCode:   clierrors.ExitGeneralError,
	}
}

func renderFindingDisposition(
	cmd *cobra.Command, format output.OutputFormat, result map[string]any,
) error {
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(result)+"\n")
	}
	columns := []string{"id", "status"}
	if getStr(result, "action") == "dismiss" {
		columns = append(columns, "comment", "external_reference_code")
	}
	return output.WriteString(cmd.OutOrStdout(), output.FormatTable(
		map[string]any{"findings": []any{result}}, columns, "findings", nil,
	))
}

func init() {
	scaCmd.AddCommand(
		newFindingDismissCommand("sca", "finding-id"),
		newFindingReopenCommand("sca", "finding-id"),
	)
	sastCmd.AddCommand(
		newFindingDismissCommand("sast", "detection-id"),
		newFindingReopenCommand("sast", "detection-id"),
	)
}
