package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

func newFindingAssessCommand(kind, resource, idName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assess <" + idName + ">",
		Short: "Request an assessment of one " + kind + " finding",
		Long: `Request a new assessment of one ` + kind + ` finding. Konvu assesses open findings
on repositories a coverage scope runs analysis on: 'konvu coverage list' shows them.

Exit codes: 0 requested, 1 a finding Konvu will not assess or another error,
2 invalid arguments, 4 auth failed`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			formatFlag, _ := cmd.Flags().GetString("output")
			format := output.DetectOutputFormat(formatFlag)
			if formatFlag != "" && formatFlag != "json" && formatFlag != "table" {
				handleFindingError(&clierrors.CLIError{
					Code: "INVALID_OUTPUT", Message: "Unsupported output format",
					Suggestion: "Use -o json or -o table.", ExitCode: clierrors.ExitUsageError,
				}, format)
			}
			client := api.NewClient("", "")
			defer client.Close()
			response, err := client.Post(fmt.Sprintf("/%s/%s/trigger_assessment", resource, args[0]), nil)
			if err != nil {
				handleFindingError(assessError(err), format)
			}
			if format == output.JSON {
				return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(response)+"\n")
			}
			return output.WriteString(cmd.OutOrStdout(), "Assessment requested.\n")
		},
	}
	cmd.Flags().StringP("output", "o", "", "Output format: json, table")
	return cmd
}

// notCoveredDetail is how Konvu says it does not assess a finding's repository: "Assessment is
// disabled." before it named coverage, then a message about the repository's coverage.
var notCoveredDetail = regexp.MustCompile(`(?i)assessment is disabled|\bnot covered\b|\bcoverage\b|\b(assessment|coverage) scope\b`)

// assessError maps a refused assessment request. A finding Konvu will not assess (a 422) is no
// error to retry: one on a repository no scope analyzes points at 'konvu coverage'.
func assessError(err error) error {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity {
		return err
	}
	detail := invariantDetail(apiErr)
	if notCoveredDetail.MatchString(detail) {
		return &clierrors.CLIError{
			Code:       "NOT_COVERED",
			Message:    "Konvu will not assess this finding (" + detail + ")",
			Suggestion: "Run 'konvu coverage list' to see the scopes that run analysis, and 'konvu coverage add <scope> <repo>' to cover the repository.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return &clierrors.CLIError{
		Code:       "NOT_ASSESSABLE",
		Message:    orDefault(detail, "Konvu cannot assess this finding"),
		Suggestion: "Only open findings are assessed: check the finding's state with its 'get'.",
		ExitCode:   clierrors.ExitGeneralError,
	}
}

func init() {
	scaCmd.AddCommand(newFindingAssessCommand("SCA", "sca_findings", "finding-id"))
	sastCmd.AddCommand(newFindingAssessCommand("SAST", "detections", "detection-id"))
	findingCmd.AddCommand(newFindingAssessCommand("SCA", "sca_findings", "finding-id"))
}
