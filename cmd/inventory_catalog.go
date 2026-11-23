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
	"text/tabwriter"

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

  products            each product and its modules
  products_confirmed  whether a person confirmed the products and modules
  modules             modules of no product
  apps                each app (a running system): its product, module, summary,
                      the components it holds, by name, and whether a person
                      confirmed it
  excluded            components that are not part of what you run (a test
                      target, a fixture): Konvu puts them in no app
  unassigned          components in no app (read only)

Each app is suggested (Konvu grouped it), edited (a person changed it) or
confirmed (a person said it is right). Konvu builds the catalog from your code.
To change it, get it, edit the file, and apply it: a dry run lists every change
first. Products, modules and apps you change are kept by later rebuilds, and the
components you place in an app stay there.`,
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
one of that product's modules; a file that removes a product or moves a module
cannot leave an app on it. Components are named as in 'catalog get'; a component
may sit in several apps. "origin" and "unassigned" are ignored.

Confirming: set an app's "confirmed" to true, or "products_confirmed" to true.
An edit undoes an app's confirmation, so confirm it again in a second apply. Set
"confirmed" to false to undo one.

Excluding: list components in "excluded" to keep them out of every app; remove
one from the list to let Konvu group it again. Without the "excluded" key,
exclusions stay as they are.

--dry-run lists the changes and writes nothing. Nothing is written either when the
file has a problem: every problem is listed.

Exit codes: 0 success, 1 general error, 2 invalid file, 3 not found, 4 auth failed or not enabled`,
		Example: `  konvu inventory catalog apply --file catalog.json --dry-run
  konvu inventory catalog get -o json | jq '...' | konvu inventory catalog apply --file -
  konvu inventory catalog get -o json \
    | jq '(.apps[] | select(.name == "Billing")).confirmed = true' \
    | konvu inventory catalog apply --file - --dry-run`,
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
		Example: `  konvu inventory apps list --product Acme
  konvu inventory apps list --status suggested`,
		Args: cobra.NoArgs,
		RunE: withJSONErrors(runInventoryAppsList),
	}
	list.Flags().String("product", "", "Only apps of this product; 'none' for apps of no product")
	list.Flags().String("status", "", "Only apps with this status: suggested, edited, confirmed")
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
	return output.WriteString(cmd.OutOrStdout(), catalogTree(catalog))
}

// catalogTree prints products, their modules, their apps and each app's
// components, then what sits outside them, e.g.
//
//	Acme
//	  module Billing
//	    Billing  suggested  1 component
//	      acme/api
func catalogTree(catalog map[string]any) string {
	var b strings.Builder
	status := "suggested by Konvu"
	if catalog["products_confirmed"] == true {
		status = "confirmed"
	}
	fmt.Fprintf(&b, "Products (%s)\n\n", status)
	apps := getSlice(catalog, "apps")
	// How many apps hold each component, to flag the shared ones.
	holders := map[string]int{}
	for _, a := range apps {
		m, _ := a.(map[string]any)
		for _, c := range stringsOf(getSlice(m, "components")) {
			holders[c]++
		}
	}
	printApps := func(indent, product, module string) {
		for _, a := range apps {
			m, _ := a.(map[string]any)
			if getStr(m, "product") != product || getStr(m, "module") != module {
				continue
			}
			components := stringsOf(getSlice(m, "components"))
			fmt.Fprintf(&b, "%s%s  %s  %s\n", indent, clean(getStr(m, "name")), appStatus(m), plural(len(components), "component"))
			for _, c := range components {
				shared := ""
				if holders[c] > 1 {
					shared = fmt.Sprintf("  (in %d apps)", holders[c])
				}
				fmt.Fprintf(&b, "%s  %s%s\n", indent, clean(c), shared)
			}
		}
	}
	// A product's declared modules first, then any other module its apps name;
	// one outside allowed is flagged.
	printProduct := func(title, product string, declared, allowed []string) {
		fmt.Fprintf(&b, "%s\n", title)
		for _, module := range declared {
			fmt.Fprintf(&b, "  module %s\n", clean(module))
			printApps("    ", product, module)
		}
		for _, module := range modulesOf(apps, product) {
			if module == "" || containsString(declared, module) {
				continue
			}
			if containsString(allowed, module) {
				fmt.Fprintf(&b, "  module %s\n", clean(module))
			} else {
				fmt.Fprintf(&b, "  module %s (not a module of this product)\n", clean(module))
			}
			printApps("    ", product, module)
		}
		printApps("  ", product, "")
		b.WriteString("\n")
	}
	listed := map[string]bool{}
	for _, p := range getSlice(catalog, "products") {
		m, _ := p.(map[string]any)
		name := getStr(m, "name")
		listed[name] = true
		modules := stringsOf(getSlice(m, "modules"))
		printProduct(clean(name), name, modules, modules)
	}
	// Apps of no product, or of a product the list no longer names.
	var orphans []string
	for _, a := range apps {
		m, _ := a.(map[string]any)
		if p := getStr(m, "product"); !listed[p] && !containsString(orphans, p) {
			orphans = append(orphans, p)
		}
	}
	for _, p := range orphans {
		title, allowed := "No product", stringsOf(getSlice(catalog, "modules"))
		if p != "" {
			title, allowed = fmt.Sprintf("%s (not a listed product)", clean(p)), nil
		}
		printProduct(title, p, nil, allowed)
	}
	if modules := stringsOf(getSlice(catalog, "modules")); len(modules) > 0 {
		fmt.Fprintf(&b, "Modules of no product: %s\n", clean(strings.Join(modules, ", ")))
	}
	if excluded := stringsOf(getSlice(catalog, "excluded")); len(excluded) > 0 {
		fmt.Fprintf(&b, "Excluded from apps: %s\n", clean(strings.Join(excluded, ", ")))
	}
	fmt.Fprintf(&b, "%s in no app: 'konvu inventory components list --unassigned' lists them\n",
		plural(len(getSlice(catalog, "unassigned")), "component"))
	return b.String()
}

// appStatus reads as the web does: suggested, edited or confirmed.
func appStatus(app map[string]any) string {
	switch {
	case app["confirmed"] == true:
		return "confirmed"
	case getStr(app, "origin") == "user":
		return "edited"
	default:
		return "suggested"
	}
}

// modulesOf lists the modules the apps of product use, in first-seen order.
func modulesOf(apps []any, product string) []string {
	var modules []string
	for _, a := range apps {
		m, _ := a.(map[string]any)
		if getStr(m, "product") == product && !containsString(modules, getStr(m, "module")) {
			modules = append(modules, getStr(m, "module"))
		}
	}
	return modules
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
	product, _ := cmd.Flags().GetString("product")
	status, _ := cmd.Flags().GetString("status")
	if status != "" && !containsString([]string{"suggested", "edited", "confirmed"}, status) {
		return &clierrors.CLIError{
			Code:       "INVALID_ARGUMENTS",
			Message:    fmt.Sprintf("unknown status %q", status),
			Suggestion: "Use suggested, edited or confirmed.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	apps := []any{}
	for _, a := range getSlice(catalog, "apps") {
		m, _ := a.(map[string]any)
		if product != "" && !sameProduct(getStr(m, "product"), product) {
			continue
		}
		if status != "" && appStatus(m) != status {
			continue
		}
		apps = append(apps, a)
	}
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(apps)+"\n")
	}
	return output.WriteString(cmd.OutOrStdout(), appsTable(apps))
}

// sameProduct matches an app's product to --product, case-insensitively;
// "none" matches apps of no product.
func sameProduct(product, want string) bool {
	if strings.EqualFold(want, "none") {
		return product == ""
	}
	return strings.EqualFold(product, want)
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
	for _, field := range []string{"product", "module", "summary"} {
		if v := getStr(app, field); v != "" {
			fmt.Fprintf(&b, "  %-8s %s\n", field, clean(v))
		}
	}
	fmt.Fprintf(&b, "  %-8s %s\n", "status", appStatus(app))
	rows := [][]string{}
	for _, m := range getSlice(detail, "members") {
		member, _ := m.(map[string]any)
		rows = append(rows, []string{clean(getStr(member, "name")), clean(getStr(member, "kind"))})
	}
	b.WriteString("\n")
	b.WriteString(alignedTable([]string{"Component", "Kind"}, rows))
	b.WriteString(connections(detail))
	return output.WriteString(cmd.OutOrStdout(), b.String())
}

// connections lists a detail's edges by component name, e.g.
// "acme/web  calls  acme/api  http, internet".
func connections(detail map[string]any) string {
	names := map[string]string{}
	for _, key := range []string{"nodes", "members"} {
		for _, n := range getSlice(detail, key) {
			m, _ := n.(map[string]any)
			names[getStr(m, "id")] = getStr(m, "name")
		}
	}
	if c, ok := detail["component"].(map[string]any); ok {
		names[getStr(c, "id")] = getStr(c, "name")
	}
	name := func(id, fallback string) string {
		if n := names[id]; n != "" {
			return n
		}
		if fallback != "" {
			return fallback
		}
		return id
	}
	edges := getSlice(detail, "edges")
	if len(edges) == 0 {
		return "\nNo connections\n"
	}
	rows := make([][]string, 0, len(edges))
	for _, e := range edges {
		m, _ := e.(map[string]any)
		var how []string
		for _, key := range []string{"protocol", "exposure"} {
			if v := getStr(m, key); v != "" {
				how = append(how, v)
			}
		}
		rows = append(rows, []string{
			clean(name(getStr(m, "source_component_id"), "")),
			clean(getStr(m, "kind")),
			clean(name(getStr(m, "target_component_id"), getStr(m, "target_name"))),
			clean(strings.Join(how, ", ")),
		})
	}
	note := ""
	if detail["edges_truncated"] == true {
		note = " (more not shown)"
	}
	return fmt.Sprintf("\n%s%s\n", plural(len(edges), "connection"), note) +
		alignedTable([]string{"From", "Kind", "To", "Via"}, rows)
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
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		m, _ := item.(map[string]any)
		var apps []string
		for _, a := range getSlice(m, "apps") {
			app, _ := a.(map[string]any)
			apps = append(apps, getStr(app, "name"))
		}
		rows = append(rows, []string{
			clean(getStr(m, "name")),
			clean(getStr(m, "kind")),
			fmt.Sprint(intOf(m["connections"])),
			clean(strings.Join(apps, ", ")),
		})
	}
	headers := []string{"Name", "Kind", "Connections", "Apps"}
	return output.WriteString(cmd.OutOrStdout(), alignedTable(headers, rows))
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
	if summary := getStr(component, "summary"); summary != "" {
		fmt.Fprintf(&b, "  summary    %s\n", clean(summary))
	}
	var apps []string
	for _, a := range getSlice(component, "apps") {
		app, _ := a.(map[string]any)
		apps = append(apps, getStr(app, "name"))
	}
	if len(apps) == 0 {
		apps = []string{"none"}
	}
	fmt.Fprintf(&b, "  apps       %s\n", clean(strings.Join(apps, ", ")))
	for _, s := range getSlice(detail, "sources") {
		m, _ := s.(map[string]any)
		fmt.Fprintf(&b, "  source     %s %s\n", clean(getStr(m, "url")), clean(getStr(m, "path_prefix")))
	}
	for _, i := range getSlice(detail, "interfaces") {
		m, _ := i.(map[string]any)
		fmt.Fprintf(&b, "  %-10s %s %s\n", clean(getStr(m, "direction")), clean(getStr(m, "kind")), clean(getStr(m, "key")))
	}
	b.WriteString(connections(detail))
	return output.WriteString(cmd.OutOrStdout(), b.String())
}

// withJSONErrors keeps the usage text out of a failed run, and also prints a failure as a JSON error on stdout when the output is JSON,
// so scripts read its code, message and suggestion like the command's result.
func withJSONErrors(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// Arguments parsed: a failure from here on is not a usage mistake.
		cmd.SilenceUsage = true
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

// pickByName is the id of the item named ref: exactly, else in any case. Names
// are unique; when none matches, the names containing ref are suggested.
func pickByName(items []any, ref, kind string) (string, error) {
	var folded, similar []string
	ids := map[string]string{}
	for _, item := range items {
		m, _ := item.(map[string]any)
		name := getStr(m, "name")
		if name == ref {
			return getStr(m, "id"), nil
		}
		ids[name] = getStr(m, "id")
		if strings.EqualFold(name, ref) {
			folded = append(folded, name)
		} else if strings.Contains(strings.ToLower(name), strings.ToLower(ref)) {
			similar = append(similar, name)
		}
	}
	if len(folded) == 1 {
		return ids[folded[0]], nil
	}
	suggestion := fmt.Sprintf("Run 'konvu inventory %ss list' for the exact names.", kind)
	if similar = append(folded, similar...); len(similar) > 0 {
		sort.Strings(similar)
		if len(similar) > 5 {
			similar = similar[:5]
		}
		suggestion = "Did you mean: " + clean(strings.Join(similar, ", ")) + "?"
	}
	return "", &clierrors.CLIError{
		Code:       "NOT_FOUND",
		Message:    fmt.Sprintf("no %s is named %q", kind, ref),
		Suggestion: suggestion,
		ExitCode:   clierrors.ExitNotFound,
	}
}

func appsTable(apps []any) string {
	rows := make([][]string, 0, len(apps))
	for _, a := range apps {
		m, _ := a.(map[string]any)
		rows = append(rows, []string{
			clean(getStr(m, "name")),
			appStatus(m),
			clean(getStr(m, "product")),
			clean(getStr(m, "module")),
			fmt.Sprint(len(getSlice(m, "components"))),
		})
	}
	return alignedTable([]string{"Name", "Status", "Product", "Module", "Components"}, rows)
}

// alignedTable lines columns up without cutting any cell: names are what a
// reader matches against the catalog file, so they print whole.
func alignedTable(headers []string, rows [][]string) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	_ = w.Flush()
	return b.String()
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
		case "products", "modules", "apps", "unassigned", "revision", "excluded", "products_confirmed":
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
