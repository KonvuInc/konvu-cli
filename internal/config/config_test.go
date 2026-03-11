package config

import (
	"os"
	"testing"
)

func TestGetAPIBaseURL_Default(t *testing.T) {
	os.Unsetenv("KONVU_API_URL")
	got := GetAPIBaseURL()
	if got != "https://api.konvu.com" {
		t.Errorf("GetAPIBaseURL() = %q, want %q", got, "https://api.konvu.com")
	}
}

func TestGetAPIBaseURL_Override(t *testing.T) {
	t.Setenv("KONVU_API_URL", "https://custom.api.com")
	got := GetAPIBaseURL()
	if got != "https://custom.api.com" {
		t.Errorf("GetAPIBaseURL() = %q, want %q", got, "https://custom.api.com")
	}
}

func TestGetZitadelDomain_Fallback(t *testing.T) {
	os.Unsetenv("KONVU_ZITADEL_DOMAIN")
	t.Setenv("ZITADEL_DOMAIN", "https://fallback.example.com")
	got := GetZitadelDomain()
	if got != "https://fallback.example.com" {
		t.Errorf("GetZitadelDomain() = %q, want %q", got, "https://fallback.example.com")
	}
}

func TestGetZitadelClientID_Primary(t *testing.T) {
	t.Setenv("KONVU_ZITADEL_CLIENT_ID", "primary-id")
	t.Setenv("ZITADEL_CLI_CLIENT_ID", "fallback-id")
	got := GetZitadelClientID()
	if got != "primary-id" {
		t.Errorf("GetZitadelClientID() = %q, want %q", got, "primary-id")
	}
}

func TestGetConfigDir(t *testing.T) {
	dir := GetConfigDir()
	if dir == "" {
		t.Error("GetConfigDir() returned empty string")
	}
}
