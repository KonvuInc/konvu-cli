package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage bundled Claude Code skills",
}

var skillsPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the path to bundled skills directory",
	Run: func(cmd *cobra.Command, args []string) {
		// Skills are bundled alongside the binary
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		// Resolve symlinks (npm installs create symlinks)
		exe, _ = filepath.EvalSymlinks(exe)
		skillsDir := filepath.Join(filepath.Dir(exe), "..", "skills")
		fmt.Println(filepath.Clean(skillsDir))
	},
}

func init() {
	skillsCmd.AddCommand(skillsPathCmd)
	rootCmd.AddCommand(skillsCmd)
}
