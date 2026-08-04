package cmd

import (
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/config"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var Version = "dev" // overridden at build time via ldflags

// resolveVersion returns the build-time version, falling back to the Go module
// version stamped into the binary when no ldflags were supplied (for example
// `go install github.com/KonvuInc/konvu-cli/cmd/konvu@v0.7.0`).
func resolveVersion() string {
	if Version != "dev" {
		return Version
	}

	// Order matters: info is nil unless ok, so the short-circuit guards it.
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return Version
	}

	return strings.TrimPrefix(info.Main.Version, "v")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show CLI version",
	Run: func(cmd *cobra.Command, args []string) {
		outputFlag, _ := cmd.Flags().GetString("output")
		format := output.DetectOutputFormat(outputFlag)

		resolved := resolveVersion()

		if format == output.JSON {
			data := map[string]string{
				"version": resolved,
				"api_url": config.GetAPIBaseURL(),
			}
			b, _ := json.MarshalIndent(data, "", "  ")
			fmt.Println(string(b))
		} else {
			fmt.Printf("konvu-cli %s (api: %s)\n", resolved, config.GetAPIBaseURL())
		}
	},
}

func init() {
	versionCmd.Flags().StringP("output", "o", "", "Output format: json, text")
	rootCmd.AddCommand(versionCmd)
}
