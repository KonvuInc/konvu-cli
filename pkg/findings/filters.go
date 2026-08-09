package findings

import "github.com/spf13/cobra"

// CommonFilters is the typed result of reading the shared filter flags
// off a cobra command. Values are returned raw; callers translate them
// to their endpoint's parameter names (SCA "repository_url" vs SAST
// "where") and validate assessment values against their own vocabulary.
type CommonFilters struct {
	Since      string
	Severity   []string
	Repository []string
	Assessment []string
	Limit      int
	Format     string // -o value ("json" | "table" | "csv" | "")
	QuietIDs   bool   // -q
}

// RegisterCommonFlags adds the flags every finding list subcommand
// exposes. Callers add their own type-specific flags afterwards.
//
// Not every backend supports every filter. Callers translate only the
// filters their endpoint accepts and ignore the rest.
func RegisterCommonFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("since", "", "Filter by first-seen date (e.g. 7d, 2025-01-01)")
	f.StringSlice("severity", nil, "Filter by severity: critical, high, medium, low")
	f.StringSlice("repo", nil, "Filter by repository (repeatable)")
	f.StringSlice("assessment", nil, "Filter by assessment (repeatable; vocabulary is per-type)")
	f.Int("limit", 0, "Maximum rows to return (page size)")
	f.StringP("output", "o", "", "Output format: json, table, csv")
	f.BoolP("quiet", "q", false, "Print bare IDs, one per line")
}

// ReadCommonFilters reads the registered flags off cmd. Flags that were not
// registered on cmd yield the zero value (pflag's GetX errors on missing
// flags; we discard the error and the result is the zero value we want), so
// subcommands can register just the subset of common flags their endpoint
// supports and still use this reader.
func ReadCommonFilters(cmd *cobra.Command) CommonFilters {
	f := cmd.Flags()
	out := CommonFilters{}
	out.Since, _ = f.GetString("since")
	out.Severity, _ = f.GetStringSlice("severity")
	out.Repository, _ = f.GetStringSlice("repo")
	out.Assessment, _ = f.GetStringSlice("assessment")
	out.Limit, _ = f.GetInt("limit")
	out.Format, _ = f.GetString("output")
	out.QuietIDs, _ = f.GetBool("quiet")
	return out
}

// LimitOr returns f.Limit if set, otherwise the fallback. Handy for building
// pagination params where each subcommand has its own default page size.
func (f CommonFilters) LimitOr(fallback int) int {
	if f.Limit > 0 {
		return f.Limit
	}
	return fallback
}
