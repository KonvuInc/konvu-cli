package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

// ingestMaxBatch mirrors dashboard_backend's INGEST_MAX_BATCH: the server
// rejects a POST /sca_findings body with more findings, so chunk client-side.
const ingestMaxBatch = 1000

var scaSubmitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit SCA findings from another scanner for triage",
	Long: `Submit SCA findings (e.g. exported from Snyk or Dependabot) for triage.

Reads a JSON array of findings from --file (or stdin with '-') and posts them
against --repo. Findings on a Konvu-connected, scanned repo flow into AI triage
automatically. Re-submitting the same finding updates it instead of duplicating;
an update replaces every optional field from the payload, so a changed 'source'
renames the scanner and an omitted one clears the label — send it every time.

Each finding object accepts:
  vulnerability_id     (required) CVE or GHSA id
  manifest_location    (required) path to the dependency manifest
  dependency_name      (required) affected package
  dependency_version   affected version
  dependency_ecosystem npm, maven, pypi, … (derived from the manifest when omitted)
  source               reporting scanner (snyk, dependabot, …), max 64 chars; kept
                       verbatim, read back as 'scanner', filterable with
                       'finding list --source'; last submission wins
  state                open (default), dismissed, or fixed
  transitivity         direct or transitive

Exit codes: 0 success, 1 general error (incl. all findings rejected), 2 invalid arguments, 4 auth failed`,
	Example: `  # Submit a Snyk export against a repo's default branch
  konvu finding submit --repo github:acme/web --file snyk-findings.json

  # Pipe findings in for a specific branch, preview only
  cat findings.json | konvu finding submit --repo github:acme/web --ref release-2.3 --file - --dry-run`,
	RunE: runScaSubmit,
}

func runScaSubmit(cmd *cobra.Command, args []string) error {
	repo, _ := cmd.Flags().GetString("repo")
	ref, _ := cmd.Flags().GetString("ref")
	file, _ := cmd.Flags().GetString("file")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	outputFlag, _ := cmd.Flags().GetString("output")
	format := output.DetectOutputFormat(outputFlag)

	if repo == "" || file == "" {
		fmt.Fprintln(os.Stderr, "Error: --repo and --file are required")
		os.Exit(clierrors.ExitUsageError)
	}

	findings, err := readFindingsFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(clierrors.ExitUsageError)
	}
	if len(findings) == 0 {
		fmt.Fprintln(os.Stderr, "No findings to submit.")
		return nil
	}

	repository := map[string]any{"vcs_repo_url": repo}
	if ref != "" {
		repository["vcs_ref"] = ref
	}

	if dryRun {
		if format == output.JSON {
			fmt.Println(output.FormatJSON(map[string]any{
				"dry_run":    true,
				"repository": repository,
				"total":      len(findings),
			}))
		} else {
			target := repo
			if ref != "" {
				target += " (ref " + ref + ")"
			}
			fmt.Fprintf(os.Stderr, "Dry run: would submit %d findings to %s. Use without --dry-run to execute.\n", len(findings), target)
		}
		return nil
	}

	client := api.NewClient("", "")
	defer client.Close()

	var created, updated, unmapped, rejected int
	var results []any
	var repoEcho map[string]any

	for start := 0; start < len(findings); start += ingestMaxBatch {
		end := start + ingestMaxBatch
		if end > len(findings) {
			end = len(findings)
		}
		resp, err := client.Post("/sca_findings", map[string]any{
			"repository": repository,
			"findings":   findings[start:end],
		})
		if err != nil {
			if _, ok := err.(*api.AuthenticationError); ok {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(clierrors.ExitAuthFailed)
			}
			fmt.Fprintln(os.Stderr, "API Error:", err)
			os.Exit(clierrors.ExitGeneralError)
		}
		created += intField(resp, "created_count")
		updated += intField(resp, "updated_count")
		unmapped += intField(resp, "accepted_unmapped_count")
		rejected += intField(resp, "rejected_count")
		if repoEcho == nil {
			repoEcho, _ = resp["repository"].(map[string]any)
		}
		// The server indexes results within its request; offset back to the
		// file's position so a chunked submission still reports stable indices.
		for _, r := range getSlice(resp, "results") {
			m, ok := r.(map[string]any)
			if !ok {
				continue
			}
			if idx, ok := m["index"].(float64); ok {
				m["index"] = int(idx) + start
			}
			results = append(results, m)
		}
	}

	renderSubmitResult(format, repoEcho, created, updated, unmapped, rejected, results)

	// The endpoint returns 201 even when every item is rejected; a submission
	// that stored nothing must exit non-zero so a broken pipeline isn't silent.
	if rejected == len(findings) {
		os.Exit(clierrors.ExitGeneralError)
	}
	return nil
}

func renderSubmitResult(format output.OutputFormat, repo map[string]any, created, updated, unmapped, rejected int, results []any) {
	if format == output.JSON {
		fmt.Println(output.FormatJSON(map[string]any{
			"repository":              repo,
			"created_count":           created,
			"updated_count":           updated,
			"accepted_unmapped_count": unmapped,
			"rejected_count":          rejected,
			"results":                 results,
		}))
		return
	}

	target := getStr(repo, "vcs_repo_url")
	if ref := getStr(repo, "vcs_ref"); ref != "" {
		target += " (ref " + ref + ")"
	}
	fmt.Fprintf(os.Stderr, "\nSubmitted to %s\n", target)
	fmt.Fprintf(os.Stderr, "created %d · updated %d · unmapped %d · rejected %d\n\n",
		created, updated, unmapped, rejected)

	rows := make([]any, 0, len(results))
	for _, r := range results {
		m, _ := r.(map[string]any)
		rows = append(rows, map[string]any{
			"index":             intField(m, "index"),
			"vulnerability_id":  getStr(m, "vulnerability_id"),
			"manifest_location": getStr(m, "manifest_location"),
			"status":            getStr(m, "status"),
			"reason":            getStr(m, "reason"),
		})
	}
	cols := []string{"index", "vulnerability_id", "manifest_location", "status", "reason"}
	fmt.Print(output.FormatTable(map[string]any{"results": rows}, cols, "results", nil))
}

// intField reads a numeric JSON field (decoded as float64) as an int.
func intField(m map[string]any, key string) int {
	if f, ok := getFloat(m, key); ok {
		return int(f)
	}
	return 0
}

func readFindingsFile(path string) ([]any, error) {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", path, err)
	}
	return parseFindings(data)
}

// parseFindings accepts either a bare JSON array of finding objects or an
// object with a "findings" array (so the API's own example body also works).
func parseFindings(data []byte) ([]any, error) {
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	switch v := parsed.(type) {
	case []any:
		return v, nil
	case map[string]any:
		if f, ok := v["findings"].([]any); ok {
			return f, nil
		}
	}
	return nil, fmt.Errorf(`expected a JSON array of findings (or an object with a "findings" array)`)
}

func init() {
	scaSubmitCmd.Flags().StringP("repo", "r", "", "Repository URL, e.g. github:acme/web (required)")
	scaSubmitCmd.Flags().String("ref", "", "Git branch or tag ref (default: repo's default branch)")
	scaSubmitCmd.Flags().StringP("file", "f", "", "JSON file of findings, or '-' for stdin (required)")
	scaSubmitCmd.Flags().Bool("dry-run", false, "Preview what would be submitted without executing")
	scaSubmitCmd.Flags().StringP("output", "o", "", "Output format: json, table")

	scaCmd.AddCommand(scaSubmitCmd)

	// BC alias: bare `konvu finding submit` → `konvu finding sca submit`.
	copyFlagsFrom(findingSubmitCmd, scaSubmitCmd)
	findingCmd.AddCommand(findingSubmitCmd)
}
