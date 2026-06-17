package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
