package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	os.Setenv("KONVU_ZITADEL_CLIENT_ID", "test")
	os.Exit(m.Run())
}

func TestClient_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing or wrong Authorization header")
		}
		if r.URL.Path != "/sca_findings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()

	data, err := client.Get("/sca_findings", nil)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if data["total"] != float64(0) {
		t.Errorf("total = %v, want 0", data["total"])
	}
}

func TestClient_Get_401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte("unauthorized"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "bad-token")
	defer client.Close()

	_, err := client.Get("/test", nil)
	if err == nil {
		t.Fatal("expected AuthenticationError, got nil")
	}
	if _, ok := err.(*AuthenticationError); !ok {
		t.Errorf("expected *AuthenticationError, got %T", err)
	}
}

func TestClient_Post(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()

	data, err := client.Post("/test", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	if data["status"] != "ok" {
		t.Errorf("status = %v, want ok", data["status"])
	}
}

func TestClient_Post_204(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()

	data, err := client.Post("/test", nil)
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil body for 204, got %v", data)
	}
}

func TestClient_TokenFromEnv(t *testing.T) {
	t.Setenv("KONVU_ACCESS_TOKEN", "env-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer env-token" {
			t.Error("expected env token in Authorization header")
		}
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	defer client.Close()

	_, err := client.Get("/test", nil)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
}

func TestClient_Patch_ArrayBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		var arr []any
		if err := json.NewDecoder(r.Body).Decode(&arr); err != nil {
			t.Fatalf("body is not a top-level JSON array: %v", err)
		}
		if len(arr) != 1 {
			t.Errorf("array len = %d, want 1", len(arr))
		}
		json.NewEncoder(w).Encode(arr) // echo the array back
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()

	body := []map[string]any{{"repository_id": "r1", "assessment_enabled": true}}
	result, err := client.Patch("/repositories/assessment_config", body)
	if err != nil {
		t.Fatalf("Patch() error: %v", err)
	}
	if _, ok := result.([]any); !ok {
		t.Errorf("expected array response, got %T", result)
	}
}

func TestClient_Patch_422(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		w.Write([]byte("invalid"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()

	_, err := client.Patch("/x", []any{})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != 422 {
		t.Errorf("expected *APIError 422, got %T %v", err, err)
	}
}

func TestClient_Put(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"assessment_severities": []any{"CRITICAL"}})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()

	data, err := client.Put("/x", map[string]any{"assessment_severities": []string{"CRITICAL"}})
	if err != nil {
		t.Fatalf("Put() error: %v", err)
	}
	sevs, _ := data["assessment_severities"].([]any)
	if len(sevs) != 1 || sevs[0] != "CRITICAL" {
		t.Errorf("unexpected response: %v", data)
	}
}

func TestServerDetailPrefersTheDetailField(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`{"detail":"konvu lane not served on this host"}`, "konvu lane not served on this host"},
		{`{"detail":{"code":7}}`, `{"code":7}`},
		{`plain text failure`, "plain text failure"},
		{``, ""},
		{`{"other":"x"}`, `{"other":"x"}`},
	} {
		if got := ServerDetail([]byte(tc.in)); got != tc.want {
			t.Errorf("ServerDetail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestServerDetailTruncates(t *testing.T) {
	long := strings.Repeat("x", 500)
	got := ServerDetail([]byte(long))
	if len([]rune(got)) != 301 {
		t.Errorf("len = %d runes, want 301 (300 + ellipsis)", len([]rune(got)))
	}
}

// A 401 from a service behind the API must not be reported as an expired session with no
// other information — that sends the reader round a loop that cannot succeed.
func TestUnauthorizedCarriesTheServerReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"konvu lane not served on this host"}`))
	}))
	defer srv.Close()
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")

	_, err := NewClient(srv.URL, "tok").Get("/anything", nil)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "not served on this host") {
		t.Errorf("the server's reason is missing: %v", err)
	}
	if !strings.Contains(err.Error(), "konvu login") {
		t.Errorf("the login hint should still be there: %v", err)
	}
}

func TestClient_GetText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/knowledge/pages/company.md" || r.URL.Query().Get("format") != "md" {
			t.Errorf("request = %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte("# Company\n\nText.\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()

	text, err := client.GetText("/knowledge/pages/company.md", map[string]any{"format": "md"}, 1024)
	if err != nil || text != "# Company\n\nText.\n" {
		t.Fatalf("GetText() = %q, %v", text, err)
	}
	if _, err := client.GetText("/knowledge/pages/company.md", map[string]any{"format": "md"}, 4); err == nil {
		t.Fatal("GetText() accepted a body over its limit")
	}
}

func TestClient_GetText_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"Page not found"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()

	_, err := client.GetText("/knowledge/pages/company.md", nil, 1024)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("GetText() error = %#v, want a 404 APIError", err)
	}
}

func TestClient_PostMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/knowledge/uploads" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("request = %s %s, auth %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if r.ContentLength != int64(len(body)) {
			t.Errorf("Content-Length = %d, body is %d bytes", r.ContentLength, len(body))
		}
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		var got []string
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(part)
			got = append(got, part.FormName()+"|"+part.FileName()+"|"+part.Header.Get("Content-Type")+"|"+string(data))
		}
		if want := []string{`file|a "b".md|text/markdown|# A`, "file|b.pdf|application/pdf|%PDF"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("parts = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"items":[{"status":"pending"},{"status":"ready"}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()
	data, err := client.PostMultipart("/knowledge/uploads", []MultipartFile{
		{Field: "file", Filename: `a "b".md`, ContentType: "text/markdown", Size: 3, Body: strings.NewReader("# A")},
		{Field: "file", Filename: "b.pdf", ContentType: "application/pdf", Size: 4, Body: strings.NewReader("%PDF")},
	})
	if err != nil {
		t.Fatalf("PostMultipart() error: %v", err)
	}
	if items, _ := data["items"].([]any); len(items) != 2 {
		t.Errorf("items = %v", data["items"])
	}
}

func TestClient_PostMultipart_Errors(t *testing.T) {
	status := http.StatusFound
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusFound {
			http.Redirect(w, r, "/elsewhere", status)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"detail":"too many files"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	defer client.Close()
	files := func() []MultipartFile {
		return []MultipartFile{{Field: "file", Filename: "a.md", ContentType: "text/markdown", Size: 3, Body: strings.NewReader("# A")}}
	}
	if _, err := client.PostMultipart("/x", files()); err == nil {
		t.Fatal("a redirect was taken as an answer")
	} else if _, ok := err.(*RedirectError); !ok {
		t.Errorf("redirect: got %T %v", err, err)
	}
	status = http.StatusUnprocessableEntity
	if _, err := client.PostMultipart("/x", files()); err == nil {
		t.Fatal("a 422 was taken as an answer")
	} else if apiErr, ok := err.(*APIError); !ok || apiErr.StatusCode != 422 {
		t.Errorf("422: got %T %v", err, err)
	}
	// A body shorter than its declared size fails the request instead of sending other bytes.
	status = http.StatusOK
	short := []MultipartFile{{Field: "file", Filename: "a.md", ContentType: "text/markdown", Size: 10, Body: strings.NewReader("# A")}}
	if _, err := client.PostMultipart("/x", short); err == nil {
		t.Fatal("a file shorter than its size was sent")
	}
}
