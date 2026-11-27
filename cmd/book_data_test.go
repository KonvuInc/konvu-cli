package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

const (
	testRepoID      = "0190aaaa-bbbb-cccc-dddd-eeeeffff0001"
	otherTestRepoID = "0190aaaa-bbbb-cccc-dddd-eeeeffff0002"
	companyDocument = `{"revision": "r7", "company": {"industry": "Payroll", "employees": 12345678901234567890, "customer_segments": null},
		"infrastructure": {"hosting_footprint": ["AWS"]}, "provenance": {"industry": {"provenance": "agent", "asserted_by": null}}}`
)

func bookDataCLIError(t *testing.T, err error) *clierrors.CLIError {
	t.Helper()
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("error = %#v, want a CLIError", err)
	}
	return cliErr
}

func TestCompanyGetPrintsTheDocumentAsKonvuHoldsIt(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{
		"GET /company_profile/facts/company": {{http.StatusOK, json.RawMessage(strings.Replace(companyDocument, `"Payroll"`, "\"Payroll\u009b2J\"", 1))}},
	})
	out, err := runInvariants(newBookDataCmd(companyData), "get")
	if err != nil {
		t.Fatal(err)
	}
	// A large number prints as it was sent, never rounded: apply sends it back unchanged.
	if !strings.Contains(out, `"employees": 12345678901234567890`) || !strings.Contains(out, `"provenance"`) {
		t.Errorf("out = %s", out)
	}
	if strings.ContainsRune(out, '\u009b') || !strings.Contains(out, `Payroll\u009b2J`) {
		t.Errorf("a C1 control character reached the terminal: %q", out)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Errorf("not JSON: %v", err)
	}
}

func TestCompanyGetRefusesAnotherFormat(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{})
	_, err := runInvariants(newBookDataCmd(companyData), "get", "-o", "table")
	if cliErr := bookDataCLIError(t, err); cliErr.ExitCode != clierrors.ExitUsageError {
		t.Errorf("exit %d", cliErr.ExitCode)
	}
	if len(calls()) != 0 {
		t.Errorf("calls %v", calls())
	}
}

func TestCompanyApplySendsTheFileWithoutProvenance(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		"PUT /company_profile/facts/company": {{http.StatusOK, map[string]any{
			"applied": true, "changes": []any{"Set customer_segments: (none) → banks\u001b[2J"}, "revision": "r8",
		}}},
	})
	file := writeInvariants(t, companyDocument)
	out, err := runInvariants(newBookDataCmd(companyData), "apply", "--file", file, "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	call := calls()[0]
	if call.query != "dry_run=false" {
		t.Errorf("query = %q", call.query)
	}
	if _, sent := call.body["provenance"]; sent {
		t.Errorf("provenance was sent: %v", call.body)
	}
	company, _ := call.body["company"].(map[string]any)
	if call.body["revision"] != "r7" || company["industry"] != "Payroll" || call.body["infrastructure"] == nil {
		t.Errorf("body = %v", call.body)
	}
	for _, want := range []string{"Applied 1 change:\n", "Set customer_segments: (none) → banks [2J", "Now at revision r8."} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("a control character reached the terminal: %q", out)
	}
}

// A large number goes back exactly as read, so a field the person did not touch is not changed.
func TestCompanyApplyKeepsNumbersExact(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"applied": false, "changes": [], "revision": "r7"}`))
	}))
	defer server.Close()
	invariantsEnv(t, server)
	out, err := runInvariants(newBookDataCmd(companyData), "apply", "--file", writeInvariants(t, companyDocument), "--dry-run", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"employees":12345678901234567890`) {
		t.Errorf("body = %s", body)
	}
	if !strings.Contains(out, "No changes: the file matches your company facts. Nothing was written.") {
		t.Errorf("out = %q", out)
	}
}

func TestCompanyApplyDryRun(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		"PUT /company_profile/facts/company": {{http.StatusOK, map[string]any{
			"applied": false, "changes": []any{"Set hosting_footprint: AWS → GCP", "Set industry: Payroll → HR"}, "revision": "r7",
		}}},
	})
	file := writeInvariants(t, companyDocument)
	out, err := runInvariants(newBookDataCmd(companyData), "apply", "--file", file, "--dry-run", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if calls()[0].query != "dry_run=true" {
		t.Errorf("query = %q", calls()[0].query)
	}
	if !strings.Contains(out, "Would apply 2 changes (dry run, nothing written):\n  Set hosting_footprint: AWS → GCP\n  Set industry: Payroll → HR\n") {
		t.Errorf("out = %q", out)
	}
	if strings.Contains(out, "revision") {
		t.Errorf("a dry run claims a new revision: %q", out)
	}
}

func TestBookDataApplyRefusals(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   any
		code   string
		exit   int
		want   string
	}{
		{"stale revision", http.StatusConflict, map[string]any{"detail": "The facts changed since revision r7\u001b[2J"}, "DATA_CHANGED", clierrors.ExitGeneralError, "changed since the file was read"},
		{"validation", http.StatusUnprocessableEntity, map[string]any{"detail": []any{
			map[string]any{"loc": []any{"body", "company", "employees"}, "msg": "Input should be a valid string"},
			"infrastructure.cloud: unknown fact \"x\u001b[2J\"",
		}}, "INVALID_DATA", clierrors.ExitUsageError, "company.employees: Input should be a valid string\n  infrastructure.cloud: unknown fact"},
		{"demo view", http.StatusForbidden, map[string]any{"detail": "Only a user of the company can edit its facts"}, "FORBIDDEN", clierrors.ExitAuthFailed, "read-only"},
		{"older server", http.StatusNotFound, map[string]any{"detail": "Not Found"}, "DATA_EDITS_UNAVAILABLE", clierrors.ExitNotFound, "doesn't support editing your company facts yet"},
		{"no PUT route", http.StatusMethodNotAllowed, map[string]any{"detail": "Method Not Allowed"}, "DATA_EDITS_UNAVAILABLE", clierrors.ExitNotFound, "yet"},
		{"logged out", http.StatusUnauthorized, map[string]any{"detail": "expired\u001b[2J"}, "AUTH_FAILED", clierrors.ExitAuthFailed, "expired"},
		// A server error can come after the commit: never "nothing was written".
		{"server error", http.StatusGatewayTimeout, map[string]any{"detail": "upstream timed out\u001b[2J"}, "OUTCOME_UNKNOWN", clierrors.ExitGeneralError, "may have been applied"},
		{"lost connection", dropConnection, nil, "OUTCOME_UNKNOWN", clierrors.ExitGeneralError, "may have been applied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewServer(t, map[string][]invariantReply{
				"PUT /company_profile/facts/company": {{test.status, test.body}},
			})
			file := writeInvariants(t, companyDocument)
			_, err := runInvariants(newBookDataCmd(companyData), "apply", "--file", file, "-o", "table")
			cliErr := bookDataCLIError(t, err)
			if cliErr.Code != test.code || cliErr.ExitCode != test.exit || !strings.Contains(cliErr.Message, test.want) {
				t.Errorf("error = %#v", cliErr)
			}
			if strings.Contains(cliErr.Message, "\x1b") {
				t.Errorf("a control character reached the message: %q", cliErr.Message)
			}
		})
	}
}

func TestBookDataApplyRefusesAPartialFileBeforeAnyRequest(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{})
	for name, body := range map[string]string{
		"no revision":       `{"company": {}, "infrastructure": {}}`,
		"null revision":     `{"revision": null, "company": {}, "infrastructure": {}}`,
		"empty revision":    `{"revision": " ", "company": {}, "infrastructure": {}}`,
		"number revision":   `{"revision": 7, "company": {}, "infrastructure": {}}`,
		"no infrastructure": `{"revision": "r7", "company": {}}`,
		"null company":      `{"revision": "r7", "company": null, "infrastructure": {}}`,
		"a list":            `[{"revision": "r7"}]`,
		"null":              `null`,
		"two documents":     `{"revision": "r7", "company": {}, "infrastructure": {}} {}`,
		"not JSON":          `revision: r7`,
	} {
		file := writeInvariants(t, body)
		_, err := runInvariants(newBookDataCmd(companyData), "apply", "--file", file)
		if cliErr := bookDataCLIError(t, err); cliErr.Code != "INVALID_DATA" || cliErr.ExitCode != clierrors.ExitUsageError {
			t.Errorf("%s: error = %#v", name, cliErr)
		}
	}
	if _, err := runInvariants(newBookDataCmd(companyData), "apply"); bookDataCLIError(t, err).ExitCode != clierrors.ExitUsageError {
		t.Errorf("no --file: %v", err)
	}
	if len(calls()) != 0 {
		t.Errorf("calls %v", calls())
	}
}

// An answer that does not show the write as asked is never reported as success.
func TestBookDataApplyChecksTheAnswer(t *testing.T) {
	for _, test := range []struct {
		name   string
		dryRun bool
		answer map[string]any
		code   string
	}{
		{"applied on a dry run", true, map[string]any{"applied": true, "changes": []any{"Set industry: Payroll → HR"}, "revision": "r8"}, "DRY_RUN_APPLIED"},
		{"not applied", false, map[string]any{"applied": false, "changes": []any{"x"}, "revision": "r7"}, "NOT_APPLIED"},
		{"no applied field", false, map[string]any{"revision": "r7"}, "UNEXPECTED_ANSWER"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reviewServer(t, map[string][]invariantReply{
				"PUT /company_profile/facts/company": {{http.StatusOK, test.answer}},
			})
			args := []string{"apply", "--file", writeInvariants(t, companyDocument), "-o", "table"}
			if test.dryRun {
				args = append(args, "--dry-run")
			}
			out, err := runInvariants(newBookDataCmd(companyData), args...)
			if cliErr := bookDataCLIError(t, err); cliErr.Code != test.code || cliErr.ExitCode != clierrors.ExitGeneralError {
				t.Errorf("error = %#v", cliErr)
			}
			// Nothing prints as if the save, or the preview, had worked: the error says what happened.
			if out != "" {
				t.Errorf("printed %q", out)
			}
			// With -o json, stdout is one document: the error, carrying the answer.
			reviewServer(t, map[string][]invariantReply{
				"PUT /company_profile/facts/company": {{http.StatusOK, test.answer}},
			})
			out, _ = runInvariants(newBookDataCmd(companyData), append(args, "-o", "json")...)
			var printed struct {
				Error struct {
					Code    string         `json:"code"`
					Context map[string]any `json:"context"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(out), &printed); err != nil || printed.Error.Code != test.code || printed.Error.Context["changes"] == nil {
				t.Errorf("-o json printed %q (%v)", out, err)
			}
		})
	}
}

// Every key but the read-only provenance and overrides goes back as the file has it, so keys
// this CLI does not know survive.
func TestThreatModelApplySendsEveryOtherKeyBack(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		"GET /company_profile/facts/threat-model": {{http.StatusOK, json.RawMessage(`{"revision": "r3", "threat_model": {"attackers_of_interest": ["fraudsters"]}, "assumptions": {"a": 1}, "provenance": {}, "overrides": {"assets": {"added": [{"value": "billing", "asserted_by": "a@acme.com"}], "hidden": []}}}`)}},
		"PUT /company_profile/facts/threat-model": {{http.StatusOK, map[string]any{"applied": true, "changes": []any{"Set attackers_of_interest: fraudsters → fraudsters, insiders"}, "revision": "r4"}}},
	})
	out, err := runInvariants(newBookDataCmd(threatModelData), "get")
	if err != nil || !strings.Contains(out, `"assumptions"`) {
		t.Fatalf("get: %v, %s", err, out)
	}
	edited := strings.Replace(out, `"fraudsters"`, `"fraudsters", "insiders"`, 1)
	if _, err := runInvariants(newBookDataCmd(threatModelData), "apply", "--file", writeInvariants(t, edited), "-o", "json"); err != nil {
		t.Fatal(err)
	}
	sent := calls()[1].body
	if _, ok := sent["assumptions"].(map[string]any); !ok || sent["provenance"] != nil || sent["overrides"] != nil || len(sent) != 3 {
		t.Errorf("body = %v", sent)
	}
	attackers, _ := sent["threat_model"].(map[string]any)["attackers_of_interest"].([]any)
	if len(attackers) != 2 {
		t.Errorf("threat_model = %v", sent["threat_model"])
	}
	if _, err := runInvariants(newBookDataCmd(threatModelData), "apply", "--file", writeInvariants(t, `{"revision": "r3"}`)); bookDataCLIError(t, err).Code != "INVALID_DATA" {
		t.Errorf("a file with no threat_model was not refused: %v", err)
	}
}

func threatProfileCoverage() invariantReply {
	return invariantReply{http.StatusOK, map[string]any{"repositories": []any{
		map[string]any{"id": testRepoID, "url": "https://github.com/acme/api"},
		map[string]any{"id": otherTestRepoID, "url": "https://gitlab.com/acme/web"},
		map[string]any{"id": "0190aaaa-bbbb-cccc-dddd-eeeeffff0003", "url": "https://github.com/acme/web"},
	}}}
}

func TestThreatProfileResolvesTheRepository(t *testing.T) {
	attributes := "GET /threat_profile/repositories/" + testRepoID + "/attributes"
	_, calls := reviewServer(t, map[string][]invariantReply{
		"GET /repositories/assessment_config": {threatProfileCoverage(), threatProfileCoverage(), threatProfileCoverage()},
		attributes:                            {{http.StatusOK, json.RawMessage(`{"revision": "r2", "attributes": {"internet_exposed": true}}`)}, {http.StatusOK, json.RawMessage(`{"revision": "r2", "attributes": {}}`)}, {http.StatusOK, json.RawMessage(`{"revision": "r2", "attributes": {}}`)}},
		"PUT /threat_profile/repositories/" + testRepoID + "/attributes": {{http.StatusOK, map[string]any{"applied": true, "changes": []any{"Set internet_exposed: yes → no"}, "revision": "r3"}}},
	})
	out, err := runInvariants(newBookDataCmd(threatProfileData), "get", "--repo", "github:Acme/API")
	if err != nil || !strings.Contains(out, `"internet_exposed": true`) {
		t.Fatalf("get: %v, %s", err, out)
	}
	// An id goes straight to the route, with no lookup.
	if _, err := runInvariants(newBookDataCmd(threatProfileData), "get", "--repo", strings.ToUpper(testRepoID)); err != nil {
		t.Fatal(err)
	}
	if _, err := runInvariants(newBookDataCmd(threatProfileData), "get", "--repo", "https://github.com/acme/api.git"); err != nil {
		t.Fatal(err)
	}
	file := writeInvariants(t, `{"revision": "r2", "attributes": {"internet_exposed": false}}`)
	out, err = runInvariants(newBookDataCmd(threatProfileData), "apply", "--repo", "acme/api", "--file", file, "-o", "table")
	if err != nil || !strings.Contains(out, "Set internet_exposed: yes → no") {
		t.Fatalf("apply: %v, %s", err, out)
	}
	if got := len(calls()); got != 7 {
		t.Errorf("%d calls: %v", got, calls())
	}
}

func TestThreatProfileRepositoryRefusals(t *testing.T) {
	for _, test := range []struct {
		repo string
		code string
		exit int
	}{
		{"", "INVALID_ARGUMENTS", clierrors.ExitUsageError},
		{"../../admin", "INVALID_ARGUMENTS", clierrors.ExitUsageError},
		{"github:", "INVALID_ARGUMENTS", clierrors.ExitUsageError},
		// A bare name is looked up like owner/name: no repository is named acme.
		{"acme", "REPOSITORY_NOT_FOUND", clierrors.ExitNotFound},
		{"acme/missing", "REPOSITORY_NOT_FOUND", clierrors.ExitNotFound},
		// acme/web is on GitHub and on GitLab: only naming where it lives picks one.
		{"acme/web", "AMBIGUOUS_REPOSITORY", clierrors.ExitUsageError},
		{"web", "AMBIGUOUS_REPOSITORY", clierrors.ExitUsageError},
		// acme/api is only on GitHub: a GitLab URL is not it.
		{"https://gitlab.com/acme/api", "REPOSITORY_NOT_FOUND", clierrors.ExitNotFound},
		{"gitlab:acme/api", "REPOSITORY_NOT_FOUND", clierrors.ExitNotFound},
	} {
		reviewServer(t, map[string][]invariantReply{
			"GET /repositories/assessment_config": {threatProfileCoverage()},
		})
		_, err := runInvariants(newBookDataCmd(threatProfileData), "get", "--repo", test.repo)
		if cliErr := bookDataCLIError(t, err); cliErr.Code != test.code || cliErr.ExitCode != test.exit {
			t.Errorf("--repo %q: error = %#v", test.repo, cliErr)
		}
	}
	_, calls := reviewServer(t, map[string][]invariantReply{
		"GET /repositories/assessment_config":                                 {threatProfileCoverage()},
		"GET /threat_profile/repositories/" + otherTestRepoID + "/attributes": {{http.StatusOK, json.RawMessage(`{"revision": "r1", "attributes": {}}`)}},
	})
	if _, err := runInvariants(newBookDataCmd(threatProfileData), "get", "--repo", "https://gitlab.com/acme/web"); err != nil {
		t.Fatalf("a repository named with where it lives: %v (calls %v)", err, calls())
	}
}

func TestThreatProfileOnAnOlderServer(t *testing.T) {
	for _, test := range []struct {
		status int
		detail string
		code   string
	}{
		{http.StatusNotFound, "Not Found", "DATA_EDITS_UNAVAILABLE"},
		{http.StatusMethodNotAllowed, "Method Not Allowed", "DATA_EDITS_UNAVAILABLE"},
		// The route answered: the repository has no threat profile yet.
		{http.StatusNotFound, "No threat profile for this repository", "NOT_FOUND"},
	} {
		reviewServer(t, map[string][]invariantReply{
			"GET /threat_profile/repositories/" + testRepoID + "/attributes": {{test.status, map[string]any{"detail": test.detail}}},
		})
		_, err := runInvariants(newBookDataCmd(threatProfileData), "get", "--repo", testRepoID)
		cliErr := bookDataCLIError(t, err)
		if cliErr.Code != test.code || cliErr.ExitCode != clierrors.ExitNotFound {
			t.Errorf("%d %s: error = %#v", test.status, test.detail, cliErr)
		}
		if test.code == "DATA_EDITS_UNAVAILABLE" && !strings.Contains(cliErr.Message, "doesn't support editing your repository threat profile yet") {
			t.Errorf("message = %q", cliErr.Message)
		}
	}
}

const appProfileDoc = `{"revision": "a2",
	"attributes": {"internet_exposed": true, "data_classes": [], "handles_payments": null},
	"provenance": {"internet_exposed": {"provenance": "computed"}, "data_classes": {"provenance": "asserted", "asserted_by": "alice@acme.com"}, "handles_payments": {"provenance": "computed"}},
	"sources": {"internet_exposed": {"from": ["acme/api"], "unknown_on": ["acme/job"], "guessed": true}, "data_classes": {"from": ["acme/api"], "unknown_on": [], "guessed": false}, "handles_payments": {"from": [], "unknown_on": ["acme/api", "acme/job"], "guessed": false}},
	"tier": "high", "score": 72}`

func TestThreatProfileAppGetAndApply(t *testing.T) {
	route := "/inventory/apps/" + testAppID + "/threat-profile"
	_, calls := reviewServer(t, map[string][]invariantReply{
		"GET /inventory/catalog": {{http.StatusOK, catalogFixture()}, {http.StatusOK, catalogFixture()}},
		"GET " + route:           {{http.StatusOK, json.RawMessage(appProfileDoc)}, {http.StatusOK, json.RawMessage(appProfileDoc)}},
		"PUT " + route: {
			{http.StatusOK, map[string]any{"applied": false, "changes": []any{"Set internet_exposed: yes → no"}, "revision": "a2"}},
			{http.StatusOK, map[string]any{"applied": true, "changes": []any{"Set internet_exposed: yes → no"}, "revision": "a3"}},
		},
	})
	// A name, in any case, is looked up in the catalog.
	out, err := runInvariants(newBookDataCmd(threatProfileData), "get", "--app", "billing")
	if err != nil || !strings.Contains(out, `"unknown_on"`) || !strings.Contains(out, `"revision": "a2"`) {
		t.Fatalf("get: %v\n%s", err, out)
	}
	out, err = runInvariants(newBookDataCmd(threatProfileData), "get", "--app", testAppID, "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ATTRIBUTE", "internet_exposed  yes", "combined from repos    acme/api", "acme/job", "data_classes      none", "handles_payments  unknown", "person alice@acme.com", "tier high, score 72, revision a2"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	// The read-only keys are never sent: the server refuses unknown ones.
	file := writeInvariants(t, strings.Replace(appProfileDoc, `"internet_exposed": true,`, `"internet_exposed": false,`, 1))
	out, err = runInvariants(newBookDataCmd(threatProfileData), "apply", "--app", "Billing", "--file", file, "--dry-run", "-o", "table")
	if err != nil || !strings.Contains(out, "Would apply 1 change") {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	out, err = runInvariants(newBookDataCmd(threatProfileData), "apply", "--app", testAppID, "--file", file, "-o", "table")
	if err != nil || !strings.Contains(out, "Now at revision a3") {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	var puts []invariantCall
	for _, call := range calls() {
		if call.method == "PUT" {
			puts = append(puts, call)
		}
	}
	if len(puts) != 2 || puts[0].query != "dry_run=true" || puts[1].query != "dry_run=false" {
		t.Fatalf("PUTs = %v", puts)
	}
	for _, key := range []string{"provenance", "sources", "tier", "score"} {
		if _, ok := puts[1].body[key]; ok {
			t.Errorf("sent read-only %q: %v", key, puts[1].body)
		}
	}
	if attributes, _ := puts[1].body["attributes"].(map[string]any); attributes["internet_exposed"] != false || puts[1].body["revision"] != "a2" {
		t.Errorf("body = %v", puts[1].body)
	}
}

func TestThreatProfileAppRefusals(t *testing.T) {
	for _, args := range [][]string{
		{"get"},
		{"get", "--repo", "acme/api", "--app", "Billing"},
		{"get", "--app", " "},
		{"get", "--app", testAppID, "-o", "yaml"},
		// A table is only for an app's profile.
		{"get", "--repo", testRepoID, "-o", "table"},
	} {
		_, calls := reviewServer(t, map[string][]invariantReply{})
		_, err := runInvariants(newBookDataCmd(threatProfileData), args...)
		if cliErr := bookDataCLIError(t, err); cliErr.ExitCode != clierrors.ExitUsageError || len(calls()) != 0 {
			t.Errorf("%v: error = %#v, calls %v", args, cliErr, calls())
		}
	}
	// An unknown name suggests the close ones; nothing else is read.
	_, calls := reviewServer(t, map[string][]invariantReply{"GET /inventory/catalog": {{http.StatusOK, catalogFixture()}}})
	_, err := runInvariants(newBookDataCmd(threatProfileData), "get", "--app", "Bill")
	if cliErr := bookDataCLIError(t, err); cliErr.Code != "NOT_FOUND" || cliErr.ExitCode != clierrors.ExitNotFound || !strings.Contains(cliErr.Suggestion, "Billing") || len(calls()) != 1 {
		t.Errorf("unknown app: error = %#v, calls %v", cliErr, calls())
	}
	route := "/inventory/apps/" + testAppID + "/threat-profile"
	for _, test := range []struct {
		reply invariantReply
		code  string
	}{
		{invariantReply{http.StatusNotFound, map[string]any{"detail": "App not found"}}, "NOT_FOUND"},
		{invariantReply{http.StatusNotFound, map[string]any{"detail": "Not Found"}}, "DATA_EDITS_UNAVAILABLE"},
		{invariantReply{http.StatusConflict, map[string]any{"detail": "The app's threat profile changed"}}, "DATA_CHANGED"},
	} {
		reviewServer(t, map[string][]invariantReply{"PUT " + route: {test.reply}})
		file := writeInvariants(t, appProfileDoc)
		_, err := runInvariants(newBookDataCmd(threatProfileData), "apply", "--app", testAppID, "--file", file, "-o", "table")
		if cliErr := bookDataCLIError(t, err); cliErr.Code != test.code {
			t.Errorf("%v: error = %#v", test.reply.body, cliErr)
		}
	}
}
