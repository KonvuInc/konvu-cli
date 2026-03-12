package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/KonvuTeam/konvu-cli/internal/config"
	"github.com/KonvuTeam/konvu-cli/internal/output"
	"github.com/spf13/cobra"
)

var Version = "dev" // overridden by goreleaser via ldflags

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show CLI version",
	Run: func(cmd *cobra.Command, args []string) {
		outputFlag, _ := cmd.Flags().GetString("output")
		format := output.DetectOutputFormat(outputFlag)

		if format == output.JSON {
			data := map[string]string{
				"version": Version,
				"api_url": config.GetAPIBaseURL(),
			}
			b, _ := json.MarshalIndent(data, "", "  ")
			fmt.Println(string(b))
		} else {
			fmt.Printf("konvu-cli %s (api: %s)\n", Version, config.GetAPIBaseURL())
		}
	},
}

func init() {
	versionCmd.Flags().StringP("output", "o", "", "Output format: json, text")
	rootCmd.AddCommand(versionCmd)
}
