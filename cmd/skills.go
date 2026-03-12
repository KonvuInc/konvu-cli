package cmd

import (
	"fmt"
	"os"

	"github.com/KonvuTeam/konvu-cli/internal/skills"
	"github.com/spf13/cobra"
)

var skillsForceFlag bool

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage bundled Claude Code skills",
}

var skillsPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the path to the skills directory",
	Run: func(cmd *cobra.Command, args []string) {
		dir, err := skills.InstallDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		fmt.Fprintln(cmd.OutOrStdout(), dir)
	},
}

var skillsInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install bundled skills to ~/.agents/skills/",
	Run: func(cmd *cobra.Command, args []string) {
		RunSkillsInstall(skillsForceFlag, true)
	},
}

// RunSkillsInstall extracts embedded skills. If verbose, prints progress.
func RunSkillsInstall(force bool, verbose bool) {
	count, err := skills.Install(force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error installing skills: %v\n", err)
		os.Exit(1)
	}

	if count == 0 {
		if verbose {
			fmt.Fprintln(os.Stderr, "Skills already up to date.")
		}
		return
	}

	dir, _ := skills.InstallDir()
	if verbose {
		for _, s := range skills.SkillDirs() {
			fmt.Fprintf(os.Stderr, "✓ Installed %s to %s/%s/\n", s.InstallName, dir, s.InstallName)
		}
	}
}

func init() {
	skillsInstallCmd.Flags().BoolVar(&skillsForceFlag, "force", false, "Overwrite existing skills")
	skillsCmd.AddCommand(skillsPathCmd)
	skillsCmd.AddCommand(skillsInstallCmd)
	rootCmd.AddCommand(skillsCmd)
}
