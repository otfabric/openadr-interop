// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The repository's fixtures: ../../fixtures, or /fixtures in the image build.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	for _, root := range []string{"../../fixtures", fixtureRoot} {
		p := filepath.Join(root, name, "fixture.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skipf("no fixture %s", name)
	return ""
}

func TestFixturesOfTheRepositoryAreValid(t *testing.T) {
	for _, name := range []string{"baseline", "baseline-301", "descriptors"} {
		if _, err := loadFixture(fixturePath(t, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestInvalidFixtures(t *testing.T) {
	data, err := os.ReadFile(fixturePath(t, "baseline"))
	if err != nil {
		t.Fatal(err)
	}
	var base map[string]any
	if err := json.Unmarshal(data, &base); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(m map[string]any){
		"no bl client": func(m map[string]any) {
			var kept []any
			for _, c := range m["clients"].([]any) {
				if c.(map[string]any)["role"] != "bl" {
					kept = append(kept, c)
				}
			}
			m["clients"] = kept
		},
		"event of an unknown program": func(m map[string]any) { m["events"].([]any)[0].(map[string]any)["program"] = "nope" },
		"two programs of one name": func(m map[string]any) {
			p := m["programs"].([]any)
			p[1].(map[string]any)["programName"] = p[0].(map[string]any)["programName"]
		},
		"unknown property":    func(m map[string]any) { m["extra"] = 1 },
		"short secret":        func(m map[string]any) { m["clients"].([]any)[0].(map[string]any)["clientSecret"] = "short" },
		"ven of a bl client":  func(m map[string]any) { m["vens"].([]any)[0].(map[string]any)["client"] = "bl" },
		"resource of nothing": func(m map[string]any) { m["resources"].([]any)[0].(map[string]any)["ven"] = "nope" },
		"duplicate targets": func(m map[string]any) {
			m["programs"].([]any)[1].(map[string]any)["targets"] = []any{"a", "a"}
		},
		"programID in an event": func(m map[string]any) { m["events"].([]any)[0].(map[string]any)["programID"] = "x" },
		"unknown revision":      func(m map[string]any) { m["openadrVersion"] = "3.0.0" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			mutate(m)
			b, _ := json.Marshal(m)
			if _, err := parseFixture(b, "baseline"); !isFixtureError(err) {
				t.Errorf("accepted, err %v", err)
			}
		})
	}
	if _, err := parseFixture(data, "other"); err == nil {
		t.Error("a fixture whose name is not its directory was accepted")
	}
	if _, err := parseFixture([]byte("{ not json"), "baseline"); err == nil {
		t.Error("not JSON was accepted")
	}
	f, err := loadFixture(fixturePath(t, "baseline-301"))
	if err != nil {
		t.Fatal(err)
	}
	if served(f) == nil {
		t.Error("a fixture of 3.0.1 is served")
	}
}

func TestUsageErrors(t *testing.T) {
	fx := fixturePath(t, "baseline")
	bad := [][]string{
		{},
		{"frobnicate"},
		{"server", "--frobnicate", "1"},
		{"server", "--port", "http"},
		{"server", "--port", "0"},
		{"server", "--public-url", "nowhere"},
		{"server", "--mqtt-port", "1883"},
		{"server", "--port", "1", "--port", "2"},
		{"client"},
		{"client", "frobnicate", "--url", "http://x", "--as", "bl"},
		{"client", "list-programs", "--as", "bl", "--fixture", fx},
		{"client", "list-programs", "--url", "http://x", "--fixture", fx},
		{"client", "list-programs", "--url", "nowhere", "--as", "bl", "--fixture", fx},
		{"client", "list-programs", "--url", "http://x", "--as", "nobody", "--fixture", fx},
		{"client", "list-programs", "--url", "http://x", "--as", "bl", "--fixture", fx, "--id", "abc"},
		{"client", "get-program", "--url", "http://x", "--as", "bl", "--fixture", fx},
		{"client", "get-program", "--url", "http://x", "--as", "bl", "--fixture", fx, "--id", "a/b"},
		{"client", "create-program", "--url", "http://x", "--as", "bl", "--fixture", fx, "--body", "{not json"},
		{"client", "create-program", "--url", "http://x", "--as", "bl", "--fixture", fx, "--body", "[]"},
		{"client", "create-program", "--url", "http://x", "--as", "ven-1", "--fixture", fx, "--body", "{}"},
		{"client", "create-report", "--url", "http://x", "--as", "bl", "--fixture", fx, "--body", "{}"},
		{"client", "list-programs", "--url", "http://x", "--as", "bl", "--fixture", fx, "--limit", "many"},
		{"client", "list-programs", "--url", "http://x", "--as", "bl", "--fixture", "/no/such/fixture.json"},
		{"client", "list-programs", "--url", "http://x", "--as", "bl", "--fixture", fixturePath(t, "baseline-301")},
		{"client", "list-programs", "--url", "http://x", "--as", "bl", "--anonymous", "bl", "--fixture", fx},
		{"client", "list-programs", "--url", "http://x", "--anonymous", "admin"},
		{"client", "list-programs", "--url", "http://x", "--anonymous", "ven", "--token-url", "http://x/t"},
		{"client", "create-program", "--url", "http://x", "--anonymous", "ven", "--body", "{}"},
		{"client", "get-topics", "--url", "http://x", "--as", "bl", "--fixture", fx},
		{"client", "get-topics", "--url", "http://x", "--as", "bl", "--fixture", fx, "--scope", "everything"},
		{"client", "get-topics", "--url", "http://x", "--as", "ven-1", "--fixture", fx, "--scope", "ven-events"},
		{"client", "get-topics", "--url", "http://x", "--as", "bl", "--fixture", fx, "--scope", "programs", "--id", "abc"},
		{"client", "list-programs", "--url", "http://x", "--as", "bl", "--fixture", fx, "--targets", "a,,b"},
		{"client", "list-programs", "--url", "http://x", "--as", "bl", "--fixture", fx, "--timeout-ms", "0"},
		{"client", "listen-mqtt", "--url", "http://x", "--as", "bl", "--fixture", fx, "--scope", "programs"},
		{"client", "listen-webhook", "--url", "http://x", "--as", "bl", "--fixture", fx},
		{"client", "listen-webhook", "--url", "http://x", "--anonymous", "ven", "--callback-url", "http://x/n"},
		{"client", "listen-webhook", "--url", "http://x", "--as", "bl", "--fixture", fx, "--callback-url", "http://x/n", "--objects", "EVENT,BOGUS"},
		{"client", "listen-webhook", "--url", "http://x", "--as", "bl", "--fixture", fx, "--callback-url", "http://x/n", "--messages", "0"},
		{"print-fixture", "a", "b"},
		{"version", "x"},
	}
	for _, args := range bad {
		if code := run(args); code != exitUsage {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

func TestCapabilitiesAreConsistent(t *testing.T) {
	caps := capabilities()
	ops := caps["clientOperations"].(map[string][]string)
	for _, role := range []string{"bl", "ven"} {
		for _, op := range ops[role] {
			args := []string{op, "--url", "http://x", "--as", map[string]string{"bl": "bl", "ven": "ven-1"}[role], "--fixture", fixturePath(t, "baseline")}
			switch {
			case strings.HasPrefix(op, "get-topics"):
				args = append(args, "--scope", "programs")
			case op == "listen-webhook":
				args = append(args, "--callback-url", "http://x:8090/notify")
			case strings.HasPrefix(op, "get-") && op != "get-notifiers", strings.HasPrefix(op, "delete-"):
				args = append(args, "--id", "abc")
			case strings.HasPrefix(op, "create-"):
				args = append(args, "--body", "{}")
			case strings.HasPrefix(op, "update-"):
				args = append(args, "--id", "abc", "--body", "{}")
			}
			if _, err := parseClient(args); err != nil {
				t.Errorf("%s %s is declared and refused: %v", role, op, err)
			}
		}
	}
	if contains(ops["bl"], "create-report") || contains(ops["ven"], "create-program") {
		t.Error("an operation the library does not offer is declared")
	}
}

// vtnForTest runs the VTN of the adapter on a loopback port with a fixture.
func vtnForTest(t *testing.T, name string) (string, *fixture) {
	t.Helper()
	f, err := loadFixture(fixturePath(t, name))
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	url := "http://127.0.0.1:" + itoa(port)
	o := serverOptions{bind: "127.0.0.1", port: port, publicURL: url, ready: filepath.Join(t.TempDir(), "ready")}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, o, f, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(o.ready); err == nil {
			return url, f
		}
		select {
		case err := <-done:
			t.Fatalf("the VTN stopped: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the VTN did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func do(t *testing.T, url string, f *fixture, op string, args ...string) (resultDocument, int) {
	t.Helper()
	r, err := parseClient(append([]string{op, "--url", url, "--fixture", fixturePath(t, f.Name)}, args...))
	if err != nil {
		t.Fatalf("%s %v: %v", op, args, err)
	}
	return execute(r)
}

func count(d resultDocument) int {
	b, _ := json.Marshal(d.Response)
	var list []any
	if json.Unmarshal(b, &list) == nil {
		return len(list)
	}
	if d.Response == nil {
		return 0
	}
	return 1
}

func TestClientAgainstTheVTN(t *testing.T) {
	url, f := vtnForTest(t, "baseline")
	ids := map[string]string{}
	d, _ := do(t, url, f, "list-programs", "--as", "bl")
	b, _ := json.Marshal(d.Response)
	var programs []struct{ ID, ProgramName string }
	_ = json.Unmarshal(b, &programs)
	for _, p := range programs {
		ids[p.ProgramName] = p.ID
	}

	type want struct {
		code   int
		ok     bool
		err    string
		status int
		n      int
	}
	cases := []struct {
		op   string
		args []string
		want want
	}{
		{"list-programs", []string{"--as", "bl"}, want{0, true, "", 200, 2}},
		{"list-programs", []string{"--as", "ven-2"}, want{0, true, "", 200, 1}},
		{"list-programs", []string{"--as", "ven-1", "--targets", "group-a"}, want{0, true, "", 200, 1}},
		{"list-programs", []string{"--as", "bl", "--skip", "1", "--limit", "50"}, want{0, true, "", 200, 1}},
		{"list-programs", []string{"--as", "bl", "--skip", "0", "--limit", "51"}, want{1, false, "http-error", 400, 0}},
		{"get-program", []string{"--as", "bl", "--id", "no-such-program"}, want{1, false, "http-error", 404, 0}},
		{"get-program", []string{"--as", "ven-1", "--id", ids["group-a-flex"]}, want{0, true, "", 200, 1}},
		{"list-events", []string{"--as", "bl", "--program-id", ids["day-ahead"]}, want{0, true, "", 200, 1}},
		{"list-vens", []string{"--as", "ven-1"}, want{0, true, "", 200, 1}},
		{"list-resources", []string{"--as", "bl"}, want{0, true, "", 200, 1}},
		{"list-reports", []string{"--as", "bl"}, want{0, true, "", 200, 0}},
		{"list-subscriptions", []string{"--as", "ven-1"}, want{0, true, "", 200, 0}},
		{"list-resource-groups", []string{"--as", "bl"}, want{0, true, "", 200, 0}},
		{"get-notifiers", []string{"--as", "bl"}, want{0, true, "", 200, 1}},
		{"create-program", []string{"--as", "bl", "--body", `{"targets":["group-a"]}`}, want{1, false, "invalid-request", 0, 0}},
		{"create-subscription", []string{"--as", "ven-1", "--body", `{"clientName":"site-1"}`}, want{1, false, "invalid-request", 0, 0}},
		{"create-program", []string{"--as", "bl", "--body", `{"programName":"day-ahead"}`}, want{1, false, "http-error", 409, 0}},
		{"create-program", []string{"--as", "bl", "--body", `{"programName":"extra","targets":["group-a"]}`}, want{0, true, "", 201, 1}},
		{"create-ven", []string{"--as", "ven-3", "--body", `{"venName":"site-3"}`}, want{0, true, "", 201, 1}},
		{"list-programs", []string{"--anonymous", "ven"}, want{1, false, "http-error", 401, 0}},
	}
	for _, c := range cases {
		d, code := do(t, url, f, c.op, c.args...)
		got := want{code, d.OK, "", 0, count(d)}
		if d.Error != nil {
			got.err = d.Error.Code
		}
		if d.Status != nil {
			got.status = *d.Status
		}
		if got != c.want {
			t.Errorf("%s %v: got %+v, want %+v (%v)", c.op, c.args, got, c.want, d.Error)
		}
	}

	// A wrong secret is no token; a VTN that cannot be reached is exit 3.
	wrong := *f
	wrong.Clients = append([]fixtureClient(nil), f.Clients...)
	for i := range wrong.Clients {
		wrong.Clients[i].ClientSecret = "not-the-secret"
	}
	r, _ := parseClient([]string{"list-programs", "--url", url, "--as", "ven-1", "--fixture", fixturePath(t, "baseline")})
	r.who = wrong.Clients[1]
	if d, code := execute(r); code != 1 || d.Error == nil || d.Error.Code != "auth-failed" || d.Authenticated {
		t.Errorf("wrong secret: exit %d, %+v", code, d)
	}
	r.url = "http://127.0.0.1:1"
	if d, code := execute(r); code != 3 || d.Error == nil || d.Error.Code != "connect-failed" || d.Status != nil {
		t.Errorf("no VTN: exit %d, %+v", code, d)
	}
}

func TestTheWorkflow(t *testing.T) {
	url, f := vtnForTest(t, "baseline")
	d, _ := do(t, url, f, "create-program", "--as", "bl", "--body",
		`{"programName":"workflow","payloadDescriptors":[{"objectType":"EVENT_PAYLOAD_DESCRIPTOR","payloadType":"PRICE","units":"KWH","currency":"EUR"}]}`)
	program := idOf(t, d)
	d, _ = do(t, url, f, "create-event", "--as", "bl", "--body", `{"programID":"`+program+`","eventName":"prices","priority":3,`+
		`"intervalPeriod":{"start":"2031-06-01T10:00:00Z","duration":"PT15M"},"reportDescriptors":[{"payloadType":"USAGE"}],`+
		`"intervals":[{"id":0,"payloads":[{"type":"PRICE","values":[0.31]}]}]}`)
	event := idOf(t, d)
	d, _ = do(t, url, f, "create-report", "--as", "ven-2", "--body", `{"eventID":"`+event+`","clientName":"site-2","reportName":"usage",`+
		`"resources":[{"resourceName":"meter","intervals":[{"id":0,"payloads":[{"type":"USAGE","values":[1.8]}]}]}]}`)
	report := idOf(t, d)
	if !strings.Contains(string(mustJSON(d.Response)), `"clientID":"interop-ven-2"`) {
		t.Errorf("the report is not of the VEN's client: %s", mustJSON(d.Response))
	}
	if d, _ = do(t, url, f, "list-reports", "--as", "ven-1", "--event-id", event); count(d) != 0 {
		t.Error("another VEN sees the report")
	}
	if d, _ = do(t, url, f, "update-report", "--as", "ven-2", "--id", report, "--body", `{"eventID":"`+event+`","clientName":"site-2",`+
		`"reportName":"usage-corrected","resources":[{"resourceName":"meter","intervals":[{"id":0,"payloads":[{"type":"USAGE","values":[1.9]}]}]}]}`); !d.OK {
		t.Errorf("update-report: %+v", d.Error)
	}
	for _, step := range [][]string{{"delete-report", "ven-2", report}, {"delete-event", "bl", event}, {"delete-program", "bl", program}} {
		if d, _ = do(t, url, f, step[0], "--as", step[1], "--id", step[2]); !d.OK {
			t.Errorf("%s: %+v", step[0], d.Error)
		}
	}
	if d, _ = do(t, url, f, "get-program", "--as", "bl", "--id", program); d.OK || *d.Status != 404 {
		t.Errorf("the program is not gone: %+v", d)
	}
}

func idOf(t *testing.T, d resultDocument) string {
	t.Helper()
	var v struct{ ID string }
	if !d.OK || json.Unmarshal(mustJSON(d.Response), &v) != nil || v.ID == "" {
		t.Fatalf("no object: %+v", d.Error)
	}
	return v.ID
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func TestListenWebhook(t *testing.T) {
	url, f := vtnForTest(t, "baseline")
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := itoa(l.Addr().(*net.TCPAddr).Port)
	_ = l.Close()
	d, _ := do(t, url, f, "list-programs", "--as", "bl")
	var programs []struct{ ID, ProgramName string }
	_ = json.Unmarshal(mustJSON(d.Response), &programs)
	program := ""
	for _, p := range programs {
		if p.ProgramName == "day-ahead" {
			program = p.ID
		}
	}

	type result struct {
		d    resultDocument
		code int
	}
	done := make(chan result, 1)
	go func() {
		r, err := parseClient([]string{"listen-webhook", "--url", url, "--as", "ven-1", "--fixture", fixturePath(t, "baseline"),
			"--listen-port", port, "--callback-url", "http://127.0.0.1:" + port + "/notify", "--program-id", program,
			"--listen-ms", "10000"})
		if err != nil {
			t.Error(err)
			done <- result{}
			return
		}
		d, code := execute(r)
		done <- result{d, code}
	}()
	// The subscription is in place once the VTN lists it.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if d, _ := do(t, url, f, "list-subscriptions", "--as", "ven-1"); count(d) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no subscription")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if d, _ := do(t, url, f, "create-event", "--as", "bl", "--body", `{"programID":"`+program+`","eventName":"hook"}`); !d.OK {
		t.Fatalf("create-event: %+v", d.Error)
	}
	got := <-done
	if got.code != 0 || !got.d.OK {
		t.Fatalf("listen-webhook: exit %d, %+v", got.code, got.d.Error)
	}
	b := string(mustJSON(got.d.Response))
	for _, want := range []string{`"objectType":"EVENT"`, `"operation":"CREATE"`, `"eventName":"hook"`} {
		if !strings.Contains(b, want) {
			t.Errorf("no %s in %s", want, b)
		}
	}
	if d, _ := do(t, url, f, "list-subscriptions", "--as", "ven-1"); count(d) != 0 {
		t.Error("the subscription was not deleted")
	}

	// Nothing happens: too few notifications is a timeout.
	r, err := parseClient([]string{"listen-webhook", "--url", url, "--as", "ven-1", "--fixture", fixturePath(t, "baseline"),
		"--listen-port", port, "--callback-url", "http://127.0.0.1:" + port + "/notify", "--listen-ms", "200"})
	if err != nil {
		t.Fatal(err)
	}
	if d, code := execute(r); code != 1 || d.Error == nil || d.Error.Code != "timeout" {
		t.Errorf("no notification: exit %d, %+v", code, d.Error)
	}
	// A callback that does not answer the echo check is refused by the VTN.
	r.webhook.callbackURL = "http://127.0.0.1:1/notify"
	if d, code := execute(r); code != 1 || d.Error == nil || d.Error.Code != "http-error" || *d.Status != 400 {
		t.Errorf("unreachable callback: exit %d, %+v", code, d.Error)
	}
}
