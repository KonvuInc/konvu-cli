package cmd

import (
	"strings"
)

const guardrailsBaselineModel = "gpt-5.6-luna"

type guardrailsRunner func(args []string, apiKey, model string)

func resolveGuardrailsAPIKey(flagValue, envValue string) string {
	if value := strings.TrimSpace(flagValue); value != "" {
		return value
	}
	return strings.TrimSpace(envValue)
}

func runGuardrailsBaselineScan(
	repo, apiKey string,
	yes bool,
	run guardrailsRunner,
) {
	args := []string{"baseline", "scan", repo}
	if yes {
		args = append(args, "--yes")
	}
	run(args, apiKey, guardrailsBaselineModel)
}
