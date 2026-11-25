package cmd

import "fmt"

// graphInventoryPath is Guardrails' route for a repository's endpoints and data objects, through
// Konvu's proxy. One document holds both lists; each command edits one and leaves the other out.
const graphInventoryPath = "/services/guardrails/v1/graph/"

var (
	endpointsData = inventoryData("endpoints", "endpoints", "endpoint", `each endpoint group: name, description, location (file#symbol)
              and routes, each {"method", "path"}`, "data_objects")
	dataObjectsData = inventoryData("data-objects", "data_objects", "data object", `each data object (a schema): name, description, location
              (file#symbol) and fields, its field names such as "email"`, "endpoints")
)

// inventoryData is one list of a repository's inventory document, edited by
// 'konvu inventory <name>'.
func inventoryData(name, section, noun, items, other string) bookDataObject {
	what := "repository's " + noun + "s"
	nouns := noun + "s"
	return bookDataObject{
		name: name, command: "inventory " + name, what: what, section: section, byRepo: true,
		required: []string{section}, readOnly: []string{"provenance", "hidden"}, refused: []string{other},
		repoPath: func(id string) string { return graphInventoryPath + id + "/inventory" },
		notFound: fmt.Sprintf("Konvu lists a repository's %s once it has mapped it: map it from the dashboard's Inventory, then try again.", nouns),
		shown:    fmt.Sprintf("The Security Book, the dashboard and the PR check show these %s on their next read.", nouns),
		long: fmt.Sprintf(`A repository's %[1]s, as Konvu's mapping found them and as people edited
them, as one JSON document:

  revision    the version you read; apply is refused once it changed since
  %-11[2]s %[3]s
  provenance  who set each %[4]s a person added or changed: read-only,
              never sent
  hidden      the %[1]s people hid: read-only, never sent

--repo is the repository as owner/name, a repository URL, or its Konvu id.
What you apply is kept by every later mapping run, and the Security Book's
Inventory, the dashboard and Konvu's PR check read it.`, nouns, section, items, noun),
		applyLong: fmt.Sprintf(`Make the repository's %[1]s match --file (or stdin with '-'), a document
as 'konvu inventory %[2]s get' prints it, with your edits: "revision" and
%[3]q are required.

The repository then shows exactly the %[1]s listed. One left as Konvu's
mapping found it stays Konvu's; one you add or change is yours; one you leave
out is hidden. Later mapping runs keep yours and never bring a hidden one back:
list it again to show it. Names match ignoring case and punctuation, so
"User" and "user" are one %[4]s.

"revision" names the version the file was read from: once the %[1]s changed
since (someone else's edit, or a new mapping), apply is refused and writes
nothing. Get them again and redo the edit then. "provenance" and "hidden" are
read-only and never sent.

--dry-run lists the changes and writes nothing. Nothing is written either when
the file has a problem: every problem is listed.

Exit codes: 0 success, 1 the %[1]s changed since you read them or another
error, 2 invalid file, 3 not found or a server that does not support this yet,
4 not logged in, no access, a read-only key or the demo view`, nouns, name, section, noun),
	}
}

func init() {
	inventoryCmd.AddCommand(newBookDataCmd(endpointsData), newBookDataCmd(dataObjectsData))
}
