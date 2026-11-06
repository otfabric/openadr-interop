// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cB-Sigma/openadr/oadr3"
)

// listen-webhook (contract 1.5): a subscription whose callback is a webhook
// receiver of this process. cB-Sigma openadr has the VTN side of webhooks
// (delivery, the echo check, the bearer token) and the client that writes
// the subscription; it has no receiver, so the receiver is the adapter's: it
// answers the echo check, refuses a notification without the subscription's
// bearer token, and keeps the JSON of each one it accepts.

var webhookFlags = []string{"listen-port", "callback-url", "program-id", "objects", "operations", "messages", "listen-ms"}

type webhookRequest struct {
	port        int
	callbackURL string
	programID   string
	objects     []string
	operations  []string
	messages    int
	listen      time.Duration
}

func parseWebhook(given map[string]string) (*webhookRequest, error) {
	w := &webhookRequest{callbackURL: given["callback-url"]}
	var err error
	if !httpURL.MatchString(w.callbackURL) {
		return nil, usagef("--callback-url: %q is not an http or https URL", w.callbackURL)
	}
	if w.port, err = number("listen-port", valueOr(given, "listen-port", "8090"), 1, 65535); err != nil {
		return nil, err
	}
	if v, ok := given["program-id"]; ok {
		if !objectID.MatchString(v) {
			return nil, usagef("--program-id: %q is not an object ID", v)
		}
		w.programID = v
	}
	if w.objects, err = enumList("objects", valueOr(given, "objects", oadr3.TypeEvent), oadr3.ObjectTypes); err != nil {
		return nil, err
	}
	if w.operations, err = enumList("operations", valueOr(given, "operations", "CREATE"), oadr3.Operations); err != nil {
		return nil, err
	}
	if w.messages, err = number("messages", valueOr(given, "messages", "1"), 1, 1000); err != nil {
		return nil, err
	}
	ms, err := number("listen-ms", valueOr(given, "listen-ms", "10000"), 1, 600000)
	if err != nil {
		return nil, err
	}
	w.listen = time.Duration(ms) * time.Millisecond
	return w, nil
}

func enumList(flag, text string, allowed []string) ([]string, error) {
	var out []string
	for _, v := range strings.Split(text, ",") {
		if !contains(allowed, v) || contains(out, v) {
			return nil, usagef("--%s: %q is not a list of distinct values of %s", flag, text, strings.Join(allowed, ", "))
		}
		out = append(out, v)
	}
	return out, nil
}

// receiver is the callback: GET ?echo= is the VTN's check of the URL, POST
// a notification.
type receiver struct {
	bearer string
	want   int

	mu       sync.Mutex
	messages []any
	refused  int
	full     chan struct{}
}

func (h *receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		echo := r.URL.Query().Get("echo")
		if echo == "" || len(echo) > 256 {
			http.Error(w, "an echo parameter is required", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = io.WriteString(w, echo)
	case http.MethodPost:
		if r.Header.Get("Authorization") != "Bearer "+h.bearer {
			h.mu.Lock()
			h.refused++
			h.mu.Unlock()
			http.Error(w, "the bearer token of the subscription is required", http.StatusForbidden)
			return
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var n map[string]any
		if err != nil || json.Unmarshal(b, &n) != nil || n == nil {
			http.Error(w, "a notification is a JSON object", http.StatusBadRequest)
			return
		}
		h.mu.Lock()
		h.messages = append(h.messages, n)
		if len(h.messages) == h.want {
			close(h.full)
		}
		h.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
	}
}

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// listenWebhook serves the receiver, subscribes with the library's client,
// waits for the notifications and deletes the subscription again.
func listenWebhook(r *clientRequest) (any, bool, int, error) {
	w := r.webhook
	h := &receiver{bearer: randomToken(), want: w.messages, full: make(chan struct{})}
	ln, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(w.port)))
	if err != nil {
		return nil, false, 0, &failure{phase: phaseAPI, err: fmt.Errorf("the webhook receiver: %w", err)}
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second,
		ErrorLog: slog.NewLogLogger(slog.NewTextHandler(os.Stderr, nil), slog.LevelWarn)}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	objects := make([]any, len(w.objects))
	for i, o := range w.objects {
		objects[i] = o
	}
	operations := make([]any, len(w.operations))
	for i, o := range w.operations {
		operations[i] = o
	}
	body := map[string]any{
		"clientName": r.who.Name,
		"objectOperations": []any{map[string]any{
			"objects": objects, "operations": operations, "callbackUrl": w.callbackURL, "bearerToken": h.bearer,
		}},
	}
	if w.programID != "" {
		body["programID"] = w.programID
	}
	checked, err := oadr3.CheckRequest(oadr3.TypeSubscription, body)
	if err != nil {
		return nil, false, 0, &failure{phase: phaseRequest, err: err}
	}
	c := oadr3.NewClient(oadr3.ClientConfig{BaseURL: r.url, TokenURL: r.tokenURL, ClientID: r.who.ClientID,
		Secret: r.who.ClientSecret, Timeout: r.timeout})
	ctx := context.Background()
	if _, err := c.Token(ctx); err != nil {
		return nil, false, 0, &failure{phase: phaseToken, err: err}
	}
	var sub oadr3.Object
	status, err := c.Do(ctx, http.MethodPost, "/subscriptions", nil, checked, &sub)
	if err != nil {
		return nil, true, status, &failure{phase: phaseAPI, status: status, err: err}
	}
	id := oadr3.Str(sub, "id")
	defer func() {
		if id != "" {
			_, _ = c.Do(context.Background(), http.MethodDelete, "/subscriptions/"+id, nil, nil, nil)
		}
	}()
	// For people, and for a test that has to know when to change something.
	b, _ := json.Marshal(map[string]any{"event": "subscribed", "subscription": id, "callbackUrl": w.callbackURL})
	fmt.Fprintln(os.Stderr, string(b))

	timer := time.NewTimer(w.listen)
	defer timer.Stop()
	select {
	case <-h.full:
	case <-timer.C:
	}
	h.mu.Lock()
	messages := append([]any{}, h.messages...)
	refused := h.refused
	h.mu.Unlock()
	if len(messages) < w.messages {
		return nil, true, status, &failure{phase: phaseAPI, status: status, err: fmt.Errorf("%w: %d of %d notifications in %s (%d refused for a wrong bearer token)",
			context.DeadlineExceeded, len(messages), w.messages, w.listen, refused)}
	}
	return map[string]any{"subscription": sub, "callbackUrl": w.callbackURL, "messages": messages[:w.messages]}, true, status, nil
}
