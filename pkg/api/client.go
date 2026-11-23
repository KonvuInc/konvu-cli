package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
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

// RedirectError is a 3xx answer that was not followed. Location is the server's text, unchecked.
type RedirectError struct {
	StatusCode int
	Location   string
}

func (e *RedirectError) Error() string {
	return fmt.Sprintf("the server answered %d, a redirect, which was not followed", e.StatusCode)
}

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
	resp, err := c.sendQuery(method, path, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetText reads an endpoint that answers with text, such as a markdown file, rather than JSON.
// A body over maxBytes is an error, never a silently cut text.
func (c *Client) GetText(path string, params map[string]any, maxBytes int64) (string, error) {
	resp, err := c.sendQuery("GET", path, params)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(body)) > maxBytes {
		return "", fmt.Errorf("the server's answer is over %d bytes", maxBytes)
	}
	return string(body), nil
}

// sendQuery sends a request whose arguments travel as query parameters. The caller closes the
// body of the response it gets; an error response is already read and closed.
func (c *Client) sendQuery(method, path string, params map[string]any) (*http.Response, error) {
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

	if err := c.checkResponse(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
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
	auth, err := c.authHeader()
	if err != nil {
		return nil, err
	}
	return c.postJSON(c.httpClient, path, data, map[string]string{"Authorization": auth})
}

// PostWithoutLogin sends a JSON POST that carries headers instead of the login token, for a
// route that identifies the caller another way (an onboarding code). It never reads or sends
// the login token, so it works logged out. It never follows a redirect either: Go resends custom
// headers on one, and a 307 or 308 resends the body, maybe to another host. Any 3xx comes back
// as a RedirectError.
func (c *Client) PostWithoutLogin(path string, data map[string]any, headers map[string]string) (map[string]any, error) {
	client := &http.Client{
		Timeout:   c.httpClient.Timeout,
		Transport: c.httpClient.Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return c.postJSON(client, path, data, headers)
}

func (c *Client) postJSON(client *http.Client, path string, data map[string]any, headers map[string]string) (map[string]any, error) {
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

	for name, value := range headers {
		req.Header.Set(name, value)
	}
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// A 3xx still here was not followed, so its body is no answer to this request.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, &RedirectError{StatusCode: resp.StatusCode, Location: resp.Header.Get("Location")}
	}
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

// MultipartFile is one file part of a multipart/form-data request; an empty ContentType is sent
// as application/octet-stream. Size must be Body's exact length: it sets the request's
// Content-Length, so a file that changed since it was measured fails the request instead of
// sending other bytes.
type MultipartFile struct {
	Field       string
	Filename    string
	ContentType string
	Size        int64
	Body        io.Reader
}

// PostMultipart sends files as one multipart/form-data POST, streamed rather than held in memory,
// with the same login and error handling as Post. Like PostWithoutLogin it never follows a
// redirect: a 301 or 302 would resend the request as a GET without the files, and its answer
// would read as theirs. Any 3xx comes back as a RedirectError.
func (c *Client) PostMultipart(path string, files []MultipartFile) (map[string]any, error) {
	auth, err := c.authHeader()
	if err != nil {
		return nil, err
	}
	headers := make([]textproto.MIMEHeader, len(files))
	// The framing around the files does not depend on their bytes, so writing it once with empty
	// parts measures it.
	var framing bytes.Buffer
	measure := multipart.NewWriter(&framing)
	length := int64(0)
	for i, file := range files {
		headers[i] = textproto.MIMEHeader{}
		headers[i].Set("Content-Disposition", multipart.FileContentDisposition(file.Field, file.Filename))
		contentType := file.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		headers[i].Set("Content-Type", contentType)
		if _, err := measure.CreatePart(headers[i]); err != nil {
			return nil, err
		}
		length += file.Size
	}
	if err := measure.Close(); err != nil {
		return nil, err
	}
	length += int64(framing.Len())

	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	if err := form.SetBoundary(measure.Boundary()); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i, file := range files {
			if err := writeMultipartFile(form, headers[i], file); err != nil {
				writer.CloseWithError(err)
				return
			}
		}
		writer.CloseWithError(form.Close())
	}()
	// Closing the reader stops the writer whatever became of the request, so no body is read
	// once this returns.
	defer func() {
		reader.Close()
		<-done
	}()

	req, err := http.NewRequest("POST", c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.ContentLength = length
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", form.FormDataContentType())

	client := &http.Client{
		Transport: c.httpClient.Transport,
		// A large upload on a slow link gets one more second per 128 KiB sent.
		Timeout: c.httpClient.Timeout + time.Duration(length/(128<<10))*time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, &RedirectError{StatusCode: resp.StatusCode, Location: resp.Header.Get("Location")}
	}
	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// writeMultipartFile writes one part, which must hold exactly file.Size bytes: checked per file, so
// one file that grew cannot make up for another that shrank.
func writeMultipartFile(form *multipart.Writer, header textproto.MIMEHeader, file MultipartFile) error {
	part, err := form.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := io.CopyN(part, file.Body, file.Size); err != nil {
		return fmt.Errorf("%s is shorter than the %d bytes it held when checked: %w", file.Filename, file.Size, err)
	}
	var extra [1]byte
	if n, _ := io.ReadFull(file.Body, extra[:]); n > 0 {
		return fmt.Errorf("%s is longer than the %d bytes it held when checked", file.Filename, file.Size)
	}
	return nil
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
