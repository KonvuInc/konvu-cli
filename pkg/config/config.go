package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const AppName = "konvu"

const (
	DefaultAPIBaseURL      = "https://api.konvu.com"
	DefaultZitadelDomain   = "https://auth.konvu.com"
	DefaultZitadelClientID = "362950727238234934"
)

func GetConfigDir() string {
	switch runtime.GOOS {
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, AppName)
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, AppName)
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", AppName)
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, AppName)
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", AppName)
	}
}

func GetCredentialsPath() string {
	return filepath.Join(GetConfigDir(), "credentials.json")
}

func GetAPIBaseURL() string {
	if v := os.Getenv("KONVU_API_URL"); v != "" {
		return v
	}
	return DefaultAPIBaseURL
}

func GetZitadelDomain() string {
	if v := os.Getenv("KONVU_ZITADEL_DOMAIN"); v != "" {
		return v
	}
	if v := os.Getenv("ZITADEL_DOMAIN"); v != "" {
		return v
	}
	return DefaultZitadelDomain
}

func GetZitadelClientID() string {
	if v := os.Getenv("KONVU_ZITADEL_CLIENT_ID"); v != "" {
		return v
	}
	if v := os.Getenv("ZITADEL_CLI_CLIENT_ID"); v != "" {
		return v
	}
	return DefaultZitadelClientID
}

// IsProductionClientID returns true when the active Zitadel client ID is
// the production one baked into the binary.
func IsProductionClientID() bool {
	return GetZitadelClientID() == DefaultZitadelClientID
}

// ValidateURL checks that the URL uses HTTPS when the production client ID
// is in use, preventing tokens from being sent over plaintext.
func ValidateURL(rawURL string) error {
	if IsProductionClientID() && !strings.HasPrefix(rawURL, "https://") {
		return fmt.Errorf("HTTPS required when using production credentials (got %s)", rawURL)
	}
	return nil
}
