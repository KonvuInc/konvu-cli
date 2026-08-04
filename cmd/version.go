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

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Version
	}

	if moduleVersion := normalizeModuleVersion(info.Main.Version); moduleVersion != "" {
		return moduleVersion
	}

	return Version
}

// normalizeModuleVersion turns the module version Go stamps into a binary into a
// display version, returning "" when Go recorded nothing usable. Kept separate
// from resolveVersion because debug.ReadBuildInfo cannot be stubbed in tests.
func normalizeModuleVersion(moduleVersion string) string {
	if moduleVersion == "" || moduleVersion == "(devel)" {
		return ""
	}

	return strings.TrimPrefix(moduleVersion, "v")
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
