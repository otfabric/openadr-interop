// Package probe is the HTTP client of the test runner. It sends exactly
// what a scenario asks for, including requests a well-behaved client would
// never make, and records every exchange with credentials removed.
package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Redacted replaces credentials in recorded exchanges.
const Redacted = "[redacted]"

// maxRecorded bounds the size of a recorded body.
const maxRecorded = 64 << 10

// Exchange is one request and its response, as recorded for a report.
type Exchange struct {
	// Client names who sent the request, such as "BL" or "VEN".
	Client string `json:"client"`
	Method string `json:"method"`
	URL    string `json:"url"`
	// RequestHeaders and ResponseHeaders hold the headers that matter to
	// the protocol; the Authorization value is redacted.
	RequestHeaders  map[string]string `json:"requestHeaders,omitempty"`
	RequestBody     string            `json:"requestBody,omitempty"`
	Status          int               `json:"status,omitempty"`
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
	ResponseBody    string            `json:"responseBody,omitempty"`
	// Error is set when no response was received.
	Error      string  `json:"error,omitempty"`
	DurationMS float64 `json:"durationMs"`

	// body is the complete response body, which ResponseBody may truncate.
	body []byte
}

// Body returns the complete response body.
func (e *Exchange) Body() []byte { return e.body }

// JSON decodes the response body. It returns nil if the body is not JSON.
func (e *Exchange) JSON() any {
	var v any
	if json.Unmarshal(e.body, &v) != nil {
		return nil
	}
	return v
}

// String describes the exchange in one line.
func (e *Exchange) String() string {
	if e.Error != "" {
		return fmt.Sprintf("%s %s: %s", e.Method, e.URL, e.Error)
	}
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.URL, e.Status, clip(e.ResponseBody, 300))
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Client sends requests to one VTN as one identity.
type Client struct {
	// Name labels the client's exchanges.
	Name string
	// BaseURL is the URL of the API, without a trailing slash.
	BaseURL string
	HTTP    *http.Client
	// Token is sent as a bearer token when not empty.
	Token string
}

// As returns a copy of the client that presents another token.
func (c *Client) As(name, token string) *Client {
	out := *c
	out.Name, out.Token = name, token
	return &out
}

// Do sends a request to an API path such as "/programs?limit=1". A nil body
// sends none, a string or []byte is sent as it is, and anything else as JSON.
func (c *Client) Do(ctx context.Context, method, path string, body any) *Exchange {
	var raw []byte
	switch b := body.(type) {
	case nil:
	case string:
		raw = []byte(b)
	case []byte:
		raw = b
	default:
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return &Exchange{Client: c.Name, Method: method, URL: c.BaseURL + path, Error: "encoding the request: " + err.Error()}
		}
	}
	header := http.Header{"Accept": {"application/json"}}
	if raw != nil {
		header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		header.Set("Authorization", "Bearer "+c.Token)
	}
	return send(ctx, c.HTTP, c.Name, method, c.BaseURL+path, header, raw, string(raw))
}

// send performs the request and records it; recordedBody is what the record
// shows as the request body.
func send(ctx context.Context, hc *http.Client, name, method, target string, header http.Header, body []byte, recordedBody string) *Exchange {
	ex := &Exchange{Client: name, Method: method, URL: target, RequestBody: clip(recordedBody, maxRecorded), RequestHeaders: map[string]string{}}
	for _, h := range []string{"Content-Type", "Authorization"} {
		if v := header.Get(h); v != "" {
			if h == "Authorization" {
				scheme, _, _ := strings.Cut(v, " ")
				v = scheme + " " + Redacted
			}
			ex.RequestHeaders[h] = v
		}
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		ex.Error = err.Error()
		return ex
	}
	req.Header = header
	start := time.Now()
	resp, err := hc.Do(req)
	ex.DurationMS = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		ex.Error = err.Error()
		return ex
	}
	defer func() { _ = resp.Body.Close() }()
	ex.Status = resp.StatusCode
	ex.ResponseHeaders = map[string]string{}
	for _, h := range []string{"Content-Type", "WWW-Authenticate", "Allow", "Content-Encoding"} {
		if v := resp.Header.Get(h); v != "" {
			ex.ResponseHeaders[h] = v
		}
	}
	ex.body, err = io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		ex.Error = "reading the response: " + err.Error()
	}
	ex.ResponseBody = clip(string(ex.body), maxRecorded)
	return ex
}

// FetchToken performs the OAuth2 client-credentials grant as OpenADR
// defines it: a form POST with the credentials in the body. The recorded
// exchange shows neither the secret nor the token.
func FetchToken(ctx context.Context, hc *http.Client, name, tokenURL, clientID, clientSecret string) (string, *Exchange) {
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {clientID}, "client_secret": {clientSecret}}
	shown := url.Values{"grant_type": {"client_credentials"}, "client_id": {clientID}, "client_secret": {Redacted}}
	header := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}, "Accept": {"application/json"}}
	ex := send(ctx, hc, name, http.MethodPost, tokenURL, header, []byte(form.Encode()), shown.Encode())

	var resp map[string]any
	_ = json.Unmarshal(ex.body, &resp)
	token, _ := resp["access_token"].(string)
	// Keep the shape of the response in the record, without the token.
	for _, key := range []string{"access_token", "refresh_token"} {
		if _, ok := resp[key]; ok {
			resp[key] = Redacted
		}
	}
	if resp != nil {
		redacted, _ := json.Marshal(resp)
		ex.ResponseBody = string(redacted)
	} else if token == "" && ex.Status == http.StatusOK {
		ex.ResponseBody = Redacted
	}
	return token, ex
}
