package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/spf13/pflag"
)

// The list command must forward every --dependabot-alert value as a repeated
// `dependabot_alert` query param, for both the comma-separated and repeated-flag
// forms, so the backend can OR them.
func TestListDependabotAlertSendsRepeatedParams(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"comma-separated", []string{"finding", "list", "--dependabot-alert", "RVA_a,RVA_b", "-q"}},
		{"repeated flag", []string{"finding", "list", "--dependabot-alert", "RVA_a", "--dependabot-alert", "RVA_b", "-q"}},
	}
	want := []string{"RVA_a", "RVA_b"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the shared flag so values don't accumulate across runs.
			if sv, ok := scaListCmd.Flags().Lookup("dependabot-alert").Value.(pflag.SliceValue); ok {
				_ = sv.Replace(nil)
			}

			var got []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/sca_findings" {
					got = r.URL.Query()["dependabot_alert"]
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
			}))
			defer server.Close()

			t.Setenv("KONVU_ZITADEL_CLIENT_ID", "test-client")
			t.Setenv("KONVU_API_URL", server.URL)
			t.Setenv("KONVU_ACCESS_TOKEN", "test-token")

			rootCmd.SetArgs(tt.args)
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dependabot_alert params = %v, want %v", got, want)
			}
		})
	}
}
