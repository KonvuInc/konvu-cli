package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"
)

const (
	DefaultLoginTimeout = 300
	DefaultPollInterval = 5
)

func PerformDeviceFlowLogin(zitadelDomain, clientID string, timeout float64, echo func(string)) (map[string]any, error) {
	if clientID == "" {
		return nil, fmt.Errorf("Zitadel client ID not configured. Set KONVU_ZITADEL_CLIENT_ID.")
	}

	deviceAuthURL := zitadelDomain + "/oauth/v2/device_authorization"
	resp, err := http.PostForm(deviceAuthURL, url.Values{
		"client_id": {clientID},
		"scope":     {"openid profile email"},
	})
	if err != nil {
		return nil, fmt.Errorf("device authorization request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device authorization failed: %s", string(body))
	}

	var deviceData map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&deviceData); err != nil {
		return nil, err
	}

	deviceCode := deviceData["device_code"].(string)
	userCode := deviceData["user_code"].(string)
	verificationURI := deviceData["verification_uri"].(string)
	verificationURIComplete, _ := deviceData["verification_uri_complete"].(string)
	pollInterval := DefaultPollInterval
	if v, ok := deviceData["interval"].(float64); ok {
		pollInterval = int(v)
	}
	expiresIn := timeout
	if v, ok := deviceData["expires_in"].(float64); ok && v < timeout {
		expiresIn = v
	}

	echo(fmt.Sprintf("\nTo authenticate, visit: %s", verificationURI))
	echo(fmt.Sprintf("And enter code: %s\n", userCode))

	openURL := verificationURIComplete
	if openURL == "" {
		openURL = verificationURI
	}
	openBrowser(openURL)

	echo(fmt.Sprintf("Waiting for authentication (timeout: %ds)...", int(expiresIn)))

	return pollForToken(zitadelDomain, clientID, deviceCode, pollInterval, expiresIn, echo)
}

func pollForToken(zitadelDomain, clientID, deviceCode string, pollInterval int, timeout float64, echo func(string)) (map[string]any, error) {
	tokenURL := zitadelDomain + "/oauth/v2/token"
	start := time.Now()

	for time.Since(start).Seconds() < timeout {
		resp, err := http.PostForm(tokenURL, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {deviceCode},
			"client_id":   {clientID},
		})
		if err != nil {
			return nil, err
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 200 {
			var tokenData map[string]any
			if err := json.Unmarshal(body, &tokenData); err != nil {
				return nil, err
			}
			if _, ok := tokenData["access_token"]; !ok {
				return nil, fmt.Errorf("token response missing access_token")
			}
			return map[string]any{
				"access_token": tokenData["access_token"],
				"token_type":   tokenData["token_type"],
				"expires_in":   tokenData["expires_in"],
			}, nil
		}

		var errData map[string]string
		json.Unmarshal(body, &errData)
		errCode := errData["error"]

		switch errCode {
		case "authorization_pending":
			time.Sleep(time.Duration(pollInterval) * time.Second)
		case "slow_down":
			pollInterval += 5
			time.Sleep(time.Duration(pollInterval) * time.Second)
		case "expired_token":
			return nil, fmt.Errorf("device code expired. Please try again.")
		case "access_denied":
			return nil, fmt.Errorf("authentication was denied by the user.")
		default:
			desc := errData["error_description"]
			if desc == "" {
				desc = errCode
			}
			if desc == "" {
				desc = string(body)
			}
			return nil, fmt.Errorf("authentication failed: %s", desc)
		}
	}

	return nil, fmt.Errorf("login timed out. Please try again.\nYou can also set KONVU_ACCESS_TOKEN environment variable manually.")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}
