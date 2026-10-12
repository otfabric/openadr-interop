package suite

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/otfabric/openadr-interop/conformance/internal/probe"
	"github.com/otfabric/openadr-interop/conformance/spec"
)

// A stub that answers a few fixed requests; it is not a VTN.
func stub(t *testing.T) Env {
	t.Helper()
	deleted := map[string]bool{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/server", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tokenURL":"https://idp.example/token"}`))
	})
	mux.HandleFunc("GET /programs/bad", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"programName":"no metadata"}`))
	})
	mux.HandleFunc("GET /programs/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"title":"Not Found","status":500}`))
	})
	mux.HandleFunc("DELETE /programs/{id}", func(_ http.ResponseWriter, r *http.Request) { deleted[r.PathValue("id")] = true })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		if !deleted["a"] || !deleted["b"] {
			t.Errorf("cleanups did not all run: %v", deleted)
		}
	})
	doc, err := spec.Load("3.1.0")
	if err != nil {
		t.Fatal(err)
	}
	c := &probe.Client{Name: "BL", BaseURL: srv.URL, HTTP: srv.Client()}
	return Env{Spec: doc, BL: c, VEN: c, Anonymous: c, Nonce: "n0"}
}

func TestRun(t *testing.T) {
	env := stub(t)
	var order []string
	scenarios := []Scenario{
		{ID: "A-001", Title: "passes", Run: func(t *T) {
			info := t.Object(t.Expect(t.Call(t.BL, "GET", "/auth/server", nil), 200, "getAuthServerInfo"))
			if info["tokenURL"] == nil || t.Name("x") != "x-n0" {
				t.Errorf("unexpected: %v %s", info, t.Name("x"))
			}
			t.Delete(t.BL, "/programs/a")
			t.Cleanup(func() { order = append(order, "first registered") })
			t.Cleanup(func() { order = append(order, "last registered") })
		}},
		{ID: "A-002", Title: "wrong status ends the scenario", Run: func(t *T) {
			t.Delete(t.BL, "/programs/b")
			t.Expect(t.Call(t.BL, "GET", "/auth/server", nil), 201, "getAuthServerInfo")
			t.Errorf("not reached")
		}},
		{ID: "A-003", Title: "a schema fault is recorded and the scenario continues", Run: func(t *T) {
			t.Expect(t.Call(t.BL, "GET", "/programs/bad", nil), 200, "searchProgramByProgramId")
			t.Errorf("reached")
		}},
		{ID: "A-004", Title: "problem status has to match", Run: func(t *T) {
			t.ExpectProblem(t.Call(t.BL, "GET", "/programs/missing", nil), 404, "searchProgramByProgramId")
		}},
		{ID: "A-005", Title: "skips", Run: func(t *T) { t.Skipf("no second VEN") }},
		{ID: "A-006", Title: "panics", Run: func(t *T) { panic("boom") }},
		{ID: "A-007", Title: "a cleanup that fails does not stop the run", Run: func(t *T) {
			t.Cleanup(func() { panic("cleanup") })
		}},
		{ID: "B-001", Title: "filtered out", Run: func(t *T) { t.Errorf("must not run") }},
	}
	results := Run(context.Background(), env, scenarios, regexp.MustCompile(`^A-`))
	if len(results) != 7 {
		t.Fatalf("%d results, want 7", len(results))
	}
	want := []struct {
		status  Status
		message string
	}{
		{Passed, ""},
		{Failed, "status 200, want 201"},
		{Failed, "does not match the schema"},
		{Failed, "problem.status is 500 in a 404 response"},
		{Skipped, "no second VEN"},
		{Failed, "panicked"},
		{Passed, ""},
	}
	for i, w := range want {
		r := results[i]
		if r.Status != w.status || !strings.Contains(strings.Join(r.Messages, "\n"), w.message) {
			t.Errorf("%s: %s %v; want %s with %q", r.ID, r.Status, r.Messages, w.status, w.message)
		}
	}
	if msgs := results[1].Messages; len(msgs) != 1 {
		t.Errorf("A-002 went on after a wrong status: %v", msgs)
	}
	if msgs := strings.Join(results[2].Messages, "\n"); !strings.Contains(msgs, "reached") {
		t.Errorf("A-003 stopped at the schema fault: %v", msgs)
	}
	if len(order) != 2 || order[0] != "last registered" {
		t.Errorf("cleanup order = %v", order)
	}
	// The exchanges of a scenario include its cleanup.
	if ex := results[0].Exchanges; len(ex) != 2 || ex[1].Method != "DELETE" || !strings.Contains(ex[1].Client, "cleanup") {
		t.Errorf("exchanges of A-001 = %+v", ex)
	}
}
