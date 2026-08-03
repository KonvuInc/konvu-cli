package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KonvuInc/konvu-cli/pkg/config"
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
	if err := config.ValidateURL(baseURL); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
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

// ServerDetail pulls the human-readable reason out of an error response: the "detail"
// field when the body is the usual JSON error shape, otherwise the raw body. Truncated,
// since it ends up in a one-line message.
func ServerDetail(body []byte) string {
	detail := strings.TrimSpace(string(body))
	var payload struct {
		Detail any `json:"detail"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Detail != nil {
		if s, ok := payload.Detail.(string); ok {
			detail = s
		} else if b, err := json.Marshal(payload.Detail); err == nil {
			detail = string(b)
		}
	}
	if len(detail) > 300 {
		detail = detail[:300] + "…"
	}
	return detail
}

func (c *Client) checkResponse(resp *http.Response) error {
	if resp.StatusCode == 401 {
		// A 401 can also come from a service behind the API refusing the request for its
		// own reasons. Saying only "log in again" then sends the user round a loop that
		// cannot succeed, so include what the server actually said.
		body, _ := io.ReadAll(resp.Body)
		msg := "Session expired. Run 'konvu login' again."
		if detail := ServerDetail(body); detail != "" {
			msg += " (server said: " + detail + ")"
		}
		return &AuthenticationError{Message: msg}
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
	return c.query("GET", path, params)
}

// Delete calls an endpoint whose arguments travel as query parameters and which sends no body.
func (c *Client) Delete(path string, params map[string]any) (map[string]any, error) {
	return c.query("DELETE", path, params)
}

func (c *Client) query(method, path string, params map[string]any) (map[string]any, error) {
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

	req, err := http.NewRequest(method, reqURL, nil)
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

// GetList calls a GET endpoint that returns a JSON array (not an object).
func (c *Client) GetList(path string, params map[string]any) ([]any, error) {
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

	var result []any
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

// Patch sends a PATCH request. data may be any JSON-serializable value —
// the assessment_config endpoint takes a top-level array, which map[string]any
// cannot represent — so the response is decoded into a generic value.
func (c *Client) Patch(path string, data any) (any, error) {
	return c.sendBody("PATCH", path, data)
}

// Put sends a PUT request with a JSON object body.
func (c *Client) Put(path string, data map[string]any) (map[string]any, error) {
	result, err := c.sendBody("PUT", path, data)
	if err != nil {
		return nil, err
	}
	m, _ := result.(map[string]any)
	return m, nil
}

// sendBody issues a request with a JSON body and decodes the response into a
// generic value (object or array). Shared by Patch and Put.
func (c *Client) sendBody(method, path string, data any) (any, error) {
	reqURL := c.baseURL + path

	var body io.Reader
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, reqURL, body)
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

	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// PostForm sends application/x-www-form-urlencoded. Some service endpoints behind the
// gateway take form fields rather than JSON, because the same route also accepts a file
// upload and a route cannot take both a JSON body and a file part.
func (c *Client) PostForm(path string, form url.Values) (map[string]any, error) {
	req, err := http.NewRequest("POST", c.baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	auth, err := c.authHeader()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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

// PostMultipart sends multipart/form-data: the given form fields plus one file part. It is the
// second way to hand a route a file — the first being to ask for a pre-authorized upload URL and
// PUT to that — and exists for servers that cannot issue one.
//
// The body is streamed off disk rather than assembled first, so sending a large file does not
// need room for a second copy of it. Like PutPresigned it takes its own timeout, because a body
// carrying a file can outlast the API client's.
func (c *Client) PostMultipart(
	path string, form url.Values, fileField, filePath string, timeout time.Duration,
) (map[string]any, error) {
	auth, err := c.authHeader()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	body, out := io.Pipe()
	mw := multipart.NewWriter(out)
	// Writing the body has to be able to fail after the request has started, and it must reach the
	// reader as an error: closing the pipe cleanly instead would hand the server a truncated body
	// under a complete-looking request, which it would have no way to tell from the real thing.
	go func() { _ = out.CloseWithError(writeMultipart(mw, form, fileField, filepath.Base(filePath), f)) }()

	req, err := http.NewRequest("POST", c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
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

// writeMultipart writes the fields, then the file, then the closing boundary. Split out so the
// goroutine above is one line and every error on this path has somewhere to go.
func writeMultipart(mw *multipart.Writer, form url.Values, fileField, fileName string, file io.Reader) error {
	for name, values := range form {
		for _, value := range values {
			if err := mw.WriteField(name, value); err != nil {
				return err
			}
		}
	}
	part, err := mw.CreateFormFile(fileField, fileName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	// Writes the trailing boundary, without which the server sees an unterminated body.
	return mw.Close()
}

// PutPresigned uploads a file to a pre-authorized upload URL.
//
// It deliberately sends no Authorization header: the URL carries its own signature, and
// an unexpected auth header makes the storage service reject the request. size must match
// the length the URL was issued for — it is part of that signature — so it is set
// explicitly rather than left to chunked encoding. Uploads get their own timeout because
// a large body can outlast the API client's.
func (c *Client) PutPresigned(target, filePath string, size int64, timeout time.Duration) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	req, err := http.NewRequest("PUT", target, f)
	if err != nil {
		return err
	}
	req.ContentLength = size

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &APIError{
			Message:    fmt.Sprintf("upload failed: %s", strings.TrimSpace(string(body))),
			StatusCode: resp.StatusCode,
		}
	}
	return nil
}
