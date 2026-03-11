package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/KonvuTeam/konvu-cli/internal/config"
)

type AuthenticationError struct {
	Message string
}

func (e *AuthenticationError) Error() string { return e.Message }

type APIError struct {
	Message    string
	StatusCode int
}

func (e *APIError) Error() string { return e.Message }

type Client struct {
	baseURL       string
	explicitToken string
	httpClient    *http.Client
}

func NewClient(baseURL, accessToken string) *Client {
	if baseURL == "" {
		baseURL = config.GetAPIBaseURL()
	}
	return &Client{
		baseURL:       baseURL,
		explicitToken: accessToken,
		httpClient:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) Close() {
	c.httpClient.CloseIdleConnections()
}

func (c *Client) getToken() (string, error) {
	if c.explicitToken != "" {
		return c.explicitToken, nil
	}
	if envToken := os.Getenv("KONVU_ACCESS_TOKEN"); envToken != "" {
		return envToken, nil
	}
	return readTokenFromFile()
}

func readTokenFromFile() (string, error) {
	credsPath := config.GetCredentialsPath()
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return "", &AuthenticationError{Message: "Not logged in. Run 'konvu login' first."}
	}
	var creds map[string]any
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", &AuthenticationError{Message: "Corrupted credentials. Run 'konvu login' again."}
	}
	token, ok := creds["access_token"].(string)
	if !ok || token == "" {
		return "", &AuthenticationError{Message: "Invalid credentials. Run 'konvu login' again."}
	}
	return token, nil
}

func (c *Client) authHeader() (string, error) {
	token, err := c.getToken()
	if err != nil {
		return "", err
	}
	return "Bearer " + token, nil
}

func (c *Client) checkResponse(resp *http.Response) error {
	if resp.StatusCode == 401 {
		return &AuthenticationError{Message: "Session expired. Run 'konvu login' again."}
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{
			Message:    fmt.Sprintf("API error: %s", string(body)),
			StatusCode: resp.StatusCode,
		}
	}
	return nil
}

func (c *Client) Get(path string, params map[string]any) (map[string]any, error) {
	reqURL := c.baseURL + path
	if len(params) > 0 {
		values := url.Values{}
		for k, v := range params {
			switch val := v.(type) {
			case []string:
				for _, s := range val {
					values.Add(k, s)
				}
			case string:
				values.Set(k, val)
			default:
				values.Set(k, fmt.Sprintf("%v", val))
			}
		}
		reqURL += "?" + values.Encode()
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	auth, err := c.authHeader()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Post(path string, data map[string]any) (map[string]any, error) {
	reqURL := c.baseURL + path

	var body io.Reader
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest("POST", reqURL, body)
	if err != nil {
		return nil, err
	}

	auth, err := c.authHeader()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth)
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}

	if resp.StatusCode == 204 {
		return nil, nil
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}
