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

// Named so that changing it cannot silently disable the fallback below.
const devVersion = "dev"

var Version = devVersion // overridden at build time via ldflags

// resolveVersion returns the build-time version, falling back to the Go module
// version stamped into the binary when no ldflags were supplied (for example
// `go install github.com/KonvuInc/konvu-cli/cmd/konvu@v0.7.0`). It never reports
// a version that was not released.
func resolveVersion() string {
	// A malformed `-X` sets an empty value, which is not a version.
	if Version != devVersion && Version != "" {
		return Version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return devVersion
	}

	// A checkout build records vcs.* settings; a proxy install records none.
	// Since Go 1.24 the former stamps Main.Version with a pseudo-version, which
	// would name a release that never existed.
	var revision, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}

	if revision == "" {
		if moduleVersion := normalizeModuleVersion(info.Main.Version); moduleVersion != "" {
			return moduleVersion
		}
		return devVersion
	}

	return devBuildVersion(revision, modified == "true")
}

// devBuildVersion labels a checkout build: "dev+9c1a51e", "-dirty" if modified.
func devBuildVersion(revision string, dirty bool) string {
	const shortLen = 7
	if len(revision) > shortLen {
		revision = revision[:shortLen]
	}

	version := devVersion + "+" + revision
	if dirty {
		version += "-dirty"
	}
	return version
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

	// Setting Version makes Cobra honour --version. Registered here rather than
	// left to Cobra so it does not also claim -v, which is --verbose elsewhere.
	rootCmd.Version = resolveVersion()
	rootCmd.Flags().Bool("version", false, "version for konvu")
	rootCmd.SetVersionTemplate("konvu-cli {{.Version}}\n")
}
