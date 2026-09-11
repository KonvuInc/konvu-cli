package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/spf13/cobra"
)

var telemetryCmd = &cobra.Command{
	Use:   "telemetry",
	Short: "Upload agent telemetry collected locally",
}

var telemetryUploadCmd = &cobra.Command{
	Use:   "upload <file>",
	Short: "Upload one metadata-only telemetry batch",
	Args:  cobra.ExactArgs(1),
	RunE:  runTelemetryUpload,
}

func runTelemetryUpload(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read telemetry file: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("parse telemetry JSON: %w", err)
	}
	if payload == nil {
		return fmt.Errorf("parse telemetry JSON: expected an object")
	}

	client := api.NewClient("", "")
	response, err := client.Post("/agent_telemetry/batches", payload)
	if err != nil {
		if _, ok := err.(*api.AuthenticationError); ok {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(clierrors.ExitAuthFailed)
		}
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(response)
}

func init() {
	telemetryCmd.AddCommand(telemetryUploadCmd)
	rootCmd.AddCommand(telemetryCmd)
}
