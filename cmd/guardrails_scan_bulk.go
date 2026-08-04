package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

// A var so a test can drive the poll loop without waiting real seconds.
var bulkPollEvery = 5 * time.Second

func init() {
	f := guardrailsScanCmd.Flags()
	f.Bool("all", false, "scan every repository Konvu can see, without a checkout")
	f.StringArray("remote", nil,
		"repository for Konvu to scan without a checkout, e.g. acme/web (repeatable)")
	f.Bool("wait", false, "wait for the scans to finish")
}

// bulkRequested reports whether Konvu should fetch the code rather than the caller bundling the
// checkout they are standing in. Neither flag reuses --repo: that already names the local one.
func bulkRequested(cmd *cobra.Command) bool {
	all, _ := cmd.Flags().GetBool("all")
	remotes, _ := cmd.Flags().GetStringArray("remote")
	return all || len(remotes) > 0
}

func bulkScanFlow(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")
	remotes, _ := cmd.Flags().GetStringArray("remote")
	wait, _ := cmd.Flags().GetBool("wait")
	format := output.DetectOutputFormat(mustGuardrailsOutput(cmd))

	// A path and --all/--remote ask for different things, and picking one would quietly ignore half
	// the command.
	if len(args) > 0 {
		return &clierrors.CLIError{
			Code:       "CONFLICTING_SCOPE",
			Message:    fmt.Sprintf("%q is a local checkout, but --all/--remote asks Konvu to fetch instead", args[0]),
			Suggestion: "Drop the path to let Konvu fetch, or drop --all/--remote to send this checkout.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	if all && len(remotes) > 0 {
		return &clierrors.CLIError{
			Code:       "CONFLICTING_SCOPE",
			Message:    "--all already means every repository Konvu can see",
			Suggestion: "Pass --all on its own, or name repositories with --remote.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	// --branch names ONE branch, so it is only meaningful when the scope is one repository. With
	// --all, or more than one --remote, there is no single repository for it to describe and
	// accepting it would record somewhere the caller did not ask for. Exactly one --remote is
	// unambiguous, so it is allowed and sent; anything else stays a usage error.
	branch := requestedBranch(cmd)
	if branch != "" && (all || len(remotes) != 1) {
		return &clierrors.CLIError{
			Code:       "CONFLICTING_SCOPE",
			Message:    "--branch names one branch, but --all or several --remote covers many repositories",
			Suggestion: "Name a single repository with one --remote, or scan a checkout without --all/--remote.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	if scanRepo != "" {
		return &clierrors.CLIError{
			Code:       "CONFLICTING_SCOPE",
			Message:    "--repo labels the local checkout, but --all/--remote names repositories itself",
			Suggestion: "Name them with --remote instead.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}

	body := map[string]any{}
	if !all {
		named := make([]any, 0, len(remotes))
		for _, r := range remotes {
			if r = strings.TrimSpace(r); r != "" {
				named = append(named, r)
			}
		}
		if len(named) == 0 {
			return &clierrors.CLIError{
				Code:       "NO_REPO_NAMED",
				Message:    "--remote was given but named no repository",
				Suggestion: "Name one as owner/name, or pass --all.",
				ExitCode:   clierrors.ExitUsageError,
			}
		}
		// Omitted, not empty: an omitted list asks for every visible repository, [] for none.
		body["repos"] = named
	}
	// Omitted when unknown, so Konvu resolves the repository's own default branch rather than
	// receiving a guess it cannot tell apart from a deliberate choice. Requires the matching API
	// support, released separately; an older server ignores the field and scans the default branch.
	if branch != "" {
		body["branch"] = branch
	}

	client := api.NewClient("", "")
	defer client.Close()

	data, err := client.Post(guardrailsAPI+"/dashboard/baselines", body)
	if err != nil {
		return err
	}

	jobs := queuedJobs(data)
	stranded := unobservable(data)

	if format == output.JSON {
		if !wait || (len(jobs) == 0 && len(stranded) == 0) {
			fmt.Println(output.FormatJSON(data))
			return nil
		}
		// Followed silently, then reported in the same document: a machine reader gets the final
		// state of every scan, and a failure still exits non-zero.
		outcomes, err := followScans(client, jobs, false)
		for _, repo := range stranded {
			outcomes[repo] = "unreadable"
		}
		data["results"] = outcomes
		fmt.Println(output.FormatJSON(data))
		return firstProblem(err, stranded)
	}

	printBulkQueued(data)
	if !wait || (len(jobs) == 0 && len(stranded) == 0) {
		return nil
	}
	_, err = followScans(client, jobs, true)
	return firstProblem(err, stranded)
}

// queuedJobs is repo -> job id for every scan there is something to follow, which includes one
// already running that this call did not start. Waiting on a repository you asked about should not
// depend on whether you were the one who queued it.
func queuedJobs(data map[string]any) map[string]string {
	queued := map[string]string{}
	for _, r := range getSlice(data, "rows") {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if id := getStr(m, "job_id"); id != "" {
			queued[getStr(m, "repo")] = id
		}
	}
	return queued
}

// unobservable is a repository whose scan is running but which came back with no job to poll -- the
// job finished between the queue attempt and the lookup. Under --wait that is "could not confirm",
// not success.
func unobservable(data map[string]any) []string {
	var out []string
	for _, r := range getSlice(data, "rows") {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if getStr(m, "outcome") == "already_queued" && getStr(m, "job_id") == "" {
			out = append(out, getStr(m, "repo"))
		}
	}
	sort.Strings(out)
	return out
}

// printBulkQueued reports every repository the server answered for. Nothing is dropped, and the
// outcomes are not collapsed: each needs different action.
func printBulkQueued(data map[string]any) {
	queued := queuedJobs(data)
	byOutcome := map[string][]string{}

	for _, r := range getSlice(data, "rows") {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		byOutcome[getStr(m, "outcome")] = append(byOutcome[getStr(m, "outcome")], getStr(m, "repo"))
	}
	for _, names := range byOutcome {
		sort.Strings(names)
	}

	if names := byOutcome["queued"]; len(names) > 0 {
		fmt.Printf("Scanning %d repositor%s:\n", len(names), plural(len(names)))
		for _, n := range names {
			fmt.Printf("  %s\n", n)
		}
	}
	if names := byOutcome["already_queued"]; len(names) > 0 {
		fmt.Printf("Already running: %s\n", strings.Join(names, ", "))
	}
	if names := byOutcome["not_visible"]; len(names) > 0 {
		fmt.Println()
		fmt.Printf("Konvu cannot see: %s\n", strings.Join(names, ", "))
		fmt.Println("  Add them to the repository selection with 'konvu guardrails connect'.")
	}
	if names := byOutcome["unknown"]; len(names) > 0 {
		fmt.Println()
		fmt.Printf("Could not tell whether Konvu can see: %s\n", strings.Join(names, ", "))
		fmt.Println("  GitHub could not be reached, so this is not the same as them being missing.")
	}
	if names := byOutcome["not_allowed"]; len(names) > 0 {
		fmt.Println()
		fmt.Printf("Not allowed: %s\n", strings.Join(names, ", "))
		fmt.Println("  Ask whoever runs your Konvu service.")
	}
	// Without this, a run that covered four of twelve repositories reads like one that covered all.
	if un := strList(data["unreachable"]); len(un) > 0 {
		fmt.Println()
		fmt.Printf("Could not list repositories for: %s\n", strings.Join(un, ", "))
		fmt.Println("  Some repositories may be missing from this run. Try again shortly.")
	}
	if len(byOutcome) == 0 {
		fmt.Println("Nothing to do: no repositories matched.")
		return
	}
	if len(queued) > 0 {
		fmt.Println()
		fmt.Println("This takes a while. Check progress with 'konvu guardrails list',")
		fmt.Println("then approve each repository's rules with 'konvu guardrails approve <repo>'.")
	}
}

// followScans polls every queued job until all finish or the timeout passes, returning each
// repository's final state. One round-robin pass rather than a goroutine each: the scans already
// run in parallel server-side.
//
// `progress` prints as it goes, for a human watching. The outcomes come back either way, so
// --wait means the same thing whatever the output format.
func followScans(
	client *api.Client, jobs map[string]string, progress bool,
) (map[string]string, error) {
	if progress {
		fmt.Println()
		fmt.Printf("Waiting for %d scan(s) (Ctrl-C to stop; they carry on)...\n", len(jobs))
	}

	outcomes := make(map[string]string, len(jobs))
	pending := make(map[string]string, len(jobs))
	for repo, id := range jobs {
		pending[repo] = id
	}
	var failed, unread []string

	deadline := time.Now().Add(scanTimeout)
	for len(pending) > 0 {
		time.Sleep(bulkPollEvery)
		for _, repo := range sortedKeys(pending) {
			st, err := client.Get(guardrailsAPI+"/baselines/jobs/"+pending[repo], nil)
			if err != nil {
				// Reported, not swallowed, and the other repositories are still followed.
				if progress {
					fmt.Printf("  %s: could not read progress (%v)\n", repo, err)
				}
				outcomes[repo] = "unreadable"
				unread = append(unread, repo)
				delete(pending, repo)
				continue
			}
			switch getStr(st, "status") {
			case "done":
				if progress {
					fmt.Printf("  %s: done\n", repo)
				}
				outcomes[repo] = "done"
				delete(pending, repo)
			case "error":
				msg := getStr(st, "error")
				if msg == "" {
					msg = "the scan could not be completed"
				}
				if progress {
					fmt.Printf("  %s: failed - %s\n", repo, msg)
				}
				outcomes[repo] = "failed: " + msg
				failed = append(failed, repo)
				delete(pending, repo)
			}
		}
		if time.Now().After(deadline) {
			for repo := range pending {
				outcomes[repo] = "still running"
			}
			// An error, like the single-repo path beside this one: a caller that asked to wait and
			// did not get confirmation must not read exit 0 as "all done". They do carry on, so the
			// message says where to look.
			return outcomes, &clierrors.CLIError{
				Code: "STILL_RUNNING",
				Message: fmt.Sprintf("still running after %s: %s",
					scanTimeout, strings.Join(sortedKeys(pending), ", ")),
				Suggestion: "They carry on Konvu's side; check with 'konvu guardrails list'.",
				ExitCode:   clierrors.ExitGeneralError,
			}
		}
	}
	// Anything that did not finish cleanly is a non-zero exit. Printing the failure and returning
	// nil would hand CI a green run over scans that never produced anything.
	if len(failed) > 0 || len(unread) > 0 {
		return outcomes, &clierrors.CLIError{
			Code:     "SCAN_FAILED",
			Message:  bulkFailureMessage(failed, unread),
			ExitCode: clierrors.ExitGeneralError,
		}
	}
	if progress {
		fmt.Println()
		fmt.Println("Approve each repository's rules with 'konvu guardrails approve <repo>'.")
	}
	return outcomes, nil
}

// firstProblem keeps a real polling failure ahead of an unobservable scan: the first names what
// went wrong, the second only says we could not tell.
func firstProblem(err error, stranded []string) error {
	if err != nil {
		return err
	}
	if len(stranded) > 0 {
		return &clierrors.CLIError{
			Code: "SCAN_UNOBSERVED",
			Message: fmt.Sprintf("already running, and finished before we could follow it: %s",
				strings.Join(stranded, ", ")),
			Suggestion: "Check the outcome with 'konvu guardrails list'.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return nil
}

func bulkFailureMessage(failed, unread []string) string {
	var parts []string
	if len(failed) > 0 {
		sort.Strings(failed)
		parts = append(parts, fmt.Sprintf("failed: %s", strings.Join(failed, ", ")))
	}
	if len(unread) > 0 {
		sort.Strings(unread)
		parts = append(parts, fmt.Sprintf("progress unreadable: %s", strings.Join(unread, ", ")))
	}
	return strings.Join(parts, "; ")
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
