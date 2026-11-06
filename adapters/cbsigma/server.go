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

	"github.com/cB-Sigma/openadr/oadr3"
	"github.com/cB-Sigma/openadr/oadr3/vtn"
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

// newVTN is the OpenADR 3.1 VTN of cB-Sigma openadr as an application
// configures it: the clients of the fixture at its own token endpoint, the
// in-memory store, and webhooks to plain-http callbacks on the test network.
func newVTN(o serverOptions, f *fixture, logger *slog.Logger) (*vtn.VTN, error) {
	clients := make([]vtn.Client, 0, len(f.Clients))
	for _, c := range f.Clients {
		role := vtn.RoleVEN
		if c.Role == "bl" {
			role = vtn.RoleBL
		}
		clients = append(clients, vtn.Client{ClientID: c.ClientID, Secret: c.ClientSecret, Role: role})
	}
	return vtn.New(vtn.Config{
		PublicURL: o.publicURL,
		Version:   vtn.Version31,
		Clients:   clients,
		Webhooks:  vtn.WebhookConfig{AllowHTTP: true, AllowPrivate: true},
		Logger:    logger,
	})
}

func serve(ctx context.Context, o serverOptions, f *fixture, logger *slog.Logger) error {
	v, err := newVTN(o, f, logger)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	ln, err := net.Listen("tcp", net.JoinHostPort(o.bind, strconv.Itoa(o.port)))
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           v,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := load(ctx, v, f); err != nil {
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
		"event": "ready", "adapter": adapterName, "address": net.JoinHostPort(o.bind, strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)),
		"url": o.publicURL, "tokenUrl": o.publicURL + "/auth/token", "fixture": f.Name,
	})
	<-ctx.Done()
	return nil
}

// load creates the objects of the fixture as business logic in the same
// process (vtn.VTN.Create), which is how the library takes them from an
// application: the VTN assigns the IDs.
func load(ctx context.Context, v *vtn.VTN, f *fixture) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	clients := map[string]fixtureClient{}
	for _, c := range f.Clients {
		clients[c.Name] = c
	}
	type ven struct{ id, clientID string }
	vens := map[string]ven{}
	for _, raw := range f.VENs {
		name := raw["venName"].(string)
		body := oadr3.Object{"objectType": "BL_VEN_REQUEST", "venName": name, "clientID": clients[raw["client"].(string)].ClientID}
		if t, ok := raw["targets"]; ok {
			body["targets"] = t
		}
		created, err := v.Create(ctx, oadr3.TypeVEN, body)
		if err != nil {
			return fmt.Errorf("ven %s: %w", name, err)
		}
		vens[name] = ven{id: oadr3.Str(created, "id"), clientID: body["clientID"].(string)}
	}
	for _, raw := range f.Resources {
		owner := vens[raw["ven"].(string)]
		body := oadr3.Object{"objectType": "BL_RESOURCE_REQUEST", "resourceName": raw["resourceName"], "venID": owner.id, "clientID": owner.clientID}
		if t, ok := raw["targets"]; ok {
			body["targets"] = t
		}
		if _, err := v.Create(ctx, oadr3.TypeResource, body); err != nil {
			return fmt.Errorf("resource %s: %w", raw["resourceName"], err)
		}
	}
	programs := map[string]string{}
	for _, raw := range f.Programs {
		body, err := plain(raw)
		if err != nil {
			return err
		}
		created, err := v.Create(ctx, oadr3.TypeProgram, body)
		if err != nil {
			return fmt.Errorf("program %s: %w", raw["programName"], err)
		}
		programs[raw["programName"].(string)] = oadr3.Str(created, "id")
	}
	for _, raw := range f.Events {
		body, err := plain(raw)
		if err != nil {
			return err
		}
		delete(body, "program")
		body["programID"] = programs[raw["program"].(string)]
		if _, err := v.Create(ctx, oadr3.TypeEvent, body); err != nil {
			return fmt.Errorf("event %s: %w", raw["eventName"], err)
		}
	}
	return nil
}

// plain turns an object of the fixture, read with json.Number, into the
// form encoding/json gives by default (float64 numbers), which is what the
// library's checks take.
func plain(raw map[string]any) (oadr3.Object, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var o oadr3.Object
	return o, json.Unmarshal(b, &o)
}
