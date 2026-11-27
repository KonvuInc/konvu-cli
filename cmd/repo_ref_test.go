package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/spf13/cobra"
)

const (
	juiceShopID = "0190aaaa-0000-0000-0000-000000000001"
	juiceCTFID  = "0190aaaa-0000-0000-0000-000000000002"
	githubWebID = "0190aaaa-0000-0000-0000-000000000003"
	gitlabWebID = "0190aaaa-0000-0000-0000-000000000004"
	teamAPIID   = "0190aaaa-0000-0000-0000-000000000005"
)

func repoRefCoverage() invariantReply {
	return invariantReply{http.StatusOK, map[string]any{"repositories": repoRefRepositories()}}
}

func repoRefRepositories() []any {
	return []any{
		map[string]any{"id": juiceShopID, "url": "github:acmekonvu/juice-shop"},
		map[string]any{"id": juiceCTFID, "url": "github:acmekonvu/juice-shop-ctf"},
		map[string]any{"id": githubWebID, "url": "github:acme/web"},
		map[string]any{"id": gitlabWebID, "url": "gitlab:acme/web"},
		map[string]any{"id": teamAPIID, "url": "https://gitlab.example.com/acme/team/api"},
	}
}

func TestMatchRepository(t *testing.T) {
	byName := indexKonvuRepositories(repoRefRepositories())
	lookup := func() (map[string][]konvuRepository, error) { return byName, nil }
	for _, test := range []struct {
		ref, id, code, suggests string
	}{
		{ref: "acmekonvu/juice-shop", id: juiceShopID},
		// A bare name is one repository's even when another's name contains it.
		{ref: "juice-shop", id: juiceShopID},
		{ref: " Juice-Shop ", id: juiceShopID},
		{ref: "github:acmekonvu/juice-shop", id: juiceShopID},
		{ref: "https://github.com/AcmeKonvu/Juice-Shop.git", id: juiceShopID},
		{ref: strings.ToUpper(juiceShopID), id: juiceShopID},
		{ref: strings.ReplaceAll(juiceShopID, "-", ""), id: juiceShopID},
		{ref: "api", id: teamAPIID},
		{ref: "acme/team/api", id: teamAPIID},
		{ref: "gitlab:acme/web", id: gitlabWebID},
		{ref: "web", code: "AMBIGUOUS_REPOSITORY", suggests: "owner/name"},
		{ref: "acme/web", code: "AMBIGUOUS_REPOSITORY"},
		{ref: "juice", code: "REPOSITORY_NOT_FOUND", suggests: "github:acmekonvu/juice-shop, github:acmekonvu/juice-shop-ctf"},
		{ref: "acmekonvu/juice-shop-v2", code: "REPOSITORY_NOT_FOUND", suggests: "github:acmekonvu/juice-shop"},
		{ref: "nope", code: "REPOSITORY_NOT_FOUND", suggests: "konvu coverage list"},
		{ref: "0190aaaa-0000-0000-0000-0000000000ff", code: "REPOSITORY_NOT_FOUND"},
		{ref: "../juice-shop", code: "INVALID_ARGUMENTS"},
		{ref: "juice shop", code: "INVALID_ARGUMENTS"},
	} {
		repo, err := matchRepository(test.ref, lookup)
		if test.code == "" {
			if err != nil || repo.id != test.id {
				t.Errorf("%q: got %q, %v; want %q", test.ref, repo.id, err, test.id)
			}
			continue
		}
		var cliErr *clierrors.CLIError
		if !errors.As(err, &cliErr) || cliErr.Code != test.code || !strings.Contains(cliErr.Suggestion, test.suggests) {
			t.Errorf("%q: error = %#v, want %s suggesting %q", test.ref, err, test.code, test.suggests)
		}
	}
}

func TestFindingListFiltersByTheResolvedRepository(t *testing.T) {
	// counts asks once per assessment.
	pages := make([]invariantReply, 20)
	for i := range pages {
		pages[i] = invariantReply{http.StatusOK, map[string]any{"items": []any{}}}
	}
	_, calls := reviewServer(t, map[string][]invariantReply{
		"GET /repositories/assessment_config": {repoRefCoverage(), repoRefCoverage()},
		"GET /sca_findings":                   pages,
	})
	list := &cobra.Command{Use: "list", RunE: scaListCmd.RunE}
	addSCAListFlags(list)
	if _, err := runInvariants(list, "-r", "juice-shop", "-q"); err != nil {
		t.Fatal(err)
	}
	counts := &cobra.Command{Use: "counts", RunE: scaCountsCmd.RunE}
	copyFlagsFrom(counts, scaCountsCmd)
	if _, err := runInvariants(counts, "--repo", "https://github.com/acmekonvu/juice-shop", "-o", "json"); err != nil {
		t.Fatal(err)
	}
	listed := 0
	for _, call := range calls() {
		if call.path != "/sca_findings" {
			continue
		}
		listed++
		query, _ := url.ParseQuery(call.query)
		if query.Get("vcs_repository_id") != juiceShopID || query.Has("vcs_repository_url") {
			t.Errorf("query = %v", query)
		}
	}
	if listed < 2 {
		t.Errorf("%d finding requests", listed)
	}
}

// TestFindingListRefusesAnUnknownRepository runs the command in a subprocess: the finding
// error handler exits the process.
func TestFindingListRefusesAnUnknownRepository(t *testing.T) {
	if ref := os.Getenv("KONVU_TEST_REPO_REF"); ref != "" {
		list := &cobra.Command{Use: "list", RunE: scaListCmd.RunE}
		addSCAListFlags(list)
		list.SetArgs([]string{"-r", ref, "-o", "json"})
		_ = list.Execute()
		os.Exit(0)
	}
	for _, test := range []struct {
		ref, code string
		exit      int
	}{
		{"juice", "REPOSITORY_NOT_FOUND", clierrors.ExitNotFound},
		{"web", "AMBIGUOUS_REPOSITORY", clierrors.ExitUsageError},
	} {
		// Any /sca_findings call fails the test: the list must not run unfiltered or empty.
		reviewServer(t, map[string][]invariantReply{"GET /repositories/assessment_config": {repoRefCoverage()}})
		command := exec.Command(os.Args[0], "-test.run=^TestFindingListRefusesAnUnknownRepository$")
		command.Env = append(os.Environ(), "KONVU_TEST_REPO_REF="+test.ref)
		out, _ := command.Output()
		if command.ProcessState == nil || command.ProcessState.ExitCode() != test.exit {
			t.Fatalf("%s: exit = %v, output %s", test.ref, command.ProcessState, out)
		}
		var answer map[string]any
		if err := json.Unmarshal(out, &answer); err != nil || getStr(getMap(answer, "error"), "code") != test.code {
			t.Errorf("%s: output = %s", test.ref, out)
		}
	}
}

func TestSastFiltersByTheResolvedRepositories(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		// One lookup serves every --repo.
		"GET /repositories/assessment_config": {repoRefCoverage()},
		"GET /detections":                     {{http.StatusOK, map[string]any{"items": []any{}}}},
	})
	list := &cobra.Command{Use: "list", RunE: runSastList}
	addSastListFlags(list)
	list.SetOut(&bytes.Buffer{})
	if _, err := runInvariants(list, "-r", "juice-shop", "--repo", "gitlab:acme/web", "-r", "acmekonvu/juice-shop", "-o", "json"); err != nil {
		t.Fatal(err)
	}
	last := calls()[len(calls())-1]
	query, _ := url.ParseQuery(last.query)
	if got := query["vcs_repository_id"]; len(got) != 2 || got[0] != juiceShopID || got[1] != gitlabWebID || query.Has("where") {
		t.Errorf("query = %v", query)
	}

	reviewServer(t, map[string][]invariantReply{"GET /repositories/assessment_config": {repoRefCoverage()}})
	list = &cobra.Command{Use: "list", RunE: runSastList}
	addSastListFlags(list)
	_, err := runInvariants(list, "-r", "web", "-o", "json")
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.Code != "AMBIGUOUS_REPOSITORY" {
		t.Errorf("error = %#v", err)
	}
}

func TestDismissFiltersByTheResolvedRepository(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		"GET /repositories/assessment_config": {repoRefCoverage(), repoRefCoverage()},
		"GET /sca_findings":                   {{http.StatusOK, map[string]any{"items": []any{}}}},
	})
	dismiss := &cobra.Command{Use: "dismiss", RunE: runDismiss}
	addDismissFlags(dismiss)
	if _, err := runInvariants(dismiss, "--assessment", "false-positive", "-r", "juice-shop", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	query, _ := url.ParseQuery(calls()[1].query)
	if query.Get("vcs_repository_id") != juiceShopID {
		t.Errorf("query = %v", query)
	}
	// A --repo naming no repository dismisses nothing.
	dismiss = &cobra.Command{Use: "dismiss", RunE: runDismiss}
	addDismissFlags(dismiss)
	_, err := runInvariants(dismiss, "--assessment", "false-positive", "-r", "juice")
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.Code != "REPOSITORY_NOT_FOUND" {
		t.Errorf("error = %#v", err)
	}
}

func TestPRCheckRepositoryFilter(t *testing.T) {
	for _, test := range []struct{ ref, want string }{
		{"acme/app", "acme/app"},
		{"https://github.com/Acme/App", "Acme/App"},
		{"github:acme/app", "acme/app"},
	} {
		// An owner/name or URL is read as given, with no lookup.
		reviewServer(t, map[string][]invariantReply{})
		got, err := prCheckRepository(nil, test.ref)
		if err != nil || got != test.want {
			t.Errorf("%q: got %q, %v", test.ref, got, err)
		}
	}
	reviewServer(t, map[string][]invariantReply{"GET /repositories/assessment_config": {repoRefCoverage()}})
	client := api.NewClient("", "")
	if got, err := prCheckRepository(client, "juice-shop"); err != nil || got != "acmekonvu/juice-shop" {
		t.Errorf("juice-shop: got %q, %v", got, err)
	}
	if _, err := prCheckRepository(nil, "../x"); err == nil {
		t.Error("../x: no error")
	}
}
