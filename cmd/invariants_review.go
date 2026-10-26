package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const (
	invariantProposed  = "proposed"
	invariantValidated = "validated"
	// The upload's limit on why_it_matters; a suggestion's can run longer.
	maxUploadWhyChars = 500
	steeringTopAgents = 3
	steeringRecent    = 5
)

func newInvariantsGetCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "get <id>",
		Short: "Show one invariant: its controls, code anchors and agent steering",
		Long: `Show one invariant, proposed or validated: its statement and why it matters, the controls
that keep it true (each with its code anchors), and how often Konvu steered coding agents
with it over the last 30 days. -o json prints everything, anchors and recent edits included.

Exit codes: 0 success, 1 general error, 2 invalid id or flag, 3 invariant not found, 4 auth failed or refused`,
		Example: `  konvu invariants get 3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b
  konvu invariants get 3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b -o json`,
		Args: cobra.ExactArgs(1),
		RunE: runInvariantsGet,
	}
	c.Flags().StringP("output", "o", "", "Output format: json, table")
	return c
}

func newInvariantsAcceptCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "accept <id>",
		Short: "Accept a security rule Konvu suggested, making it active",
		Long: `Accept a proposed invariant, a security rule Konvu suggested: it becomes validated (active),
attributed to you, and Konvu checks every change against it.

The suggestion is sent unchanged, at its rank. To change its wording first, write the edited
rule to a file and add it at the same rank with 'konvu invariants push': it replaces the
suggestion. Only a proposed invariant can be accepted. --dry-run checks it on the server and
writes nothing.

When the request fails on the server's side, accept reads the invariant back to say whether
it landed: validated means accepted (Konvu maps it to controls at the next code analysis or
rule change, nothing to repeat), proposed means nothing was written and the same accept is
safe to repeat.

Exit codes: 0 accepted, 1 general error (not a suggestion, changed meanwhile, or the request
failed), 2 invalid id or flag, 3 invariant not found, 4 auth failed or refused`,
		Example: `  konvu invariants accept 3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b --dry-run
  konvu invariants accept 3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b`,
		Args: cobra.ExactArgs(1),
		RunE: runInvariantsAccept,
	}
	c.Flags().Bool("dry-run", false, "Check on the server without writing anything")
	c.Flags().StringP("output", "o", "", "Output format: json, table")
	return c
}

func newInvariantsDeclineCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "decline <id>",
		Short: "Decline a security rule Konvu suggested",
		Long: `Decline a proposed invariant, a security rule Konvu suggested: it is dropped, its rank is
freed, and Konvu does not suggest it again. Only a proposed invariant can be declined; one
already declined comes back unchanged, so a decline is safe to repeat.

Exit codes: 0 declined, 1 general error (not a suggestion, or the request failed), 2 invalid
id or flag, 3 invariant not found, 4 auth failed or refused`,
		Example: `  konvu invariants decline 3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b`,
		Args:    cobra.ExactArgs(1),
		RunE:    runInvariantsDecline,
	}
	c.Flags().StringP("output", "o", "", "Output format: json, table")
	return c
}

// invariantID checks the <id> argument is a UUID before it becomes part of a request path.
func invariantID(arg string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(arg))
	if !uuidRe.MatchString(id) {
		return "", &clierrors.CLIError{
			Code:       "INVALID_ARGUMENTS",
			Message:    fmt.Sprintf("not an invariant id: %q", arg),
			Suggestion: "Pass an id from 'konvu invariants list': a UUID.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	return id, nil
}

func invariantPath(id string) string {
	return invariantsPath + "/" + id
}

func runInvariantsGet(cmd *cobra.Command, args []string) error {
	id, err := invariantID(args[0])
	if err != nil {
		return err
	}
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}

	client := api.NewClient("", "")
	defer client.Close()
	inv, err := client.Get(invariantPath(id), nil)
	if err != nil {
		return invariantReadError(err, id)
	}
	if format == output.JSON {
		fmt.Fprintln(cmd.OutOrStdout(), output.FormatJSON(inv))
		return nil
	}
	fmt.Fprint(cmd.OutOrStdout(), formatInvariant(inv))
	return nil
}

func runInvariantsAccept(cmd *cobra.Command, args []string) error {
	id, err := invariantID(args[0])
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}

	client := api.NewClient("", "")
	defer client.Close()
	inv, err := client.Get(invariantPath(id), nil)
	if err != nil {
		return invariantReadError(err, id)
	}
	if status := getStr(inv, "status"); status != invariantProposed {
		return invariantNotProposed("accepted", id, status)
	}
	resp, err := client.Post(invariantsPath, map[string]any{
		"dry_run":    dryRun,
		"invariants": []any{acceptedUpload(inv)},
	})
	if err != nil {
		if dryRun || !outcomeUnknown(err) {
			return invariantAcceptError(err, id, dryRun)
		}
		return confirmAccept(cmd, client, err, id, format)
	}
	rows := getSlice(resp, "invariants")
	if len(rows) != 1 {
		return &clierrors.CLIError{
			Code:       "API_ERROR",
			Message:    fmt.Sprintf("Konvu answered the accept with %d results instead of 1", len(rows)),
			Suggestion: fmt.Sprintf("Run 'konvu invariants get %s' to see where it stands.", id),
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	row, _ := rows[0].(map[string]any)
	switch getStr(row, "result") {
	case "unchanged":
		output.PrintStderr("It was already active: someone accepted it meanwhile.")
	case "created":
		// The statement left the live set between the read and the upload, so it was added anew.
		verb := "was added"
		if dryRun {
			verb = "would be added"
		}
		output.PrintStderr(fmt.Sprintf("The suggestion was no longer open, so it %s as a new active rule (id %s).", verb, getStr(row, "id")))
	}
	if dryRun {
		output.PrintStderr("Dry run: nothing was written. Run without --dry-run to accept it.")
	}
	resp["mapping_pending"] = false
	return printAccepted(cmd, format, resp)
}

// acceptedUpload is the suggestion as an upload item. The server matches it to the live
// statement and keeps the stored row as it is, so only the fields the upload requires travel;
// why_it_matters is cut to the upload's limit, which a suggestion's text is not held to.
func acceptedUpload(inv map[string]any) map[string]any {
	why := []rune(strings.TrimSpace(getStr(inv, "why_it_matters")))
	if len(why) > maxUploadWhyChars {
		why = why[:maxUploadWhyChars]
	}
	return map[string]any{
		"statement":      getStr(inv, "statement"),
		"why_it_matters": string(why),
		"property":       getStr(inv, "property"),
		"rank":           intField(inv, "rank"),
	}
}

// outcomeUnknown reports whether a failed upload may still have been saved: a server error or
// a lost connection can come after the commit (Guardrails could not queue the mapping, or
// Core's proxy gave up waiting), so the error alone does not say whether it landed.
func outcomeUnknown(err error) bool {
	var authErr *api.AuthenticationError
	if errors.As(err, &authErr) {
		return false
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500
	}
	return true
}

// confirmAccept reads the invariant back after an upload whose outcome is unknown, and says
// what actually happened: accepted, not written, or changed by someone else.
func confirmAccept(cmd *cobra.Command, client *api.Client, postErr error, id string, format output.OutputFormat) error {
	reason := postErr.Error()
	var apiErr *api.APIError
	if errors.As(postErr, &apiErr) {
		reason = invariantDetail(apiErr)
	}
	inv, err := client.Get(invariantPath(id), nil)
	if err != nil {
		return &clierrors.CLIError{
			Code:       "ACCEPT_UNCONFIRMED",
			Message:    fmt.Sprintf("the accept failed (%s) and the invariant could not be read back", reason),
			Suggestion: fmt.Sprintf("Run 'konvu invariants get %s': validated means it was accepted, proposed means run this accept again.", id),
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	switch status := getStr(inv, "status"); status {
	case invariantValidated:
		output.PrintStderr(fmt.Sprintf("Accepted, but the server then failed (%s): Konvu maps it to controls at the next code analysis or rule change. No need to accept it again.", reason))
		return printAccepted(cmd, format, map[string]any{
			"dry_run": false,
			"invariants": []any{map[string]any{
				"id":        getStr(inv, "id"),
				"rank":      intField(inv, "rank"),
				"statement": getStr(inv, "statement"),
				"status":    invariantValidated,
				"result":    "validated",
			}},
			"mapping_pending": true,
		})
	case invariantProposed:
		return &clierrors.CLIError{
			Code:       "API_ERROR",
			Message:    reason,
			Suggestion: "Nothing was accepted, and an accept is safe to repeat: run the same command again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	default:
		return &clierrors.CLIError{
			Code:       "INVARIANT_CHANGED",
			Message:    fmt.Sprintf("the accept failed (%s) and the suggestion is now %s", reason, statusOrUnknown(status)),
			Suggestion: "Run 'konvu invariants list' to check whether the rule is active under a new id.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
}

func printAccepted(cmd *cobra.Command, format output.OutputFormat, resp map[string]any) error {
	if format == output.JSON {
		fmt.Fprintln(cmd.OutOrStdout(), output.FormatJSON(resp))
		return nil
	}
	rows := make([]any, 0, 1)
	for _, r := range getSlice(resp, "invariants") {
		m, _ := r.(map[string]any)
		rows = append(rows, map[string]any{
			"rank":      intField(m, "rank"),
			"result":    getStr(m, "result"),
			"statement": getStr(m, "statement"),
		})
	}
	cols := []string{"rank", "result", "statement"}
	fmt.Fprint(cmd.OutOrStdout(), output.FormatTable(map[string]any{"invariants": rows}, cols, "invariants", nil))
	return nil
}

func runInvariantsDecline(cmd *cobra.Command, args []string) error {
	id, err := invariantID(args[0])
	if err != nil {
		return err
	}
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}

	client := api.NewClient("", "")
	defer client.Close()
	inv, err := client.Post(invariantPath(id)+"/decline", nil)
	if err != nil {
		return invariantDeclineError(err, id)
	}
	if format == output.JSON {
		fmt.Fprintln(cmd.OutOrStdout(), output.FormatJSON(inv))
		return nil
	}
	row := map[string]any{
		"rank":      intField(inv, "rank"),
		"status":    getStr(inv, "status"),
		"statement": getStr(inv, "statement"),
	}
	cols := []string{"rank", "status", "statement"}
	fmt.Fprint(cmd.OutOrStdout(), output.FormatTable(map[string]any{"invariants": []any{row}}, cols, "invariants", nil))
	return nil
}

// invariantNotProposed refuses a decision on an invariant that is not, or no longer, a suggestion.
func invariantNotProposed(verb, id, status string) error {
	message := fmt.Sprintf("invariant %s is %s; only a proposed one can be %s", id, statusOrUnknown(status), verb)
	suggestion := "Run 'konvu invariants list --status proposed' to see the open suggestions."
	if status == invariantValidated {
		message = fmt.Sprintf("invariant %s is already active (validated); only a proposed one can be %s", id, verb)
		suggestion = "Nothing to do. " + suggestion
	}
	return &clierrors.CLIError{
		Code:       "INVARIANT_NOT_PROPOSED",
		Message:    message,
		Suggestion: suggestion,
		ExitCode:   clierrors.ExitGeneralError,
	}
}

func statusOrUnknown(status string) string {
	if status == "" {
		return "of unknown status"
	}
	return status
}

// invariantReadError maps a failed read of one invariant: an id that is not one of the
// company's is a 404 (exit 3); the rest is mapped like a failed list.
func invariantReadError(err error, id string) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
		return invariantNotFound(apiErr, id)
	}
	return invariantsListError(err)
}

func invariantNotFound(apiErr *api.APIError, id string) error {
	return &clierrors.CLIError{
		Code:       "INVARIANT_NOT_FOUND",
		Message:    fmt.Sprintf("invariant %s not found: %s", id, invariantDetail(apiErr)),
		Suggestion: "Run 'konvu invariants list' to see your company's invariants and their ids.",
		ExitCode:   clierrors.ExitNotFound,
	}
}

// invariantDecisionForbidden explains a 403 on an accept or a decline: a decision is
// attributed to the person who makes it.
func invariantDecisionForbidden(apiErr *api.APIError) error {
	return &clierrors.CLIError{
		Code:       "FORBIDDEN",
		Message:    invariantDetail(apiErr),
		Suggestion: "Run 'konvu whoami' to check who you are signed in as: a decision on a security rule is attributed to a person.",
		ExitCode:   clierrors.ExitAuthFailed,
	}
}

// invariantAcceptError maps a refused upload whose outcome is known: nothing was written.
func invariantAcceptError(err error, id string, dryRun bool) error {
	var authErr *api.AuthenticationError
	if errors.As(err, &authErr) {
		return clierrors.NewAuthError(err.Error())
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		if !dryRun {
			return clierrors.NewAPIError(err.Error())
		}
		return &clierrors.CLIError{
			Code:       "API_ERROR",
			Message:    err.Error(),
			Suggestion: "A dry run writes nothing: run it again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	detail := invariantDetail(apiErr)
	switch {
	case apiErr.StatusCode == 403:
		return invariantDecisionForbidden(apiErr)
	case apiErr.StatusCode == 409:
		return &clierrors.CLIError{
			Code:       "INVARIANT_CHANGED",
			Message:    detail,
			Suggestion: fmt.Sprintf("The suggestion changed after it was read. Run 'konvu invariants get %s' to see where it stands.", id),
			ExitCode:   clierrors.ExitGeneralError,
		}
	case apiErr.StatusCode == 422:
		return &clierrors.CLIError{
			Code:       "INVARIANT_NOT_ACCEPTED",
			Message:    detail,
			Suggestion: "Konvu cannot take this suggestion unchanged: write it to a file, fix what the message names, and add it at the same rank with 'konvu invariants push'.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	case apiErr.StatusCode >= 500 && dryRun:
		return &clierrors.CLIError{
			Code:       "API_ERROR",
			Message:    detail,
			Suggestion: "A dry run writes nothing: run it again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return clierrors.NewAPIError(detail)
}

// invariantDeclineError maps a refused decline. The server answers a repeat with the declined
// invariant, so every failure that might have landed is safe to retry as is.
func invariantDeclineError(err error, id string) error {
	var authErr *api.AuthenticationError
	if errors.As(err, &authErr) {
		return clierrors.NewAuthError(err.Error())
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return &clierrors.CLIError{
			Code:       "API_ERROR",
			Message:    err.Error(),
			Suggestion: "A decline is safe to repeat: run the same command again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	detail := invariantDetail(apiErr)
	switch {
	case apiErr.StatusCode == 403:
		return invariantDecisionForbidden(apiErr)
	case apiErr.StatusCode == 404:
		return invariantNotFound(apiErr, id)
	case apiErr.StatusCode == 409:
		return &clierrors.CLIError{
			Code:       "INVARIANT_NOT_PROPOSED",
			Message:    detail,
			Suggestion: "Only a suggestion (proposed) can be declined. Run 'konvu invariants list --status proposed' to see the open ones.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	case apiErr.StatusCode >= 500:
		return &clierrors.CLIError{
			Code:       "API_ERROR",
			Message:    detail,
			Suggestion: "A decline is safe to repeat: run the same command again.",
			Retryable:  true,
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return clierrors.NewAPIError(detail)
}

// formatInvariant renders one invariant for a terminal: what it says, the controls that keep
// it true, and how Konvu steered coding agents with it.
func formatInvariant(inv map[string]any) string {
	var b strings.Builder
	status := getStr(inv, "status")
	switch status {
	case invariantProposed:
		status += " (suggested by Konvu)"
	case invariantValidated:
		status += " (active)"
	}
	fmt.Fprintf(&b, "%s\n\n", getStr(inv, "statement"))
	fmt.Fprintf(&b, "Status:    %s\n", status)
	fmt.Fprintf(&b, "Property:  %s\n", getStr(inv, "property"))
	fmt.Fprintf(&b, "Rank:      %d\n", intField(inv, "rank"))
	fmt.Fprintf(&b, "Id:        %s\n", getStr(inv, "id"))
	if why := getStr(inv, "why_it_matters"); why != "" {
		fmt.Fprintf(&b, "Why:       %s\n", why)
	}

	controls := getSlice(inv, "controls")
	fmt.Fprintf(&b, "\nControls (%d)\n", len(controls))
	if len(controls) == 0 {
		b.WriteString("  none mapped yet\n")
	}
	for _, c := range controls {
		m, _ := c.(map[string]any)
		parts := []string{getStr(m, "title"), getStr(m, "role")}
		if ref := getStr(m, "standard_ref"); ref != "" {
			parts = append(parts, ref)
		}
		parts = append(parts, plural(len(getSlice(m, "code_anchors")), "code anchor"))
		fmt.Fprintf(&b, "  - %s\n", strings.Join(parts, " · "))
	}

	steering, ok := inv["steering"].(map[string]any)
	if !ok {
		b.WriteString("\nAgent steering: not reported\n")
		return b.String()
	}
	window := "so far"
	if days := intField(steering, "days"); days > 0 {
		window = fmt.Sprintf("last %d days", days)
	}
	last := getStr(steering, "last_at")
	if last == "" {
		last = "never"
	}
	fmt.Fprintf(&b, "\nAgent steering (%s): %s, last %s\n", window, plural(intField(steering, "edits"), "edit"), last)
	if agents := topAgents(getSlice(steering, "agents"), steeringTopAgents); agents != "" {
		fmt.Fprintf(&b, "  Top agents: %s\n", agents)
	}
	recent := getSlice(steering, "recent")
	if len(recent) > 0 {
		b.WriteString("  Recent edits:\n")
	}
	for _, r := range newestFirst(recent, steeringRecent) {
		fields := []string{getStr(r, "at"), getStr(r, "agent"), getStr(r, "developer"), getStr(r, "file"), getStr(r, "control_id")}
		kept := fields[:0]
		for _, f := range fields {
			if f != "" {
				kept = append(kept, f)
			}
		}
		fmt.Fprintf(&b, "    %s\n", strings.Join(kept, "  "))
	}
	return b.String()
}

// topAgents names the n agents steered most, most first, ties by name.
func topAgents(agents []any, n int) string {
	type agent struct {
		name  string
		count int
	}
	list := make([]agent, 0, len(agents))
	for _, a := range agents {
		m, _ := a.(map[string]any)
		if name := getStr(m, "name"); name != "" {
			list = append(list, agent{name, intField(m, "count")})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].count != list[j].count {
			return list[i].count > list[j].count
		}
		return list[i].name < list[j].name
	})
	if len(list) > n {
		list = list[:n]
	}
	names := make([]string, len(list))
	for i, a := range list {
		names[i] = fmt.Sprintf("%s (%d)", a.name, a.count)
	}
	return strings.Join(names, ", ")
}

// newestFirst returns up to n recent edits, newest first; timestamps are ISO 8601 in UTC, so
// they sort as strings.
func newestFirst(recent []any, n int) []map[string]any {
	edits := make([]map[string]any, 0, len(recent))
	for _, r := range recent {
		if m, ok := r.(map[string]any); ok {
			edits = append(edits, m)
		}
	}
	sort.SliceStable(edits, func(i, j int) bool { return getStr(edits[i], "at") > getStr(edits[j], "at") })
	if len(edits) > n {
		edits = edits[:n]
	}
	return edits
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
