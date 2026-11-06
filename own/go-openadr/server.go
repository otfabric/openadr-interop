// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	openadr "github.com/otfabric/go-openadr"
	"github.com/otfabric/go-openadr/auth"
	"github.com/otfabric/go-openadr/client"
	"github.com/otfabric/go-openadr/notification/webhook"
	"github.com/otfabric/go-openadr/server"
	"github.com/otfabric/go-openadr/server/storage"
)

type serverOptions struct {
	fixture   string
	bind      string
	port      int
	publicURL string
	ready     string
}

func parseServer(args []string) (serverOptions, error) {
	given, err := parseFlags(args, "fixture", "bind-address", "port", "public-url", "ready-file")
	if err != nil {
		return serverOptions{}, err
	}
	o := serverOptions{
		fixture: fixtureRoot + "/baseline/fixture.json",
		bind:    "0.0.0.0",
		ready:   readyFile,
	}
	if v, ok := given["fixture"]; ok {
		o.fixture = v
	}
	if v, ok := given["bind-address"]; ok {
		if net.ParseIP(v) == nil {
			return o, usagef("--bind-address: %q is not an IP address", v)
		}
		o.bind = v
	}
	if o.port, err = number("port", valueOr(given, "port", "8080"), 1, 65535); err != nil {
		return o, err
	}
	if v, ok := given["ready-file"]; ok {
		if v == "" {
			return o, usagef("--ready-file is empty")
		}
		o.ready = v
	}
	host, _ := os.Hostname()
	o.publicURL = trimSlash(valueOr(given, "public-url", "http://"+net.JoinHostPort(host, strconv.Itoa(o.port))))
	if !httpURL.MatchString(o.publicURL) {
		return o, usagef("--public-url: %q is not an http or https URL", o.publicURL)
	}
	return o, nil
}

func valueOr(m map[string]string, key, def string) string {
	if v, ok := m[key]; ok {
		return v
	}
	return def
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func emit(event map[string]any) {
	b, _ := json.Marshal(event)
	fmt.Println(string(b))
}

func runServer(args []string) int {
	o, err := parseServer(args)
	if err != nil {
		return usage(err.Error())
	}
	f, err := loadFixture(o.fixture)
	if err == nil {
		err = served(f)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "openadr-interop: invalid fixture:", err)
		return exitInvalidFixture
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, o, f, logger); err != nil {
		fmt.Fprintln(os.Stderr, "openadr-interop:", err)
		return exitFailed
	}
	emit(map[string]any{"event": "stopped"})
	return exitOK
}

// vtn is go-openadr's VTN with its own token endpoint, as an application
// embeds it: an in-memory store, auth.Issuer for the clients of the fixture,
// webhook.Sender for subscriptions, and resource groups.
type vtn struct {
	handler *server.Server
	sender  *webhook.Sender
	http    *http.Server
	ln      net.Listener
}

func newVTN(o serverOptions, f *fixture, logger *slog.Logger) (*vtn, error) {
	clients := make([]auth.Client, 0, len(f.Clients))
	for _, c := range f.Clients {
		scopes := auth.VENScopes()
		if c.Role == "bl" {
			scopes = auth.BusinessLogicScopes()
		}
		clients = append(clients, auth.Client{ID: c.ClientID, Secret: c.ClientSecret, Scopes: scopes})
	}
	issuer, err := auth.NewIssuer(auth.IssuerConfig{Clients: clients})
	if err != nil {
		return nil, err
	}
	// Callback URLs on the test network are plain http on private addresses.
	sender := webhook.NewSender(webhook.SenderConfig{AllowInsecure: true, Logger: logger})
	handler, err := server.New(server.Config{
		Store:          storage.NewMemory(),
		Validator:      issuer,
		TokenURL:       o.publicURL + "/auth/token",
		TokenHandler:   issuer,
		Webhooks:       sender,
		ResourceGroups: true,
		Logger:         logger,
	})
	if err != nil {
		_ = sender.Close(context.Background())
		return nil, err
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(o.bind, strconv.Itoa(o.port)))
	if err != nil {
		_ = handler.Close()
		_ = sender.Close(context.Background())
		return nil, err
	}
	v := &vtn{handler: handler, sender: sender, ln: ln, http: &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}}
	go func() { _ = v.http.Serve(ln) }()
	return v, nil
}

func (v *vtn) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = v.handler.Close()
	_ = v.http.Shutdown(ctx)
	_ = v.sender.Close(ctx)
}

// localURL is where the adapter itself reaches the VTN, to load the fixture.
func (v *vtn) localURL() string {
	addr := v.ln.Addr().(*net.TCPAddr)
	ip := addr.IP
	if ip.IsUnspecified() {
		ip = net.IPv4(127, 0, 0, 1)
	}
	return "http://" + net.JoinHostPort(ip.String(), strconv.Itoa(addr.Port))
}

func serve(ctx context.Context, o serverOptions, f *fixture, logger *slog.Logger) error {
	v, err := newVTN(o, f, logger)
	if err != nil {
		return err
	}
	defer v.stop()
	if err := load(ctx, v.localURL(), f); err != nil {
		return fmt.Errorf("loading the fixture: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(o.ready), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(o.ready, []byte("ready\n"), 0o644); err != nil {
		return err
	}
	defer func() { _ = os.Remove(o.ready) }()
	emit(map[string]any{
		"event": "ready", "adapter": adapterName, "address": net.JoinHostPort(o.bind, strconv.Itoa(v.ln.Addr().(*net.TCPAddr).Port)),
		"url": o.publicURL, "tokenUrl": o.publicURL + "/auth/token", "fixture": f.Name,
	})
	<-ctx.Done()
	return nil
}

// load creates the objects of the fixture through the VTN's own API, as its
// Business Logic client: the VTN assigns the IDs.
func load(ctx context.Context, baseURL string, f *fixture) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var bl fixtureClient
	clients := map[string]fixtureClient{}
	for _, c := range f.Clients {
		clients[c.Name] = c
		if c.Role == "bl" && bl.Name == "" {
			bl = c
		}
	}
	c, err := client.NewBL(client.Config{BaseURL: baseURL, Credentials: &client.Credentials{
		ClientID: bl.ClientID, ClientSecret: bl.ClientSecret, TokenURL: baseURL + "/auth/token",
	}})
	if err != nil {
		return err
	}
	vens := map[string]openadr.VEN{}
	for _, raw := range f.VENs {
		v := openadr.VEN{Name: raw["venName"].(string), ClientID: clients[raw["client"].(string)].ClientID, Targets: stringList(raw["targets"])}
		if v, err = c.CreateVEN(ctx, v); err != nil {
			return fmt.Errorf("ven %s: %w", raw["venName"], err)
		}
		vens[v.Name] = v
	}
	for _, raw := range f.Resources {
		ven := vens[raw["ven"].(string)]
		r := openadr.Resource{Name: raw["resourceName"].(string), VENID: ven.ID, ClientID: ven.ClientID, Targets: stringList(raw["targets"])}
		if _, err := c.CreateResource(ctx, r); err != nil {
			return fmt.Errorf("resource %s: %w", r.Name, err)
		}
	}
	programs := map[string]string{}
	for _, raw := range f.Programs {
		p, err := decode[openadr.Program](raw)
		if err != nil {
			return fmt.Errorf("program %s: %w", raw["programName"], err)
		}
		if p, err = c.CreateProgram(ctx, p); err != nil {
			return fmt.Errorf("program %s: %w", raw["programName"], err)
		}
		programs[p.Name] = p.ID
	}
	for _, raw := range f.Events {
		body := map[string]any{}
		for k, val := range raw {
			if k != "program" {
				body[k] = val
			}
		}
		body["programID"] = programs[raw["program"].(string)]
		e, err := decode[openadr.Event](body)
		if err != nil {
			return fmt.Errorf("event %s: %w", raw["eventName"], err)
		}
		if _, err := c.CreateEvent(ctx, e); err != nil {
			return fmt.Errorf("event %s: %w", raw["eventName"], err)
		}
	}
	return nil
}

func stringList(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// decode turns an object in the JSON form of the specification into the
// library's type, with the library's own codec.
func decode[T storage.Object](v any) (T, error) {
	b, err := json.Marshal(v)
	if err != nil {
		var zero T
		return zero, err
	}
	return storage.Decode[T](b)
}
