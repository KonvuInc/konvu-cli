package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	"github.com/KonvuInc/konvu-cli/pkg/auth"
	"github.com/KonvuInc/konvu-cli/pkg/config"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/KonvuInc/konvu-cli/skills"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication commands",
}

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show current user and company",
	RunE: func(cmd *cobra.Command, args []string) error {
		outputFlag, _ := cmd.Flags().GetString("output")
		format := output.DetectOutputFormat(outputFlag)

		client := api.NewClient("", "")
		defer client.Close()

		data, err := client.Get("/companies/current", nil)
		if err != nil {
			if _, ok := err.(*api.AuthenticationError); ok {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		if format == output.JSON {
			fmt.Println(output.FormatJSON(data))
		} else {
			fmt.Printf("Company:        %v\n", data["name"])
			fmt.Printf("Repositories:   %v\n", data["repositories_count"])
			fmt.Printf("Integrations:   %v\n", data["integrations_count"])
		}
		return nil
	},
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Konvu",
	RunE: func(cmd *cobra.Command, args []string) error {
		timeout, _ := cmd.Flags().GetInt("timeout")
		apiKeyFlag, _ := cmd.Flags().GetString("api-key")
		apiKeySet := cmd.Flags().Changed("api-key")

		if apiKeySet {
			key := apiKeyFlag
			if key == "" {
				key = promptAPIKey()
			}
			return loginWithAPIKey(key)
		}

		// Interactive picker
		domain := config.GetZitadelDomain()
		clientID := config.GetZitadelClientID()
		oauthAvailable := clientID != "" && strings.HasPrefix(domain, "https://")

		if !oauthAvailable {
			key := promptAPIKey()
			return loginWithAPIKey(key)
		}

		choice := output.Pick(
			"How would you like to authenticate?",
			[]string{"Browser login (OAuth)", "API key"},
			0,
		)

		if choice == 1 {
			key := promptAPIKey()
			return loginWithAPIKey(key)
		}
		return loginWithOAuth(timeout)
	},
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear stored credentials",
	Run: func(cmd *cobra.Command, args []string) {
		credsPath := config.GetCredentialsPath()
		if err := os.Remove(credsPath); err != nil {
			if os.IsNotExist(err) {
				fmt.Println("Not currently logged in.")
				return
			}
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		fmt.Println("Logged out successfully.")
	},
}

func promptAPIKey() string {
	fmt.Fprint(os.Stderr, "\nCreate an API key at: https://app.konvu.com/configuration/api_keys\n\n")
	fmt.Fprint(os.Stderr, "Paste your API key: ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

func loginWithAPIKey(apiKey string) error {
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "Error: API key cannot be empty.")
		os.Exit(1)
	}

	fmt.Println("Validating API key...")
	client := api.NewClient("", apiKey)
	defer client.Close()

	company, err := client.Get("/companies/current", nil)
	if err != nil {
		if _, ok := err.(*api.AuthenticationError); ok {
			fmt.Fprintln(os.Stderr, "Error: Invalid API key.")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}

	if err := auth.SaveCredentials(config.GetCredentialsPath(), map[string]any{
		"access_token": apiKey,
	}); err != nil {
		return err
	}

	fmt.Printf("Logged in to: %v\n", company["name"])

	offerSkills()

	return nil
}

func loginWithOAuth(timeout int) error {
	fmt.Println("Starting browser login...")

	echo := func(msg string) { fmt.Println(msg) }

	tokenData, err := auth.PerformDeviceFlowLogin(
		config.GetZitadelDomain(),
		config.GetZitadelClientID(),
		float64(timeout),
		echo,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		fmt.Fprintln(os.Stderr, "If browser login fails, try: konvu login --api-key")
		os.Exit(1)
	}

	if err := auth.SaveCredentials(config.GetCredentialsPath(), tokenData); err != nil {
		return err
	}

	fmt.Println("\nLogin successful!")

	client := api.NewClient("", "")
	defer client.Close()
	if company, err := client.Get("/companies/current", nil); err == nil {
		fmt.Printf("Logged in to: %v\n", company["name"])
	}

	offerSkills()

	return nil
}

func init() {
	whoamiCmd.Flags().StringP("output", "o", "", "Output format: json, table")
	loginCmd.Flags().IntP("timeout", "t", auth.DefaultLoginTimeout, "Login timeout in seconds")
	loginCmd.Flags().String("api-key", "", "Authenticate with an API key")

	authCmd.AddCommand(whoamiCmd)
	authCmd.AddCommand(loginCmd)
	authCmd.AddCommand(logoutCmd)

	rootCmd.AddCommand(authCmd)

	// Top-level convenience aliases — must clone flag definitions
	whoamiAlias := &cobra.Command{
		Use:   "whoami",
		Short: "Show current user and company",
		RunE:  whoamiCmd.RunE,
	}
	whoamiAlias.Flags().StringP("output", "o", "", "Output format: json, table")
	rootCmd.AddCommand(whoamiAlias)

	loginAlias := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with Konvu",
		RunE:  loginCmd.RunE,
	}
	loginAlias.Flags().IntP("timeout", "t", auth.DefaultLoginTimeout, "Login timeout in seconds")
	loginAlias.Flags().String("api-key", "", "Authenticate with an API key")
	rootCmd.AddCommand(loginAlias)

	logoutAlias := &cobra.Command{
		Use:   "logout",
		Short: "Clear stored credentials",
		Run:   logoutCmd.Run,
	}
	rootCmd.AddCommand(logoutAlias)
}

// offerSkills offers the bundled Claude Code skills after a successful login. One function for
// both login paths: they carried a byte-identical copy each, so a skill added to one pitch was
// invisible to whoever used the other.
func offerSkills() {
	if !skills.NeedsUpdate() {
		return
	}
	if !output.IsInteractive() {
		fmt.Fprintln(os.Stderr, "Run 'konvu skills install' to install Claude Code skills.")
		return
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Konvu ships with Claude Code skills for AI-assisted security workflows.")
	// Straight from the bundled inventory, so a skill cannot ship without being named here.
	for _, sd := range skills.SkillDirs() {
		if len(sd.Pitch) == 0 {
			continue
		}
		fmt.Fprintln(os.Stderr)
		for _, line := range sd.Pitch {
			fmt.Fprintln(os.Stderr, line)
		}
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Skills are installed to ~/.claude/skills/.")
	fmt.Fprintln(os.Stderr, "You can always install or update them later with: konvu skills install")
	fmt.Fprintln(os.Stderr)
	if output.Confirm("Install now?", true) {
		RunSkillsInstall(false, true)
	} else {
		fmt.Fprintln(os.Stderr, "Skipped. You can install later with: konvu skills install")
	}
}
