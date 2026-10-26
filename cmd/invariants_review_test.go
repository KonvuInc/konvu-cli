package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

const (
	testInvariantID  = "3f2a1c9e-8b7d-4e6f-9a0b-1c2d3e4f5a6b"
	otherInvariantID = "9d8c7b6a-5f4e-4d3c-8b2a-1f0e9d8c7b6a"
	// A reply status that closes the connection before any response.
	dropConnection = -1
)

type invariantCall struct {
	method string
	path   string
	body   map[string]any
}

type invariantReply struct {
	status int
	body   any
}

// reviewServer answers each "METHOD path" with its replies in order, and records every call.
// A call with no reply left fails the test: a command that should not reach the server, or
// reaches it once too often, is caught.
func reviewServer(t *testing.T, replies map[string][]invariantReply) (*httptest.Server, func() []invariantCall) {
	t.Helper()
	var (
		mu    sync.Mutex
		calls []invariantCall
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		call := invariantCall{method: r.Method, path: r.URL.Path}
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&call.body); err != nil {
				t.Errorf("%s: body is not JSON: %v", key, err)
			}
		}
		mu.Lock()
		calls = append(calls, call)
		queue := replies[key]
		if len(queue) == 0 {
			mu.Unlock()
			t.Errorf("unexpected call %s", key)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		reply := queue[0]
		replies[key] = queue[1:]
		mu.Unlock()
		if reply.status == dropConnection {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("%s: hijack: %v", key, err)
				return
			}
			_ = conn.Close()
			return
		}
		w.WriteHeader(reply.status)
		_ = json.NewEncoder(w).Encode(reply.body)
	}))
	t.Cleanup(server.Close)
	invariantsEnv(t, server)
	return server, func() []invariantCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]invariantCall(nil), calls...)
	}
}

func oneInvariant(status string) map[string]any {
	return map[string]any{
		"id": testInvariantID, "repository_id": nil, "rank": 4,
		"statement":      "Session tokens never reach logs.",
		"why_it_matters": "A leaked token is a stolen session.",
		"property":       "confidentiality", "status": status,
		"threat_links": []any{}, "hooks": []any{}, "protected_assets": []any{},
		"controls": []any{map[string]any{
			"graph_id": "g1", "control_id": "c1", "role": "enforces",
			"title": "Redact tokens in the logger", "property": "confidentiality",
			"standard_ref": "OWASP ASVS 7.1.1", "protected_assets": []any{map[string]any{"kind": "data", "name": "session token"}},
			"code_anchors": []any{
				map[string]any{"name": "redact", "decl": "app/log.py#redact", "quote": "def redact("},
				map[string]any{"name": "filter", "decl": "app/log.py#TokenFilter", "quote": "class TokenFilter"},
			},
		}},
		"steering": map[string]any{
			"days": 30, "edits": 12, "last_at": "2026-10-20T09:00:00Z",
			"agents": []any{
				map[string]any{"name": "cursor", "count": 4},
				map[string]any{"name": "claude-code", "count": 8},
				map[string]any{"name": "aider", "count": 1},
				map[string]any{"name": "codex", "count": 4},
			},
			"recent": []any{
				steeredEdit("2026-10-18T09:00:00Z", "cursor", "app/api.py"),
				steeredEdit("2026-10-20T09:00:00Z", "claude-code", "app/log.py"),
				steeredEdit("2026-10-11T09:00:00Z", "codex", "app/oldest.py"),
				steeredEdit("2026-10-17T09:00:00Z", "codex", "app/b.py"),
				steeredEdit("2026-10-16T09:00:00Z", "codex", "app/c.py"),
				steeredEdit("2026-10-15T09:00:00Z", "codex", "app/d.py"),
			},
		},
	}
}

func steeredEdit(at, agent, file string) map[string]any {
	return map[string]any{"at": at, "agent": agent, "developer": "alice", "repository_id": "r1", "file": file, "control_id": "c1"}
}

func invariantRoute(method string, suffix ...string) string {
	return method + " " + invariantsPath + "/" + testInvariantID + strings.Join(suffix, "")
}

func cliErrorOf(t *testing.T, err error) *clierrors.CLIError {
	t.Helper()
	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("err = %#v, want a CLIError", err)
	}
	return cliErr
}

func detail(msg string) map[string]any { return map[string]any{"detail": msg} }

func uploaded(result, status string) map[string]any {
	return map[string]any{"dry_run": false, "invariants": []any{map[string]any{
		"id": testInvariantID, "rank": 4, "statement": "Session tokens never reach logs.",
		"status": status, "revision": 2, "result": result,
	}}}
}

func TestInvariantsListKeepsOnlyTheAskedStatus(t *testing.T) {
	list := []any{oneInvariant("validated"), oneInvariant("proposed")}
	list[1].(map[string]any)["statement"] = "Admins confirm deletes."
	reviewServer(t, map[string][]invariantReply{"GET " + invariantsPath: {{200, list}, {200, list}, {200, list}}})

	out, err := runInvariants(newInvariantsListCmd(), "--status", "proposed", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != 1 || items[0]["status"] != "proposed" {
		t.Fatalf("out = %q", out)
	}

	out, err = runInvariants(newInvariantsListCmd(), "--status", "validated", "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Session tokens") || strings.Contains(out, "Admins confirm") || !strings.Contains(out, testInvariantID) {
		t.Fatalf("out = %q", out)
	}

	out, err = runInvariants(newInvariantsListCmd(), "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != 2 {
		t.Fatalf("default should list every live invariant, out = %q", out)
	}
}

func TestInvariantsListRefusesAnUnknownStatus(t *testing.T) {
	reviewServer(t, nil)
	_, err := runInvariants(newInvariantsListCmd(), "--status", "declined")
	if cliErrorOf(t, err).ExitCode != clierrors.ExitUsageError {
		t.Fatalf("err = %#v", err)
	}
}

func TestInvariantsListRefusedCallerIsAnAuthFailure(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{"GET " + invariantsPath: {{403, detail("No current company")}}})
	_, err := runInvariants(newInvariantsListCmd(), "-o", "json")
	if e := cliErrorOf(t, err); e.ExitCode != clierrors.ExitAuthFailed || e.Message != "No current company" {
		t.Fatalf("err = %#v", e)
	}
}

func TestInvariantsGetPrintsTheFullInvariantAsJSON(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, oneInvariant("validated")}}})
	out, err := runInvariants(newInvariantsGetCmd(), testInvariantID, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("out = %q", out)
	}
	anchors := getSlice(getSlice(got, "controls")[0].(map[string]any), "code_anchors")
	if len(anchors) != 2 || getMap(got, "steering")["edits"] != float64(12) || len(getSlice(getMap(got, "steering"), "recent")) != 6 {
		t.Fatalf("got = %#v", got)
	}
}

func TestInvariantsGetTableSummarizesControlsAndSteering(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, oneInvariant("proposed")}}})
	out, err := runInvariants(newInvariantsGetCmd(), testInvariantID, "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Session tokens never reach logs.",
		"proposed (suggested by Konvu)",
		"Why:       A leaked token is a stolen session.",
		"Redact tokens in the logger · enforces · OWASP ASVS 7.1.1 · 2 code anchors",
		"Agent steering (last 30 days): 12 edits, last 2026-10-20T09:00:00Z",
		"Top agents: claude-code (8), codex (4), cursor (4)\n",
		"    2026-10-20T09:00:00Z  claude-code  alice  app/log.py  c1\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	newest, older := strings.Index(out, "2026-10-20T09:00:00Z"), strings.Index(out, "2026-10-18T09:00:00Z")
	if newest < 0 || older < 0 || newest > older {
		t.Errorf("recent edits are not newest first:\n%s", out)
	}
	if strings.Contains(out, "aider") || strings.Contains(out, "app/oldest.py") {
		t.Errorf("shows more than the top 3 agents or the 5 newest edits:\n%s", out)
	}
}

func TestInvariantsGetStripsTerminalEscapes(t *testing.T) {
	inv := oneInvariant("proposed")
	inv["why_it_matters"] = "fine\x1b[1A\x1b[2KStatus:    validated (active)"
	getMap(inv, "steering")["recent"] = []any{steeredEdit("2026-10-20T09:00:00Z", "claude-code", "app/\x1b]52;c;aGk=\x07x.py")}
	reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, inv}}})
	out, err := runInvariants(newInvariantsGetCmd(), testInvariantID, "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Fatalf("control characters reached the terminal: %q", out)
	}
}

func TestInvariantsGetWithoutSteeringSaysSo(t *testing.T) {
	inv := oneInvariant("validated")
	delete(inv, "steering")
	inv["controls"] = []any{}
	reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, inv}}})
	out, err := runInvariants(newInvariantsGetCmd(), testInvariantID, "-o", "table")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "none mapped yet") || !strings.Contains(out, "Agent steering: not reported") {
		t.Fatalf("out = %q", out)
	}
}

func TestInvariantsGetNotFound(t *testing.T) {
	reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{404, detail("no such invariant for this company")}}})
	_, err := runInvariants(newInvariantsGetCmd(), testInvariantID)
	if e := cliErrorOf(t, err); e.Code != "INVARIANT_NOT_FOUND" || e.ExitCode != clierrors.ExitNotFound {
		t.Fatalf("err = %#v", e)
	}
}

func TestInvariantCommandsRefuseAMalformedIDBeforeAnyCall(t *testing.T) {
	reviewServer(t, nil)
	for name, run := range map[string]func() error{
		"get":     func() error { _, err := runInvariants(newInvariantsGetCmd(), "../decline"); return err },
		"accept":  func() error { _, err := runInvariants(newInvariantsAcceptCmd(), "rank-4"); return err },
		"decline": func() error { _, err := runInvariants(newInvariantsDeclineCmd(), testInvariantID+"x"); return err },
	} {
		if e := cliErrorOf(t, run()); e.ExitCode != clierrors.ExitUsageError {
			t.Errorf("%s: err = %#v", name, e)
		}
	}
}

func TestInvariantCommandsNeedALogin(t *testing.T) {
	server, calls := reviewServer(t, nil)
	t.Setenv("KONVU_API_URL", server.URL)
	t.Setenv("KONVU_ACCESS_TOKEN", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	for name, cmd := range map[string]func() error{
		"get":     func() error { _, err := runInvariants(newInvariantsGetCmd(), testInvariantID); return err },
		"accept":  func() error { _, err := runInvariants(newInvariantsAcceptCmd(), testInvariantID); return err },
		"decline": func() error { _, err := runInvariants(newInvariantsDeclineCmd(), testInvariantID); return err },
	} {
		if e := cliErrorOf(t, cmd()); e.ExitCode != clierrors.ExitAuthFailed {
			t.Errorf("%s: err = %#v", name, e)
		}
	}
	if n := len(calls()); n != 0 {
		t.Fatalf("%d calls reached the server without a token", n)
	}
}

func TestInvariantsAcceptUploadsTheSuggestionUnchanged(t *testing.T) {
	inv := oneInvariant("proposed")
	inv["why_it_matters"] = strings.Repeat("é", 520)
	_, calls := reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):    {{200, inv}},
		"POST " + invariantsPath: {{201, uploaded("validated", "validated")}},
	})
	out, err := runInvariants(newInvariantsAcceptCmd(), testInvariantID, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	sent := calls()[1].body
	item := sent["invariants"].([]any)[0].(map[string]any)
	if sent["dry_run"] != false || item["statement"] != "Session tokens never reach logs." || item["property"] != "confidentiality" {
		t.Fatalf("sent = %#v", sent)
	}
	// The server keeps the matched row's rank; a rank sent would only ever displace another suggestion.
	if _, ok := item["rank"]; ok {
		t.Fatalf("sent a rank: %#v", item)
	}
	if n := len([]rune(item["why_it_matters"].(string))); n != maxUploadWhyChars {
		t.Fatalf("why_it_matters is %d characters, want the upload's limit", n)
	}
	if _, ok := item["protected_assets"]; ok {
		t.Fatalf("sent fields the upload does not need: %#v", item)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil || resp["mapping_pending"] != false {
		t.Fatalf("out = %q", out)
	}
	if getSlice(resp, "invariants")[0].(map[string]any)["result"] != "validated" {
		t.Fatalf("resp = %#v", resp)
	}
}

func TestInvariantsAcceptDryRunWritesNothing(t *testing.T) {
	dry := uploaded("validated", "validated")
	dry["dry_run"] = true
	_, calls := reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):    {{200, oneInvariant("proposed")}},
		"POST " + invariantsPath: {{201, dry}, {502, detail("service_proxy_error")}},
	})
	if _, err := runInvariants(newInvariantsAcceptCmd(), testInvariantID, "--dry-run", "-o", "json"); err != nil {
		t.Fatal(err)
	}
	if got := calls(); len(got) != 2 || got[1].body["dry_run"] != true {
		t.Fatalf("calls = %#v", got)
	}

	// A failed dry run is not read back: it wrote nothing, so there is nothing to confirm.
	reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):    {{200, oneInvariant("proposed")}},
		"POST " + invariantsPath: {{502, detail("service_proxy_error")}},
	})
	_, err := runInvariants(newInvariantsAcceptCmd(), testInvariantID, "--dry-run")
	if e := cliErrorOf(t, err); !e.Retryable || !strings.Contains(e.Suggestion, "writes nothing") {
		t.Fatalf("err = %#v", e)
	}
}

func TestInvariantsAcceptRefusesWhatIsNotASuggestion(t *testing.T) {
	for _, status := range []string{"validated", "declined"} {
		_, calls := reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{200, oneInvariant(status)}}})
		_, err := runInvariants(newInvariantsAcceptCmd(), testInvariantID)
		e := cliErrorOf(t, err)
		if e.Code != "INVARIANT_NOT_PROPOSED" || e.ExitCode != clierrors.ExitGeneralError || !strings.Contains(e.Message, status) {
			t.Fatalf("%s: err = %#v", status, e)
		}
		if n := len(calls()); n != 1 {
			t.Fatalf("%s: %d calls, want only the read", status, n)
		}
	}
}

func TestInvariantsAcceptAfterAServerErrorReadsTheOutcomeBack(t *testing.T) {
	mapping := detail("invariants saved but not yet mapped to controls; retry the same upload")

	_, calls := reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):    {{200, oneInvariant("proposed")}, {200, oneInvariant("validated")}},
		"POST " + invariantsPath: {{502, mapping}},
	})
	out, err := runInvariants(newInvariantsAcceptCmd(), testInvariantID, "-o", "json")
	if err != nil {
		t.Fatalf("a saved accept must succeed: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil || resp["mapping_pending"] != true {
		t.Fatalf("out = %q", out)
	}
	if row := getSlice(resp, "invariants")[0].(map[string]any); row["result"] != "validated" || row["id"] != testInvariantID {
		t.Fatalf("resp = %#v", resp)
	}
	if n := len(calls()); n != 3 {
		t.Fatalf("%d calls, want the read, the upload and the read-back", n)
	}

	// Still proposed after a timeout: not accepted yet, and the request may still be running.
	for _, failure := range []invariantReply{{504, detail("service_timeout")}, {dropConnection, nil}} {
		_, calls = reviewServer(t, map[string][]invariantReply{
			invariantRoute("GET"):    {{200, oneInvariant("proposed")}, {200, oneInvariant("proposed")}},
			"POST " + invariantsPath: {failure},
		})
		_, err = runInvariants(newInvariantsAcceptCmd(), testInvariantID)
		e := cliErrorOf(t, err)
		if e.Code != "ACCEPT_UNCONFIRMED" || !e.Retryable || !strings.Contains(e.Message, "may still be running") || !strings.Contains(e.Suggestion, "safe to repeat") {
			t.Fatalf("%d: err = %#v", failure.status, e)
		}
		if n := len(calls()); n != 3 {
			t.Fatalf("%d: %d calls, want a read-back", failure.status, n)
		}
	}

	// Neither outcome can be read back.
	reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):    {{200, oneInvariant("proposed")}, {503, detail("down")}},
		"POST " + invariantsPath: {{504, detail("service_timeout")}},
	})
	_, err = runInvariants(newInvariantsAcceptCmd(), testInvariantID)
	if e := cliErrorOf(t, err); e.Code != "ACCEPT_UNCONFIRMED" || e.Retryable {
		t.Fatalf("err = %#v", e)
	}

	// Declined while the accept failed.
	reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):    {{200, oneInvariant("proposed")}, {200, oneInvariant("declined")}},
		"POST " + invariantsPath: {{502, detail("service_proxy_error")}},
	})
	_, err = runInvariants(newInvariantsAcceptCmd(), testInvariantID)
	if e := cliErrorOf(t, err); e.Code != "INVARIANT_CHANGED" || e.ExitCode != clierrors.ExitGeneralError || !strings.Contains(e.Message, "declined") {
		t.Fatalf("err = %#v", e)
	}
}

func TestInvariantsAcceptSessionExpiringOnTheUploadIsAnAuthFailure(t *testing.T) {
	_, calls := reviewServer(t, map[string][]invariantReply{
		invariantRoute("GET"):    {{200, oneInvariant("proposed")}},
		"POST " + invariantsPath: {{401, detail("token expired")}},
	})
	_, err := runInvariants(newInvariantsAcceptCmd(), testInvariantID)
	if e := cliErrorOf(t, err); e.ExitCode != clierrors.ExitAuthFailed {
		t.Fatalf("err = %#v", e)
	}
	if n := len(calls()); n != 2 {
		t.Fatalf("%d calls: a refused upload wrote nothing, so there is nothing to read back", n)
	}
}

func TestInvariantsAcceptMapsRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		route string
		reply invariantReply
		code  string
		exit  int
	}{
		{"not a person", "POST " + invariantsPath, invariantReply{403, detail("An attributed user identity is required.")}, "FORBIDDEN", clierrors.ExitAuthFailed},
		{"changed meanwhile", "POST " + invariantsPath, invariantReply{409, detail("rank 4 is held by an invariant that stays")}, "INVARIANT_CHANGED", clierrors.ExitGeneralError},
		{"not acceptable unchanged", "POST " + invariantsPath, invariantReply{422, detail("statement too long")}, "INVARIANT_NOT_ACCEPTED", clierrors.ExitGeneralError},
	} {
		reviewServer(t, map[string][]invariantReply{
			invariantRoute("GET"): {{200, oneInvariant("proposed")}},
			tc.route:              {tc.reply},
		})
		_, err := runInvariants(newInvariantsAcceptCmd(), testInvariantID)
		if e := cliErrorOf(t, err); e.Code != tc.code || e.ExitCode != tc.exit || e.Message != tc.reply.body.(map[string]any)["detail"] {
			t.Errorf("%s: err = %#v", tc.name, e)
		}
	}

	reviewServer(t, map[string][]invariantReply{invariantRoute("GET"): {{404, detail("no such invariant for this company")}}})
	_, err := runInvariants(newInvariantsAcceptCmd(), testInvariantID)
	if e := cliErrorOf(t, err); e.Code != "INVARIANT_NOT_FOUND" || e.ExitCode != clierrors.ExitNotFound {
		t.Fatalf("err = %#v", e)
	}
}

func TestInvariantsAcceptRefusesAnUploadThatLandedElsewhere(t *testing.T) {
	created := uploaded("created", "validated")
	created["invariants"].([]any)[0].(map[string]any)["id"] = otherInvariantID
	dryCreated := uploaded("created", "validated")
	dryCreated["dry_run"] = true
	dryCreated["invariants"].([]any)[0].(map[string]any)["id"] = nil
	elsewhere := uploaded("validated", "validated")
	elsewhere["invariants"].([]any)[0].(map[string]any)["id"] = otherInvariantID

	for _, tc := range []struct {
		name    string
		resp    map[string]any
		args    []string
		code    string
		message string
		written bool
	}{
		{"declined meanwhile", created, nil, "INVARIANT_CHANGED", otherInvariantID, true},
		{"declined meanwhile, dry run", dryCreated, []string{"--dry-run"}, "INVARIANT_CHANGED", "would be added", false},
		{"accepted meanwhile", uploaded("unchanged", "validated"), nil, "INVARIANT_NOT_PROPOSED", "already active", false},
		{"same wording elsewhere", elsewhere, nil, "INVARIANT_CHANGED", otherInvariantID, true},
	} {
		reviewServer(t, map[string][]invariantReply{
			invariantRoute("GET"):    {{200, oneInvariant("proposed")}},
			"POST " + invariantsPath: {{201, tc.resp}},
		})
		_, err := runInvariants(newInvariantsAcceptCmd(), append([]string{testInvariantID, "-o", "json"}, tc.args...)...)
		e := cliErrorOf(t, err)
		if e.Code != tc.code || e.ExitCode != clierrors.ExitGeneralError || !strings.Contains(e.Message, tc.message) {
			t.Errorf("%s: err = %#v", tc.name, e)
		}
		if strings.Contains(e.Message, "(id )") || strings.Contains(e.Message, "id ,") {
			t.Errorf("%s: names an empty id: %q", tc.name, e.Message)
		}
		if got := strings.Contains(e.Suggestion, "It is active now"); got != tc.written {
			t.Errorf("%s: suggestion %q, written = %v", tc.name, e.Suggestion, tc.written)
		}
	}
}

func TestInvariantsDeclinePostsTheDecision(t *testing.T) {
	declined := oneInvariant("declined")
	_, calls := reviewServer(t, map[string][]invariantReply{invariantRoute("POST", "/decline"): {{200, declined}}})
	out, err := runInvariants(newInvariantsDeclineCmd(), testInvariantID, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["status"] != "declined" || got["id"] != testInvariantID {
		t.Fatalf("out = %q", out)
	}
	if c := calls(); len(c) != 1 || c[0].body != nil {
		t.Fatalf("calls = %#v", c)
	}
}

func TestInvariantsDeclineMapsRefusals(t *testing.T) {
	for _, tc := range []struct {
		name      string
		reply     invariantReply
		code      string
		exit      int
		retryable bool
	}{
		{"not a suggestion", invariantReply{409, detail("invariant is validated; only a proposed one can be declined")}, "INVARIANT_NOT_PROPOSED", clierrors.ExitGeneralError, false},
		{"unknown", invariantReply{404, detail("no such invariant for this company")}, "INVARIANT_NOT_FOUND", clierrors.ExitNotFound, false},
		{"not a person", invariantReply{403, detail("An attributed user identity is required.")}, "FORBIDDEN", clierrors.ExitAuthFailed, false},
		{"server", invariantReply{502, detail("service_proxy_error")}, "API_ERROR", clierrors.ExitGeneralError, true},
	} {
		reviewServer(t, map[string][]invariantReply{invariantRoute("POST", "/decline"): {tc.reply}})
		_, err := runInvariants(newInvariantsDeclineCmd(), testInvariantID)
		e := cliErrorOf(t, err)
		if e.Code != tc.code || e.ExitCode != tc.exit || e.Retryable != tc.retryable || !strings.Contains(e.Message, tc.reply.body.(map[string]any)["detail"].(string)) {
			t.Errorf("%s: err = %#v", tc.name, e)
		}
		if tc.retryable && !strings.Contains(e.Suggestion, "safe to repeat") {
			t.Errorf("%s: suggestion %q does not say a retry is safe", tc.name, e.Suggestion)
		}
	}
}
