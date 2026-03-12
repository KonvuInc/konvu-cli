package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPerformDeviceFlowLogin(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/v2/device_authorization":
			json.NewEncoder(w).Encode(map[string]any{
				"device_code":               "test-device-code",
				"user_code":                 "ABCD-1234",
				"verification_uri":          "https://example.com/verify",
				"verification_uri_complete": "https://example.com/verify?code=ABCD-1234",
				"interval":                  1,
				"expires_in":                300,
			})
		case "/oauth/v2/token":
			callCount++
			if callCount == 1 {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
			} else {
				json.NewEncoder(w).Encode(map[string]any{
					"access_token": "test-access-token",
					"token_type":   "Bearer",
					"expires_in":   3600,
				})
			}
		}
	}))
	defer server.Close()

	noop := func(string) {}
	result, err := PerformDeviceFlowLogin(server.URL, "test-client-id", 10, noop)
	if err != nil {
		t.Fatalf("PerformDeviceFlowLogin error: %v", err)
	}
	if result["access_token"] != "test-access-token" {
		t.Errorf("access_token = %v, want test-access-token", result["access_token"])
	}
}

func TestSaveCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")

	err := SaveCredentials(path, map[string]any{
		"access_token": "my-token",
		"expires_in":   float64(3600),
	})
	if err != nil {
		t.Fatalf("SaveCredentials error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var creds map[string]any
	json.Unmarshal(data, &creds)
	if creds["access_token"] != "my-token" {
		t.Errorf("access_token = %v, want my-token", creds["access_token"])
	}

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file permissions = %o, want 600", info.Mode().Perm())
	}
}
