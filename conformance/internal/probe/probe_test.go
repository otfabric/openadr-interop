package probe

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDoRecordsWithoutCredentials(t *testing.T) {
	var gotAuth, gotBody, gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotAuth, gotBody, gotType = r.Header.Get("Authorization"), string(body), r.Header.Get("Content-Type")
		w.Header().Set("WWW-Authenticate", "Bearer")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":401}`))
	}))
	defer srv.Close()

	c := &Client{Name: "BL", BaseURL: srv.URL, HTTP: srv.Client(), Token: "s3cr3t-token"}
	ex := c.Do(context.Background(), "POST", "/programs?limit=1", map[string]any{"programName": "p"})
	if gotAuth != "Bearer s3cr3t-token" || gotBody != `{"programName":"p"}` || gotType != "application/json" {
		t.Errorf("the server received Authorization %q, Content-Type %q, body %q", gotAuth, gotType, gotBody)
	}
	if ex.Status != 401 || ex.ResponseHeaders["WWW-Authenticate"] != "Bearer" || ex.RequestBody != gotBody {
		t.Errorf("exchange = %+v", ex)
	}
	if m, _ := ex.JSON().(map[string]any); m["status"] != float64(401) {
		t.Errorf("JSON() = %v", ex.JSON())
	}
	record, _ := json.Marshal(ex)
	if strings.Contains(string(record), "s3cr3t") || ex.RequestHeaders["Authorization"] != "Bearer "+Redacted {
		t.Errorf("the record holds the token: %s", record)
	}

	// A string body goes out as it is; no body sends no Content-Type.
	c.Do(context.Background(), "POST", "/programs", "not json")
	if gotBody != "not json" {
		t.Errorf("raw body arrived as %q", gotBody)
	}
	c.As("anonymous", "").Do(context.Background(), "GET", "/programs", nil)
	if gotAuth != "" || gotType != "" {
		t.Errorf("a request without token or body carried Authorization %q, Content-Type %q", gotAuth, gotType)
	}

	srv.Close()
	if ex := c.Do(context.Background(), "GET", "/programs", nil); ex.Error == "" || ex.Status != 0 {
		t.Errorf("a request to a closed server: %+v", ex)
	}
}

func TestFetchTokenRedactsSecretAndToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != "client_credentials" || r.PostForm.Get("client_secret") != "hunter2" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"tok-123","token_type":"Bearer","expires_in":60}`))
	}))
	defer srv.Close()

	token, ex := FetchToken(context.Background(), srv.Client(), "VEN", srv.URL, "site-1", "hunter2")
	if token != "tok-123" {
		t.Fatalf("token = %q (%s)", token, ex)
	}
	record, _ := json.Marshal(ex)
	for _, secret := range []string{"hunter2", "tok-123"} {
		if strings.Contains(string(record), secret) {
			t.Errorf("the record holds %q: %s", secret, record)
		}
	}
	if !strings.Contains(ex.RequestBody, "client_id=site-1") || !strings.Contains(ex.ResponseBody, `"expires_in":60`) {
		t.Errorf("the record lost the rest of the exchange: %s", record)
	}

	token, ex = FetchToken(context.Background(), srv.Client(), "VEN", srv.URL, "site-1", "wrong-secret")
	record, _ = json.Marshal(ex)
	if token != "" || ex.Status != 401 || strings.Contains(string(record), "wrong-secret") {
		t.Errorf("failed login: token %q, record %s", token, record)
	}
}
