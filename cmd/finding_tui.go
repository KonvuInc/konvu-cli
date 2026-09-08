package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const findingEmptyState = "No SCA findings match these filters.\n"

type findingTUIDependencies struct {
	pick   func([]output.FindingOption, int) (int, bool, error)
	detail func(int) (string, error)
	open   func(string) (output.BaselineWorkspaceOutcome, error)
}

func shouldBrowseFindings(cmd *cobra.Command) bool {
	outputFlag, _ := cmd.Flags().GetString("output")
	quiet, _ := cmd.Flags().GetBool("quiet")
	count, _ := cmd.Flags().GetBool("count")
	groupBy, _ := cmd.Flags().GetString("group-by")
	fields, _ := cmd.Flags().GetString("fields")
	return outputFlag == "" && !quiet && !count && groupBy == "" && fields == "" && output.BaselineTerminalInteractive()
}

func findingLoading(cmd *cobra.Command, browse bool, label string) func() {
	if !browse {
		return func() {}
	}
	name := strings.TrimSpace(label + " findings")
	_ = output.WriteString(cmd.ErrOrStderr(), "Loading "+name+"…\r")
	return func() { _ = output.WriteString(cmd.ErrOrStderr(), "\r\033[K") }
}

func browseSCAFindings(cmd *cobra.Command, client *api.Client, rows []map[string]any) error {
	if len(rows) == 0 {
		return output.WriteString(cmd.OutOrStdout(), findingEmptyState)
	}
	options := make([]output.FindingOption, 0, len(rows))
	for _, row := range rows {
		options = append(options, output.FindingOption{
			Kind:       "SCA",
			Severity:   strings.ToUpper(getStr(row, "severity")),
			Assessment: findingDisplayValue(getStr(row, "assessment")),
			Finding:    findingDisplayTitle(row),
			Summary:    getStr(row, "assessment_summary"),
			Repository: getStr(row, "repository"),
			State:      findingDisplayValue(getStr(row, "state")),
		})
	}
	return executeFindingOptionsTUI(options, findingTUIDependencies{
		pick: output.PickFinding,
		detail: func(index int) (string, error) {
			detail, err := client.Get(fmt.Sprintf("/sca_findings/%s", getStr(rows[index], "id")), nil)
			if err != nil {
				return "", err
			}
			// The browser keeps the assessment rationale concise. Proof-level
			// evidence remains available through `finding get --include evidence`.
			return findingDetailText(buildFindingResult(detail, false)), nil
		},
		open: output.BrowseFindingDetail,
	})
}

func browseSASTFindings(cmd *cobra.Command, rows []map[string]any) error {
	if len(rows) == 0 {
		return output.WriteString(cmd.OutOrStdout(), "No SAST findings match these filters.\n")
	}
	options := make([]output.FindingOption, 0, len(rows))
	for _, row := range rows {
		assessment := getStr(row, "assessment")
		if assessment == "" {
			assessment = getStr(row, "triage_status")
		}
		options = append(options, output.FindingOption{
			Kind:       "SAST",
			Severity:   strings.ToUpper(getStr(row, "severity")),
			Assessment: findingDisplayValue(assessment),
			Finding:    getStr(row, "title"),
			Repository: getStr(row, "repo"),
			State:      findingDisplayValue(getStr(row, "state")),
		})
	}
	return executeFindingOptionsTUI(options, findingTUIDependencies{
		pick: output.PickFinding,
		detail: func(index int) (string, error) {
			return sastFindingDetailText(rows[index]), nil
		},
		open: output.BrowseFindingDetail,
	})
}

func browseSecretFindings(cmd *cobra.Command, rows []map[string]any) error {
	if len(rows) == 0 {
		return output.WriteString(cmd.OutOrStdout(), "No secret findings match these filters.\n")
	}
	options := make([]output.FindingOption, 0, len(rows))
	for _, row := range rows {
		options = append(options, output.FindingOption{
			Kind:       "Secret",
			Assessment: findingDisplayValue(getStr(row, "assessment")),
			Finding:    orDefault(getStr(row, "provider"), "Secret credential"),
			State:      findingDisplayValue(getStr(row, "verification_status")),
		})
	}
	return executeFindingOptionsTUI(options, findingTUIDependencies{
		pick: output.PickFinding,
		detail: func(index int) (string, error) {
			return secretFindingDetailText(rows[index]), nil
		},
		open: output.BrowseFindingDetail,
	})
}

func browseContainerFindings(cmd *cobra.Command, rows []map[string]any) error {
	if len(rows) == 0 {
		return output.WriteString(cmd.OutOrStdout(), "No container findings match these filters.\n")
	}
	options := make([]output.FindingOption, 0, len(rows))
	for _, row := range rows {
		asset := getStr(row, "image")
		if tag := getStr(row, "tag"); tag != "" {
			asset += ":" + tag
		}
		options = append(options, output.FindingOption{
			Kind:       "Container",
			Severity:   strings.ToUpper(getStr(row, "severity")),
			Assessment: findingDisplayValue(getStr(row, "assessment")),
			Finding:    findingDisplayTitle(map[string]any{"cve": getStr(row, "cve"), "dependency": getStr(row, "package")}),
			Repository: asset,
			State:      findingDisplayValue(getStr(row, "state")),
		})
	}
	return executeFindingOptionsTUI(options, findingTUIDependencies{
		pick: output.PickFinding,
		detail: func(index int) (string, error) {
			return containerFindingDetailText(rows[index]), nil
		},
		open: output.BrowseFindingDetail,
	})
}

func executeFindingOptionsTUI(options []output.FindingOption, deps findingTUIDependencies) error {
	selected := 0
	for {
		index, opened, err := deps.pick(options, selected)
		if err != nil {
			if errors.Is(err, output.ErrBaselineCancelled) {
				return nil
			}
			return err
		}
		selected = index
		if !opened {
			return nil
		}

		detail, err := deps.detail(selected)
		if err != nil {
			return findingTUIError(err)
		}
		outcome, err := deps.open(detail)
		if err != nil {
			if errors.Is(err, output.ErrBaselineCancelled) {
				return nil
			}
			return err
		}
		if outcome == output.BaselineWorkspaceBack {
			continue
		}
		return nil
	}
}

func runFindingRoot(cmd *cobra.Command, _ []string) error {
	if !output.BaselineTerminalInteractive() {
		return cmd.Help()
	}
	client := api.NewClient("", "")
	defer client.Close()
	clearLoading := findingLoading(cmd, true, "")
	data, err := client.Get("/sca_findings", map[string]any{
		"per_page": 50,
		"page":     1,
		"sort":     "recommendation",
		"order":    "desc",
	})
	clearLoading()
	if err != nil {
		return findingTUIError(err)
	}
	items := getSlice(data, "items")
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if finding, ok := item.(map[string]any); ok {
			rows = append(rows, transformFinding(finding))
		}
	}
	return browseSCAFindings(cmd, client, rows)
}

func findingTUIError(err error) error {
	if cliErr, ok := err.(*clierrors.CLIError); ok {
		return cliErr
	}
	if _, ok := err.(*api.AuthenticationError); ok {
		return clierrors.NewAuthError(err.Error())
	}
	return &clierrors.CLIError{
		Message:    fmt.Sprintf("load findings: %v", err),
		Suggestion: "Check your Konvu authentication and try again.",
	}
}

func findingDisplayTitle(row map[string]any) string {
	cve := getStr(row, "cve")
	dependency := getStr(row, "dependency")
	if cve == "" {
		return dependency
	}
	if dependency == "" {
		return cve
	}
	return cve + " · " + dependency
}

func findingDisplayValue(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	if value == "" {
		return "—"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func findingDetailText(result map[string]any) string {
	assessment := getMap(result, "assessment")
	finding := getMap(result, "finding")
	vulnerability := getMap(result, "vulnerability")

	var value strings.Builder
	value.WriteString(findingDisplayTitle(map[string]any{
		"cve":        findingVulnerabilityName(vulnerability),
		"dependency": getStr(finding, "dependency"),
	}))
	value.WriteString("\n")
	if repository := getStr(finding, "repository"); repository != "" {
		value.WriteString(repository)
		value.WriteString("\n")
	}
	value.WriteString(strings.Repeat("─", 64))
	value.WriteString("\n\nSCA FINDING · HOSTED\n")

	value.WriteString("\nASSESSMENT\n\n")
	fmt.Fprintf(&value, "%-16s %s\n", "Status", findingDisplayValue(getStr(assessment, "status")))
	if summary := getStr(assessment, "summary"); summary != "" {
		value.WriteString("\n")
		value.WriteString(summary)
		value.WriteString("\n")
	}

	checklist := getSlice(assessment, "checklist")
	if len(checklist) > 0 {
		value.WriteString("\nWHY THIS ASSESSMENT\n\n")
		for _, raw := range checklist {
			item, _ := raw.(map[string]any)
			description := getStr(item, "description")
			conclusion := getStr(item, "conclusion")
			value.WriteString("  • ")
			value.WriteString(description)
			value.WriteString("\n")
			if conclusion != "" {
				value.WriteString("    ")
				value.WriteString(conclusion)
				value.WriteString("\n")
			}
		}
	}

	value.WriteString("\nVULNERABILITY\n\n")
	fmt.Fprintf(&value, "%-16s %s\n", "Identifier", findingVulnerabilityName(vulnerability))
	fmt.Fprintf(&value, "%-16s %s\n", "Severity", strings.ToUpper(getStr(vulnerability, "severity")))
	fmt.Fprintf(&value, "%-16s %s\n", "Fix available", findingDisplayValue(getStr(vulnerability, "has_fix")))
	if summary := getStr(vulnerability, "summary"); summary != "" {
		value.WriteString("\n")
		value.WriteString(summary)
		value.WriteString("\n")
	}

	value.WriteString("\nFINDING\n\n")
	for _, field := range []struct{ label, key string }{
		{"ID", "id"},
		{"Dependency", "dependency"},
		{"Repository", "repository"},
		{"Manifest", "manifest"},
		{"State", "state"},
		{"Scanner", "scanner"},
		{"First seen", "first_seen"},
		{"Triage URL", "triage_url"},
	} {
		if fieldValue := getStr(finding, field.key); fieldValue != "" {
			fmt.Fprintf(&value, "%-16s %s\n", field.label, fieldValue)
		}
	}
	return value.String()
}

func findingVulnerabilityName(vulnerability map[string]any) string {
	for _, raw := range getSlice(vulnerability, "aliases") {
		if alias, ok := raw.(string); ok && (strings.HasPrefix(alias, "CVE-") || strings.HasPrefix(alias, "GHSA-")) {
			return alias
		}
	}
	return orDefault(getStr(vulnerability, "cve"), "Unknown vulnerability")
}

func sastFindingDetailText(row map[string]any) string {
	assessment := getStr(row, "assessment")
	if assessment == "" {
		assessment = getStr(row, "triage_status")
	}
	var value strings.Builder
	writeFindingDetailHeader(&value, getStr(row, "title"), getStr(row, "repo"), "SAST FINDING · HOSTED")
	value.WriteString("\nASSESSMENT\n\n")
	writeFindingDetailField(&value, "Status", findingDisplayValue(assessment))
	writeFindingDetailField(&value, "Confidence", findingDisplayValue(getStr(row, "confidence")))
	value.WriteString("\nFINDING\n\n")
	writeFindingDetailField(&value, "Severity", strings.ToUpper(getStr(row, "severity")))
	writeFindingDetailField(&value, "Location", getStr(row, "location"))
	writeFindingDetailField(&value, "Repository", getStr(row, "repo"))
	writeFindingDetailField(&value, "CWEs", strings.Join(stringsOf(getSlice(row, "cwe_ids")), ", "))
	writeFindingDetailField(&value, "State", findingDisplayValue(getStr(row, "state")))
	writeFindingDetailField(&value, "Investigation ID", getStr(row, "id"))
	writeFindingDetailField(&value, "Detection ID", getStr(row, "detection_id"))
	writeFindingDetailField(&value, "Triage URL", getStr(row, "triage_url"))
	return value.String()
}

func secretFindingDetailText(row map[string]any) string {
	provider := orDefault(getStr(row, "provider"), "Secret credential")
	var value strings.Builder
	writeFindingDetailHeader(&value, provider, "", "SECRET FINDING · HOSTED")
	value.WriteString("\nASSESSMENT\n\n")
	writeFindingDetailField(&value, "Status", findingDisplayValue(getStr(row, "assessment")))
	value.WriteString("\nSECRET\n\n")
	writeFindingDetailField(&value, "Provider", provider)
	writeFindingDetailField(&value, "Verification", findingDisplayValue(getStr(row, "verification_status")))
	value.WriteString("\nFINDING\n\n")
	writeFindingDetailField(&value, "ID", getStr(row, "id"))
	writeFindingDetailField(&value, "First seen", getStr(row, "first_seen"))
	writeFindingDetailField(&value, "Last seen", getStr(row, "last_seen"))
	writeFindingDetailField(&value, "Triage URL", getStr(row, "triage_url"))
	return value.String()
}

func containerFindingDetailText(row map[string]any) string {
	title := findingDisplayTitle(map[string]any{"cve": getStr(row, "cve"), "dependency": getStr(row, "package")})
	asset := getStr(row, "image")
	if tag := getStr(row, "tag"); tag != "" {
		asset += ":" + tag
	}
	var value strings.Builder
	writeFindingDetailHeader(&value, title, asset, "CONTAINER FINDING · HOSTED")
	value.WriteString("\nASSESSMENT\n\n")
	writeFindingDetailField(&value, "Status", findingDisplayValue(getStr(row, "assessment")))
	value.WriteString("\nVULNERABILITY\n\n")
	writeFindingDetailField(&value, "Identifier", getStr(row, "cve"))
	writeFindingDetailField(&value, "Severity", strings.ToUpper(getStr(row, "severity")))
	writeFindingDetailField(&value, "Package", getStr(row, "package"))
	writeFindingDetailField(&value, "Version", getStr(row, "version"))
	writeFindingDetailField(&value, "Ecosystem", getStr(row, "ecosystem"))
	value.WriteString("\nASSET\n\n")
	writeFindingDetailField(&value, "Image", getStr(row, "image"))
	writeFindingDetailField(&value, "Tag", getStr(row, "tag"))
	value.WriteString("\nFINDING\n\n")
	writeFindingDetailField(&value, "ID", getStr(row, "id"))
	writeFindingDetailField(&value, "State", findingDisplayValue(getStr(row, "state")))
	writeFindingDetailField(&value, "Source", getStr(row, "source"))
	writeFindingDetailField(&value, "Observed", getStr(row, "observed_at"))
	writeFindingDetailField(&value, "Updated", getStr(row, "updated_at"))
	writeFindingDetailField(&value, "Triage URL", getStr(row, "triage_url"))
	return value.String()
}

func writeFindingDetailHeader(value *strings.Builder, title, context, kind string) {
	value.WriteString(orDefault(title, "Finding"))
	value.WriteString("\n")
	if context != "" {
		value.WriteString(context)
		value.WriteString("\n")
	}
	value.WriteString(strings.Repeat("─", 64))
	value.WriteString("\n\n")
	value.WriteString(kind)
	value.WriteString("\n")
}

func writeFindingDetailField(value *strings.Builder, label, fieldValue string) {
	if strings.TrimSpace(fieldValue) != "" && fieldValue != "—" {
		fmt.Fprintf(value, "%-18s %s\n", label, fieldValue)
	}
}
