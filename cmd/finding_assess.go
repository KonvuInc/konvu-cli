package cmd

import (
	"fmt"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

func newFindingAssessCommand(kind, resource, idName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assess <" + idName + ">",
		Short: "Request an assessment of one " + kind + " finding",
		Args:  cobra.ExactArgs(1),
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
				handleFindingError(err, format)
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

func init() {
	scaCmd.AddCommand(newFindingAssessCommand("SCA", "sca_findings", "finding-id"))
	sastCmd.AddCommand(newFindingAssessCommand("SAST", "detections", "detection-id"))
	findingCmd.AddCommand(newFindingAssessCommand("SCA", "sca_findings", "finding-id"))
}
