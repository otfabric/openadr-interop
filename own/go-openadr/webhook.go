// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	openadr "github.com/otfabric/go-openadr"
	"github.com/otfabric/go-openadr/client"
	"github.com/otfabric/go-openadr/notification/webhook"
	"github.com/otfabric/go-openadr/server/storage"
)

// listen-webhook (contract 1.5): a subscription whose callback is
// webhook.Handler of go-openadr, served by this process.

var webhookFlags = []string{"listen-port", "callback-url", "program-id", "objects", "operations", "messages", "listen-ms"}

var (
	webhookObjects    = []string{"PROGRAM", "EVENT", "REPORT", "SUBSCRIPTION", "VEN", "RESOURCE"}
	webhookOperations = []string{"READ", "CREATE", "UPDATE", "DELETE"}
)

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
		if !openadr.ValidID(v) {
			return nil, usagef("--program-id: %q is not an object ID", v)
		}
		w.programID = v
	}
	if w.objects, err = enumList("objects", valueOr(given, "objects", "EVENT"), webhookObjects); err != nil {
		return nil, err
	}
	if w.operations, err = enumList("operations", valueOr(given, "operations", "CREATE"), webhookOperations); err != nil {
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

// listenWebhook serves webhook.Handler, subscribes with the library's
// client for the role, waits for the notifications and deletes the
// subscription again.
func listenWebhook(r *clientRequest, hc *http.Client) (any, error) {
	w := r.webhook
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	bearer := hex.EncodeToString(raw)

	var mu sync.Mutex
	var messages []any
	full := make(chan struct{})
	handler := &webhook.Handler{BearerToken: bearer, Notify: func(_ context.Context, n openadr.Notification) error {
		payload, err := notificationJSON(n)
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, payload)
		if len(messages) == w.messages {
			close(full)
		}
		return nil
	}}
	ln, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(w.port)))
	if err != nil {
		return nil, fmt.Errorf("the webhook receiver: %w", err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ErrorLog: slog.NewLogLogger(slog.NewTextHandler(os.Stderr, nil), slog.LevelWarn)}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	op := openadr.ObjectOperation{CallbackURL: w.callbackURL, BearerToken: bearer}
	for _, o := range w.objects {
		op.Objects = append(op.Objects, openadr.ObjectType(o))
	}
	for _, o := range w.operations {
		op.Operations = append(op.Operations, openadr.Operation(o))
	}
	sub := openadr.Subscription{ClientName: r.who.Name, ProgramID: w.programID, ObjectOperations: []openadr.ObjectOperation{op}}
	if err := sub.Validate(); err != nil {
		return nil, invalidRequest{err}
	}
	cfg := client.Config{BaseURL: r.url, HTTPClient: hc, UserAgent: "openadr-interop-otfabric/" + adapterVersion,
		Credentials: &client.Credentials{ClientID: r.who.ClientID, ClientSecret: r.who.ClientSecret, TokenURL: r.tokenURL}}
	var c interface {
		CreateSubscription(context.Context, openadr.Subscription) (openadr.Subscription, error)
		DeleteSubscription(context.Context, string) (openadr.Subscription, error)
	}
	if r.who.Role == "bl" {
		c, err = client.NewBL(cfg)
	} else {
		c, err = client.NewVEN(cfg)
	}
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	created, err := c.CreateSubscription(ctx, sub)
	if err != nil {
		return nil, err
	}
	defer func() { _, _ = c.DeleteSubscription(context.Background(), created.ID) }()
	b, _ := json.Marshal(map[string]any{"event": "subscribed", "subscription": created.ID, "callbackUrl": w.callbackURL})
	fmt.Fprintln(os.Stderr, string(b))

	timer := time.NewTimer(w.listen)
	defer timer.Stop()
	select {
	case <-full:
	case <-timer.C:
	}
	mu.Lock()
	got := append([]any{}, messages...)
	mu.Unlock()
	if len(got) < w.messages {
		return nil, fmt.Errorf("%w: %d of %d notifications in %s", context.DeadlineExceeded, len(got), w.messages, w.listen)
	}
	subJSON, err := subscriptionJSON(created)
	if err != nil {
		return nil, err
	}
	return map[string]any{"subscription": subJSON, "callbackUrl": w.callbackURL, "messages": got[:w.messages]}, nil
}

// notificationJSON writes a notification as go-openadr read it, in the
// JSON form of OpenADR 3.1.0 (notification).
func notificationJSON(n openadr.Notification) (map[string]any, error) {
	var object json.RawMessage
	var err error
	switch o := n.Object.(type) {
	case openadr.Program:
		object, err = storage.Encode(o)
	case openadr.Event:
		object, err = storage.Encode(o)
	case openadr.Report:
		object, err = storage.Encode(o)
	case openadr.VEN:
		object, err = storage.Encode(o)
	case openadr.Resource:
		object, err = storage.Encode(o)
	case openadr.ResourceGroup:
		object, err = storage.Encode(o)
	case openadr.Subscription:
		object, err = subscriptionJSON(o)
	default:
		object, err = json.Marshal(o)
	}
	if err != nil {
		return nil, err
	}
	out := map[string]any{"objectType": n.ObjectType, "operation": n.Operation, "object": object}
	if n.Targets != nil {
		out["targets"] = n.Targets
	}
	return out, nil
}
