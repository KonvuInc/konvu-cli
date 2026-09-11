package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/KonvuInc/konvu-cli/pkg/api"
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

	client := api.NewClient("", "")
	response, err := client.Post("/agent_telemetry/batches", payload)
	if err != nil {
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(response)
}

func init() {
	telemetryCmd.AddCommand(telemetryUploadCmd)
	rootCmd.AddCommand(telemetryCmd)
}
