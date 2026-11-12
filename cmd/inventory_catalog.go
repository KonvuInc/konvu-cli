package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const (
	inventoryCatalogPath    = "/inventory/catalog"
	inventoryAppsPath       = "/inventory/apps/"
	inventoryComponentsPath = "/inventory/components"
)

func newInventoryCatalogCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "catalog",
		Short: "Your products, their modules, and the apps and components that deliver them",
		Long: `The catalog is one JSON document of how your company's code is organized:

  products     each product and its modules
  modules      modules of no product
  apps         each app (a running system): its product, module, summary and the
               components it holds, by name
  unassigned   components in no app (read only)

Konvu builds it from your code. To change it, get it, edit the file, and apply it:
a dry run lists every change first. Products, modules and apps you change are kept
by later rebuilds, and the components you place in an app stay there.`,
		Example: `  konvu inventory catalog get -o json > catalog.json
  konvu inventory catalog apply --file catalog.json --dry-run
  konvu inventory catalog apply --file catalog.json`,
	}
	get := &cobra.Command{
		Use:   "get",
		Short: "Print the catalog",
		Args:  cobra.NoArgs,
		RunE:  withJSONErrors(runInventoryCatalogGet),
	}
	get.Flags().StringP("output", "o", "", "Output format: json, table")
	apply := &cobra.Command{
		Use:   "apply",
		Short: "Make the catalog match a file",
		Long: `Make the catalog match --file (or stdin with '-'), a document like 'catalog get -o json'.

The file replaces the whole catalog, so "products", "modules" and "apps" are required,
and "revision" too: it names the catalog the file was read from, and an apply is
refused, writing nothing, once the catalog changed since (someone else's edit or a
rebuild). Get the catalog again and redo the edit then. An app
with an "id" is that app (rename it by changing its name); one without an id is
created. An app left out of the file is deleted, and its components go back to
Konvu to group. An app's product, when set, must be listed, and its module must be
one of that product's modules (an app's unchanged product and module always
stand). Components are named as in 'catalog get'; a component may sit in several
apps. "origin" and "unassigned" are ignored.

--dry-run lists the changes and writes nothing. Nothing is written either when the
file has a problem: every problem is listed.

Exit codes: 0 success, 1 general error, 2 invalid file, 3 not found, 4 auth failed or not enabled`,
		Example: `  konvu inventory catalog apply --file catalog.json --dry-run
  konvu inventory catalog get -o json | jq '...' | konvu inventory catalog apply --file -`,
		Args: cobra.NoArgs,
		RunE: withJSONErrors(runInventoryCatalogApply),
	}
	apply.Flags().String("file", "", "JSON catalog file, or '-' for stdin")
	apply.Flags().Bool("dry-run", false, "List the changes without writing anything")
	apply.Flags().StringP("output", "o", "", "Output format: json, table")
	c.AddCommand(get, apply)
	return c
}

func newInventoryAppsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "apps",
		Short: "Your apps: the running systems Konvu found in your code",
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List apps",
		Args:  cobra.NoArgs,
		RunE:  withJSONErrors(runInventoryAppsList),
	}
	list.Flags().StringP("output", "o", "", "Output format: json, table")
	show := &cobra.Command{
		Use:     "show <app>",
		Short:   "Show an app's components and how they connect",
		Example: `  konvu inventory apps show "Billing"`,
		Args:    cobra.ExactArgs(1),
		RunE:    withJSONErrors(runInventoryAppsShow),
	}
	show.Flags().StringP("output", "o", "", "Output format: json, table")
	c.AddCommand(list, show)
	return c
}

func newInventoryComponentsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "components",
		Short: "Your components: the deployable parts of your repositories",
	}
	list := &cobra.Command{
		Use:     "list",
		Short:   "List components",
		Example: `  konvu inventory components list --unassigned`,
		Args:    cobra.NoArgs,
		RunE:    withJSONErrors(runInventoryComponentsList),
	}
	list.Flags().Bool("unassigned", false, "Only components in no app")
	list.Flags().String("name", "", "Only components whose name contains this")
	list.Flags().StringP("output", "o", "", "Output format: json, table")
	show := &cobra.Command{
		Use:     "show <component>",
		Short:   "Show a component's sources, interfaces and connections",
		Example: `  konvu inventory components show acme/api`,
		Args:    cobra.ExactArgs(1),
		RunE:    withJSONErrors(runInventoryComponentsShow),
	}
	show.Flags().StringP("output", "o", "", "Output format: json, table")
	c.AddCommand(list, show)
	return c
}

func runInventoryCatalogGet(cmd *cobra.Command, _ []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	client := api.NewClient("", "")
	defer client.Close()
	catalog, err := client.Get(inventoryCatalogPath, nil)
	if err != nil {
		return inventoryCatalogError(err)
	}
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(catalog)+"\n")
	}
	var b strings.Builder
	for _, p := range getSlice(catalog, "products") {
		m, _ := p.(map[string]any)
		fmt.Fprintf(&b, "%s\n", clean(getStr(m, "name")))
		for _, module := range stringsOf(getSlice(m, "modules")) {
			fmt.Fprintf(&b, "  %s\n", clean(module))
		}
	}
	if modules := stringsOf(getSlice(catalog, "modules")); len(modules) > 0 {
		fmt.Fprintf(&b, "Modules of no product: %s\n", clean(strings.Join(modules, ", ")))
	}
	b.WriteString("\n")
	b.WriteString(appsTable(getSlice(catalog, "apps")))
	fmt.Fprintf(&b, "\n%d components in no app\n", len(getSlice(catalog, "unassigned")))
	return output.WriteString(cmd.OutOrStdout(), b.String())
}

func runInventoryCatalogApply(cmd *cobra.Command, _ []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	file, _ := cmd.Flags().GetString("file")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if file == "" {
		return &clierrors.CLIError{
			Code:       "INVALID_ARGUMENTS",
			Message:    "--file is required",
			Suggestion: "Pass the edited output of 'konvu inventory catalog get -o json', or '-' to read stdin.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	doc, err := readCatalogFile(file)
	if err != nil {
		return &clierrors.CLIError{
			Code:       "INVALID_CATALOG",
			Message:    err.Error(),
			Suggestion: "Start from 'konvu inventory catalog get -o json'.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	client := api.NewClient("", "")
	defer client.Close()
	path := inventoryCatalogPath
	if dryRun {
		path += "?dry_run=true"
	}
	result, err := client.Put(path, doc)
	if err != nil {
		return catalogCallError(err, true)
	}
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(result)+"\n")
	}
	changes := stringsOf(getSlice(result, "changes"))
	var b strings.Builder
	switch {
	case len(changes) == 0:
		b.WriteString("No changes: the catalog already matches the file.\n")
	case result["applied"] == true:
		fmt.Fprintf(&b, "Applied %d changes:\n", len(changes))
	default:
		fmt.Fprintf(&b, "Would apply %d changes (dry run, nothing written):\n", len(changes))
	}
	for _, change := range changes {
		fmt.Fprintf(&b, "  %s\n", clean(change))
	}
	return output.WriteString(cmd.OutOrStdout(), b.String())
}

func runInventoryAppsList(cmd *cobra.Command, _ []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	client := api.NewClient("", "")
	defer client.Close()
	catalog, err := client.Get(inventoryCatalogPath, nil)
	if err != nil {
		return inventoryCatalogError(err)
	}
	apps := getSlice(catalog, "apps")
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(apps)+"\n")
	}
	return output.WriteString(cmd.OutOrStdout(), appsTable(apps))
}

func runInventoryAppsShow(cmd *cobra.Command, args []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	client := api.NewClient("", "")
	defer client.Close()
	id := hexID(args[0])
	if id == "" {
		catalog, err := client.Get(inventoryCatalogPath, nil)
		if err != nil {
			return inventoryCatalogError(err)
		}
		if id, err = pickByName(getSlice(catalog, "apps"), args[0], "app"); err != nil {
			return err
		}
	}
	detail, err := client.Get(inventoryAppsPath+url.PathEscape(id), nil)
	if err != nil {
		return inventoryCatalogError(err)
	}
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(detail)+"\n")
	}
	app, _ := detail["app"].(map[string]any)
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", clean(getStr(app, "name")))
	for _, field := range []string{"product", "module", "summary", "origin"} {
		if v := getStr(app, field); v != "" {
			fmt.Fprintf(&b, "  %-8s %s\n", field, clean(v))
		}
	}
	rows := make([]any, 0)
	for _, m := range getSlice(detail, "members") {
		member, _ := m.(map[string]any)
		rows = append(rows, map[string]any{
			"name": clean(getStr(member, "name")),
			"kind": clean(getStr(member, "kind")),
		})
	}
	b.WriteString("\n")
	b.WriteString(output.FormatTable(map[string]any{"members": rows}, []string{"name", "kind"}, "members", nil))
	fmt.Fprintf(&b, "\n%d connections\n", len(getSlice(detail, "edges")))
	return output.WriteString(cmd.OutOrStdout(), b.String())
}

func runInventoryComponentsList(cmd *cobra.Command, _ []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	unassigned, _ := cmd.Flags().GetBool("unassigned")
	name, _ := cmd.Flags().GetString("name")
	client := api.NewClient("", "")
	defer client.Close()
	items, err := listComponents(client, name, unassigned)
	if err != nil {
		return inventoryCatalogError(err)
	}
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(items)+"\n")
	}
	rows := make([]any, 0, len(items))
	for _, item := range items {
		m, _ := item.(map[string]any)
		rows = append(rows, map[string]any{
			"name":        clean(getStr(m, "name")),
			"kind":        clean(getStr(m, "kind")),
			"connections": intOf(m["connections"]),
			"id":          getStr(m, "id"),
		})
	}
	cols := []string{"name", "kind", "connections", "id"}
	return output.WriteString(cmd.OutOrStdout(), output.FormatTable(map[string]any{"components": rows}, cols, "components", nil))
}

func runInventoryComponentsShow(cmd *cobra.Command, args []string) error {
	format, err := findingDispositionOutputFormat(cmd)
	if err != nil {
		return err
	}
	client := api.NewClient("", "")
	defer client.Close()
	id := hexID(args[0])
	if id == "" {
		items, err := listComponents(client, args[0], false)
		if err != nil {
			return inventoryCatalogError(err)
		}
		if id, err = pickByName(items, args[0], "component"); err != nil {
			return err
		}
	}
	detail, err := client.Get(inventoryComponentsPath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return inventoryCatalogError(err)
	}
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(detail)+"\n")
	}
	component, _ := detail["component"].(map[string]any)
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", clean(getStr(component, "name")), clean(getStr(component, "kind")))
	for _, s := range getSlice(detail, "sources") {
		m, _ := s.(map[string]any)
		fmt.Fprintf(&b, "  source     %s %s\n", clean(getStr(m, "url")), clean(getStr(m, "path_prefix")))
	}
	for _, i := range getSlice(detail, "interfaces") {
		m, _ := i.(map[string]any)
		fmt.Fprintf(&b, "  %-10s %s %s\n", clean(getStr(m, "direction")), clean(getStr(m, "kind")), clean(getStr(m, "key")))
	}
	fmt.Fprintf(&b, "%d connections\n", len(getSlice(detail, "edges")))
	return output.WriteString(cmd.OutOrStdout(), b.String())
}

// withJSONErrors also prints a failure as a JSON error on stdout when the output is JSON,
// so scripts read its code, message and suggestion like the command's result.
func withJSONErrors(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := run(cmd, args)
		var cliErr *clierrors.CLIError
		if err == nil || !errors.As(err, &cliErr) {
			return err
		}
		if format, ferr := findingDispositionOutputFormat(cmd); ferr == nil && format == output.JSON {
			_ = output.WriteString(cmd.OutOrStdout(), clierrors.FormatErrorJSON(cliErr)+"\n")
		}
		return err
	}
}

// listComponents pages through every component matching name.
func listComponents(client *api.Client, name string, unassigned bool) ([]any, error) {
	items := []any{}
	params := map[string]any{"limit": 200}
	if name != "" {
		params["name"] = name
	}
	if unassigned {
		params["unassigned"] = true
	}
	for {
		page, err := client.Get(inventoryComponentsPath, params)
		if err != nil {
			return nil, err
		}
		items = append(items, getSlice(page, "items")...)
		next := getStr(page, "next")
		if next == "" || next == params["after"] {
			return items, nil
		}
		params["after"] = next
	}
}

// hexID is ref as the server's id form when ref is a UUID, else "".
func hexID(ref string) string {
	if !uuidRe.MatchString(ref) {
		return ""
	}
	return strings.ReplaceAll(strings.ToLower(ref), "-", "")
}

// pickByName is the id of the item named exactly ref; names are unique.
func pickByName(items []any, ref, kind string) (string, error) {
	for _, item := range items {
		m, _ := item.(map[string]any)
		if getStr(m, "name") == ref {
			return getStr(m, "id"), nil
		}
	}
	return "", &clierrors.CLIError{
		Code:       "NOT_FOUND",
		Message:    fmt.Sprintf("no %s is named %q", kind, ref),
		Suggestion: fmt.Sprintf("Run 'konvu inventory %ss list' for the exact names.", kind),
		ExitCode:   clierrors.ExitNotFound,
	}
}

func appsTable(apps []any) string {
	rows := make([]any, 0, len(apps))
	for _, a := range apps {
		m, _ := a.(map[string]any)
		rows = append(rows, map[string]any{
			"name":       clean(getStr(m, "name")),
			"product":    clean(getStr(m, "product")),
			"module":     clean(getStr(m, "module")),
			"components": len(getSlice(m, "components")),
			"origin":     clean(getStr(m, "origin")),
			"id":         getStr(m, "id"),
		})
	}
	cols := []string{"name", "product", "module", "components", "origin", "id"}
	return output.FormatTable(map[string]any{"apps": rows}, cols, "apps", nil)
}

// clean keeps server text from driving the terminal.
func clean(s string) string {
	return sanitizeGuardrailsBaselineText(s)
}

func readCatalogFile(path string) (map[string]any, error) {
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
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil || doc == nil {
		return nil, errors.New("expected a JSON object like the output of 'konvu inventory catalog get -o json'")
	}
	// A key left out would delete what it lists: every list is required.
	var problems []string
	for _, key := range []string{"products", "modules", "apps"} {
		if _, ok := doc[key].([]any); !ok {
			problems = append(problems, fmt.Sprintf("no %q list", key))
		}
	}
	if revision, _ := doc["revision"].(string); revision == "" {
		problems = append(problems, `no "revision"`)
	}
	var unknown []string
	for key := range doc {
		switch key {
		case "products", "modules", "apps", "unassigned", "revision":
		default:
			unknown = append(unknown, fmt.Sprintf("%q", key))
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		problems = append(problems, "unknown keys "+strings.Join(unknown, ", "))
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("the file has %s; apply replaces the whole catalog, so start from 'catalog get'", strings.Join(problems, "; "))
	}
	return doc, nil
}

// inventoryCatalogError maps a refused call to its next step.
func inventoryCatalogError(err error) error {
	return catalogCallError(err, false)
}

// catalogCallError maps a refused call; a 422 lists the file's problems on an apply.
func catalogCallError(err error, applying bool) error {
	var authErr *api.AuthenticationError
	if errors.As(err, &authErr) {
		return clierrors.NewAuthError(err.Error())
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return clierrors.NewAPIError(err.Error())
	}
	body := []byte(strings.TrimPrefix(apiErr.Message, "API error: "))
	switch apiErr.StatusCode {
	case 403:
		return &clierrors.CLIError{
			Code:       "FORBIDDEN",
			Message:    invariantDetail(apiErr),
			Suggestion: "Ask Konvu to enable products and apps from your code, or run 'konvu whoami' to check your company.",
			ExitCode:   clierrors.ExitAuthFailed,
		}
	case 409:
		return &clierrors.CLIError{
			Code:       "CATALOG_CHANGED",
			Message:    invariantDetail(apiErr),
			Suggestion: "Nothing was written. Run 'konvu inventory catalog get -o json' again and redo the edit on the new file.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	case 404:
		return &clierrors.CLIError{
			Code:       "NOT_FOUND",
			Message:    invariantDetail(apiErr),
			Suggestion: "Run 'konvu inventory catalog get' for the current apps and components.",
			ExitCode:   clierrors.ExitNotFound,
		}
	case 422:
		if !applying {
			return &clierrors.CLIError{
				Code:       "INVALID_ARGUMENTS",
				Message:    invariantDetail(apiErr),
				Suggestion: "Check the arguments against the command's --help.",
				ExitCode:   clierrors.ExitUsageError,
			}
		}
		var payload struct {
			Detail []any `json:"detail"`
		}
		problems := []string{}
		if json.Unmarshal(body, &payload) == nil {
			for _, d := range payload.Detail {
				switch v := d.(type) {
				case string:
					problems = append(problems, clean(v))
				case map[string]any:
					problems = append(problems, clean(fmt.Sprintf("%v: %v", v["loc"], v["msg"])))
				}
			}
		}
		if len(problems) == 0 {
			problems = append(problems, invariantDetail(apiErr))
		}
		return &clierrors.CLIError{
			Code:       "INVALID_CATALOG",
			Message:    "the catalog has problems, nothing was written:\n  " + strings.Join(problems, "\n  "),
			Suggestion: "Fix them in the file and apply again; 'konvu inventory catalog get -o json' shows the current catalog.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	return clierrors.NewAPIError(invariantDetail(apiErr))
}
