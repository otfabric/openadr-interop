// Package scenarios holds the conformance scenarios for an OpenADR 3.1.0
// VTN. Every expectation here is derived from the specification: the
// OpenAPI document pinned in package spec, and the Definition document
// (20250807-X) the references name.
package scenarios

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/openadr-interop/conformance/internal/probe"
	"github.com/otfabric/openadr-interop/conformance/internal/suite"
)

// All returns every scenario, in the order they run.
func All() []suite.Scenario {
	var all []suite.Scenario
	for _, group := range [][]suite.Scenario{auth(), authorization(), programs(), events(), reports(), vens(), targeting(), workflow()} {
		all = append(all, group...)
	}
	return all
}

type object = map[string]any

func str(t *suite.T, o object, key string) string {
	s, ok := o[key].(string)
	if !ok || s == "" {
		t.Fatalf("the object has no %s: %v", key, o)
	}
	return s
}

// create posts an object as c, requires 201, and deletes the object again
// when the scenario ends.
func create(t *suite.T, c *probe.Client, collection, operationID string, body object) object {
	created := t.Object(t.Expect(t.Call(c, http.MethodPost, "/"+collection, body), http.StatusCreated, operationID))
	t.Delete(c, "/"+collection+"/"+str(t, created, "id"))
	return created
}

func createProgram(t *suite.T, name string, extra object) object {
	body := object{"programName": t.Name(name)}
	for k, v := range extra {
		body[k] = v
	}
	return create(t, t.BL, "programs", "createProgram", body)
}

func createEvent(t *suite.T, programID string, extra object) object {
	body := object{"programID": programID}
	for k, v := range extra {
		body[k] = v
	}
	return create(t, t.BL, "events", "createEvent", body)
}

// ids returns the id of each object.
func ids(t *suite.T, list []object) []string {
	out := make([]string, len(list))
	for i, o := range list {
		out[i] = str(t, o, "id")
	}
	return out
}

func contains(list []string, s string) bool { return slices.Contains(list, s) }

// timestamp parses an RFC 3339 date-time property.
func timestamp(t *suite.T, o object, key string) time.Time {
	ts, err := time.Parse(time.RFC3339, str(t, o, key))
	if err != nil {
		t.Fatalf("%s is not an RFC 3339 date-time: %v", key, err)
	}
	return ts
}

// durationRE is the duration pattern of the specification without its
// lookahead; sameDuration is only given strings the schema check accepted.
var durationRE = regexp.MustCompile(`^(-?)P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)([DW]))?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

// sameDuration reports whether two ISO 8601 durations are equal component
// by component: a VTN may write PT1H as P0Y0M0DT1H0M0S. It does not equate
// different units, such as PT60M and PT1H.
func sameDuration(a, b string) bool {
	parse := func(s string) ([7]float64, bool) {
		var out [7]float64
		m := durationRE.FindStringSubmatch(s)
		if m == nil {
			return out, false
		}
		nonzero := false
		for i, g := range []string{m[2], m[3], m[4], m[6], m[7], m[8]} {
			if g == "" {
				continue
			}
			n, err := strconv.ParseFloat(g, 64)
			if err != nil {
				return out, false
			}
			if i == 2 && m[5] == "W" {
				n *= 7
			}
			out[i] = n
			nonzero = nonzero || n != 0
		}
		// The sign of a zero duration means nothing.
		if m[1] == "-" && nonzero {
			out[6] = 1
		}
		return out, true
	}
	da, okA := parse(a)
	db, okB := parse(b)
	return okA && okB && da == db
}

// --- authentication and authorization

func auth() []suite.Scenario {
	return []suite.Scenario{
		{
			ID:    "AUTH-001",
			Title: "GET /auth/server names the token endpoint without authentication",
			Refs:  []string{"openadr3.yaml#getAuthServerInfo", "Definition: Token endpoint discovery"},
			Run: func(t *suite.T) {
				info := t.Object(t.Expect(t.Call(t.Anonymous, "GET", "/auth/server", nil), 200, "getAuthServerInfo"))
				// The schema says format: uri. Clients hand the value to an
				// OAuth2 library as it is, so a relative reference is of no use.
				tokenURL, err := url.Parse(str(t, info, "tokenURL"))
				if err != nil || tokenURL.Scheme != "http" && tokenURL.Scheme != "https" || tokenURL.Host == "" {
					t.Errorf("tokenURL = %v, want an absolute http or https URL", info["tokenURL"])
				}
			},
		},
		{
			ID:    "AUTH-002",
			Title: "A request without a bearer token is answered 401 with a problem",
			Refs:  []string{"openadr3.yaml#searchAllPrograms", "Definition: OAuth 2.0 client credential flow", "Definition: Problem"},
			Run: func(t *suite.T) {
				t.ExpectProblem(t.Call(t.Anonymous, "GET", "/programs", nil), 401, "searchAllPrograms")
				ex := t.Call(t.Anonymous, "POST", "/programs", object{"programName": t.Name("anonymous")})
				if ex.Status == 201 {
					t.Delete(t.BL, "/programs/"+str(t, t.Object(ex.JSON()), "id"))
				}
				t.ExpectProblem(ex, 401, "createProgram")
			},
		},
		{
			ID:    "AUTH-003",
			Title: "A request with a token the VTN did not issue is answered 401",
			Refs:  []string{"openadr3.yaml#searchAllEvents", "Definition: OAuth 2.0 client credential flow"},
			Run: func(t *suite.T) {
				forged := t.Anonymous.As("forged token", "openadr-interop-not-a-token")
				t.ExpectProblem(t.Call(forged, "GET", "/events", nil), 401, "searchAllEvents")
			},
		},
		{
			ID:    "AUTH-004",
			Title: "A VEN may not write programs or events (403)",
			Refs:  []string{"openadr3.yaml#createProgram", "openadr3.yaml#createEvent", "openadr3.yaml#deleteProgram", "Definition: OpenAPI Specification"},
			Run: func(t *suite.T) {
				program := createProgram(t, "auth-004", nil)
				id := str(t, program, "id")
				t.ExpectProblem(t.Call(t.VEN, "POST", "/programs", object{"programName": t.Name("auth-004-ven")}), 403, "createProgram")
				t.ExpectProblem(t.Call(t.VEN, "PUT", "/programs/"+id, object{"programName": t.Name("auth-004-renamed")}), 403, "updateProgram")
				t.ExpectProblem(t.Call(t.VEN, "DELETE", "/programs/"+id, nil), 403, "deleteProgram")
				t.ExpectProblem(t.Call(t.VEN, "POST", "/events", object{"programID": id}), 403, "createEvent")
				// And nothing happened.
				t.Expect(t.Call(t.BL, "GET", "/programs/"+id, nil), 200, "searchProgramByProgramId")
			},
		},
	}
}

// --- authorization

// operation is a request to one of the 25 operations on programs, events,
// reports, vens and resources, with the objects it needs named by role.
type operation struct {
	id, method, path string
	body             func(ids map[string]string) any
}

// coreOperations lists the 25 operations; %s in a path is the ID of the
// object of that collection in ids.
func coreOperations() []operation {
	program := func(map[string]string) any { return object{"programName": "authz-changed"} }
	event := func(ids map[string]string) any { return object{"programID": ids["programs"]} }
	rep := func(ids map[string]string) any { return report(ids["events"], "authz") }
	ven := func(map[string]string) any {
		return object{"objectType": "VEN_VEN_REQUEST", "venName": "authz-changed"}
	}
	resource := func(ids map[string]string) any {
		return object{"objectType": "VEN_RESOURCE_REQUEST", "resourceName": "authz-changed", "venID": ids["vens"]}
	}
	var ops []operation
	for _, c := range []struct {
		name, plural, search, read, create, update, remove string
		body                                               func(map[string]string) any
	}{
		{"program", "programs", "searchAllPrograms", "searchProgramByProgramId", "createProgram", "updateProgram", "deleteProgram", program},
		{"event", "events", "searchAllEvents", "searchEventsByID", "createEvent", "updateEvent", "deleteEvent", event},
		{"report", "reports", "searchAllReports", "searchReportsByReportID", "createReport", "updateReport", "deleteReport", rep},
		{"ven", "vens", "searchVens", "searchVenByID", "createVen", "updateVen", "deleteVen", ven},
		{"resource", "resources", "searchVenResources", "searchVenResourceByID", "createResource", "updateVenResource", "deleteVenResource", resource},
	} {
		ops = append(ops,
			operation{c.search, "GET", "/" + c.plural, nil},
			operation{c.read, "GET", "/" + c.plural + "/%s", nil},
			operation{c.create, "POST", "/" + c.plural, c.body},
			operation{c.update, "PUT", "/" + c.plural + "/%s", c.body},
			operation{c.remove, "DELETE", "/" + c.plural + "/%s", nil},
		)
	}
	return ops
}

// send performs an operation as c.
func send(t *suite.T, c *probe.Client, op operation, ids map[string]string) *probe.Exchange {
	path := strings.Replace(op.path, "%s", ids[strings.Split(op.path, "/")[1]], 1)
	var body any
	if op.body != nil {
		body = op.body(ids)
	}
	return t.Call(c, op.method, path, body)
}

// oneOfEach creates a program, an event, and as the VEN a report, a ven and
// a resource, and returns their IDs by collection.
func oneOfEach(t *suite.T) map[string]string {
	ids := map[string]string{}
	ids["programs"] = str(t, createProgram(t, "authz", nil), "id")
	ids["events"] = str(t, createEvent(t, ids["programs"], nil), "id")
	ids["reports"] = str(t, create(t, t.VEN, "reports", "createReport", report(ids["events"], t.Name("authz"))), "id")
	// Not through create: what a ven object looks like is VEN-001's
	// business, and must not decide the outcome of these scenarios.
	ven := t.Call(t.VEN, "POST", "/vens", object{"objectType": "VEN_VEN_REQUEST", "venName": t.Name("authz-ven")})
	if ven.Status != 201 {
		t.Fatalf("the VEN could not register a ven object: %s", ven)
	}
	ids["vens"] = str(t, t.Object(ven.JSON()), "id")
	t.Delete(t.VEN, "/vens/"+ids["vens"])
	ids["resources"] = str(t, create(t, t.VEN, "resources", "createResource",
		object{"objectType": "VEN_RESOURCE_REQUEST", "resourceName": "authz-resource", "venID": ids["vens"]}), "id")
	return ids
}

func authorization() []suite.Scenario {
	return []suite.Scenario{
		{
			ID:    "AUTHZ-001",
			Title: "Every operation on programs, events, reports, vens and resources needs a token (401)",
			Refs:  []string{"openadr3.yaml#searchAllPrograms", "openadr3.yaml#createReport", "openadr3.yaml#deleteVenResource", "Definition: OAuth 2.0 client credential flow"},
			Run: func(t *suite.T) {
				ids := oneOfEach(t)
				for _, op := range coreOperations() {
					ex := send(t, t.Anonymous, op, ids)
					if ex.Status != 401 {
						t.Errorf("%s %s without a token (%s): status %d, want 401; body: %s", op.method, ex.URL, op.id, ex.Status, ex.ResponseBody)
					}
				}
				// And nothing was changed or removed by any of them.
				for collection, id := range ids {
					if ex := t.Call(t.BL, "GET", "/"+collection+"/"+id, nil); ex.Status != 200 {
						t.Errorf("after the anonymous requests, GET /%s/%s as Business Logic: %d", collection, id, ex.Status)
					}
				}
			},
		},
		{
			ID:    "AUTHZ-002",
			Title: "A VEN reads, changes and deletes only its own vens and resources",
			Refs: []string{"openadr3.yaml#searchVenByID", "openadr3.yaml#updateVen", "openadr3.yaml#deleteVen", "openadr3.yaml#searchVenResourceByID",
				"openadr3.yaml#updateVenResource", "openadr3.yaml#deleteVenResource", "openadr3.yaml#createResource", "Definition: VEN created object privacy"},
			Run: func(t *suite.T) {
				if t.VEN2 == nil {
					t.Skipf("the run has no second VEN")
				}
				ids := oneOfEach(t)
				// The Definition asks for an empty set in searches and leaves
				// the status of a request by ID open: 403 and 404 both keep
				// the object private.
				for _, op := range coreOperations() {
					collection := strings.Split(op.path, "/")[1]
					if collection != "vens" && collection != "resources" || !strings.Contains(op.path, "%s") {
						continue
					}
					if ex := send(t, t.VEN2, op, ids); ex.Status != 403 && ex.Status != 404 {
						t.Errorf("%s %s by another VEN (%s): status %d, want 403 or 404; body: %s", op.method, ex.URL, op.id, ex.Status, ex.ResponseBody)
					}
				}
				// Another VEN cannot hang a resource on this ven either. A VTN
				// may refuse the request, or create the resource on the
				// requester's own ven, whatever venID the request names: what
				// counts is where the resource ends up.
				ex := t.Call(t.VEN2, "POST", "/resources", object{"objectType": "VEN_RESOURCE_REQUEST", "resourceName": "intruder", "venID": ids["vens"]})
				if ex.Status == 201 {
					created := t.Object(ex.JSON())
					t.Delete(t.BL, "/resources/"+str(t, created, "id"))
					if created["venID"] == ids["vens"] {
						t.Errorf("another VEN created a resource on this VEN's ven object: %v", created)
					}
				}
				onVEN, _ := t.Call(t.BL, "GET", "/resources?venID="+ids["vens"], nil).JSON().([]any)
				if len(onVEN) != 1 {
					t.Errorf("the ven has %d resources after another VEN tried to add one, want the one its owner created", len(onVEN))
				}
				// What the owner has is as it was.
				ven, _ := t.Call(t.VEN, "GET", "/vens/"+ids["vens"], nil).JSON().(object)
				if ven["venName"] != t.Name("authz-ven") {
					t.Errorf("the ven was changed or removed by another VEN: %v", ven)
				}
				res := t.Object(t.Expect(t.Call(t.VEN, "GET", "/resources/"+ids["resources"], nil), 200, "searchVenResourceByID"))
				if res["resourceName"] != "authz-resource" {
					t.Errorf("the resource was changed by another VEN: %v", res)
				}
			},
		},
		{
			ID:    "AUTHZ-003",
			Title: "A VEN cannot grant itself targets",
			Refs:  []string{"openadr3.yaml#createVen", "openadr3.yaml#updateVen", "openadr3.yaml#searchAllPrograms", "Definition: program and event objects - targeting"},
			Run: func(t *suite.T) {
				// "Only BL clients may write targets to ven and resource objects."
				target := t.Name("authz-003")
				programID := str(t, createProgram(t, "authz-003", object{"targets": []string{target}}), "id")
				sees := func() bool {
					list := t.List(t.Expect(t.Call(t.VEN, "GET", "/programs?targets="+target, nil), 200, "searchAllPrograms"))
					return contains(ids(t, list), programID)
				}
				// Whatever the VTN makes of these requests, the VEN must not
				// end up seeing the program.
				var venID string
				for _, body := range []object{
					{"objectType": "VEN_VEN_REQUEST", "venName": t.Name("authz-003-ven"), "targets": []string{target}},
					{"objectType": "BL_VEN_REQUEST", "venName": t.Name("authz-003-ven"), "clientID": t.VENClientID, "targets": []string{target}},
				} {
					ex := t.Call(t.VEN, "POST", "/vens", body)
					if ex.Status == 201 && venID == "" {
						venID = str(t, t.Object(ex.JSON()), "id")
						t.Delete(t.BL, "/vens/"+venID)
					}
					if sees() {
						t.Fatalf("after POST /vens as a VEN with %v and targets, the VEN sees a program targeted at them", body["objectType"])
					}
				}
				if venID == "" {
					t.Skipf("the VEN could not register a ven object to try an update on")
				}
				t.Call(t.VEN, "PUT", "/vens/"+venID, object{"objectType": "VEN_VEN_REQUEST", "venName": t.Name("authz-003-ven"), "targets": []string{target}})
				if sees() {
					t.Errorf("after PUT /vens as a VEN with targets, the VEN sees a program targeted at them")
				}
				// The control: when Business Logic grants the target, it does.
				if t.VENClientID == "" {
					return
				}
				grant := t.Call(t.BL, "PUT", "/vens/"+venID, object{"objectType": "BL_VEN_REQUEST", "venName": t.Name("authz-003-ven"), "clientID": t.VENClientID, "targets": []string{target}})
				if grant.Status == 200 && !sees() {
					t.Errorf("after Business Logic granted the target, the VEN still does not see the program when naming it")
				}
			},
		},
	}
}

// --- programs

func programs() []suite.Scenario {
	return []suite.Scenario{
		{
			ID:    "PROGRAM-001",
			Title: "Creating a program returns 201 and the VTN's own metadata",
			Refs:  []string{"openadr3.yaml#createProgram", "Definition: Object Metadata", "Definition: POST and PUT"},
			Run: func(t *suite.T) {
				before := time.Now().Add(-5 * time.Minute)
				// Metadata in a request is to be ignored.
				p := createProgram(t, "program-001", object{
					"id": "chosen-by-the-client", "objectType": "EVENT", "createdDateTime": "2001-01-01T00:00:00Z",
					"payloadDescriptors": []object{{"objectType": "EVENT_PAYLOAD_DESCRIPTOR", "payloadType": "PRICE", "units": "KWH", "currency": "EUR"}},
				})
				if p["id"] == "chosen-by-the-client" {
					t.Errorf("the VTN kept the id of the request; it SHALL ignore it")
				}
				if p["objectType"] != "PROGRAM" {
					t.Errorf("objectType = %v, want PROGRAM", p["objectType"])
				}
				if p["programName"] != t.Name("program-001") {
					t.Errorf("programName = %v", p["programName"])
				}
				if created := timestamp(t, p, "createdDateTime"); created.Before(before) {
					t.Errorf("createdDateTime = %v; the VTN SHALL set it on creation", created)
				}
				descriptors, _ := p["payloadDescriptors"].([]any)
				if len(descriptors) != 1 {
					t.Errorf("payloadDescriptors = %v, want the one that was sent", p["payloadDescriptors"])
				}
			},
		},
		{
			ID:    "PROGRAM-002",
			Title: "A program can be read by ID and found in a search; an unknown ID is 404",
			Refs:  []string{"openadr3.yaml#searchProgramByProgramId", "openadr3.yaml#searchAllPrograms", "Definition: Response Codes and Errors"},
			Run: func(t *suite.T) {
				p := createProgram(t, "program-002", nil)
				id := str(t, p, "id")
				for _, c := range []*probe.Client{t.BL, t.VEN} {
					got := t.Object(t.Expect(t.Call(c, "GET", "/programs/"+id, nil), 200, "searchProgramByProgramId"))
					if got["id"] != id || got["programName"] != p["programName"] {
						t.Errorf("%s read %v, want the program that was created", c.Name, got)
					}
				}
				list := t.List(t.Expect(t.Call(t.BL, "GET", "/programs", nil), 200, "searchAllPrograms"))
				// A VTN with more programs than one page holds may list ours later.
				if len(list) < 50 && !contains(ids(t, list), id) {
					t.Errorf("the search does not list program %s", id)
				}
				t.ExpectProblem(t.Call(t.BL, "GET", "/programs/"+t.Name("no-such-program"), nil), 404, "searchProgramByProgramId")
			},
		},
		{
			ID:    "PROGRAM-003",
			Title: "Updating a program replaces it and keeps its identity",
			Refs:  []string{"openadr3.yaml#updateProgram", "Definition: Object Metadata", "Definition: POST and PUT"},
			Run: func(t *suite.T) {
				p := createProgram(t, "program-003", object{"targets": []string{"group-1"}})
				id := str(t, p, "id")
				body := object{"id": "other", "programName": t.Name("program-003-renamed")}
				updated := t.Object(t.Expect(t.Call(t.BL, "PUT", "/programs/"+id, body), 200, "updateProgram"))
				if updated["id"] != id {
					t.Errorf("id changed from %s to %v", id, updated["id"])
				}
				if updated["programName"] != body["programName"] {
					t.Errorf("programName = %v after the update", updated["programName"])
				}
				if !timestamp(t, updated, "createdDateTime").Equal(timestamp(t, p, "createdDateTime")) {
					t.Errorf("createdDateTime changed on update")
				}
				if timestamp(t, updated, "modificationDateTime").Before(timestamp(t, p, "modificationDateTime")) {
					t.Errorf("modificationDateTime went backwards on update")
				}
				got := t.Object(t.Expect(t.Call(t.BL, "GET", "/programs/"+id, nil), 200, "searchProgramByProgramId"))
				if got["programName"] != body["programName"] {
					t.Errorf("a read after the update returns programName %v", got["programName"])
				}
				t.ExpectProblem(t.Call(t.BL, "PUT", "/programs/"+t.Name("no-such-program"), body), 404, "updateProgram")
			},
		},
		{
			ID:    "PROGRAM-004",
			Title: "A second program with the same programName is a 409 conflict",
			Refs:  []string{"openadr3.yaml#createProgram", "Definition: Object names"},
			Run: func(t *suite.T) {
				createProgram(t, "program-004", nil)
				ex := t.Call(t.BL, "POST", "/programs", object{"programName": t.Name("program-004")})
				if ex.Status == 201 {
					t.Delete(t.BL, "/programs/"+str(t, t.Object(ex.JSON()), "id"))
				}
				t.ExpectProblem(ex, 409, "createProgram")
			},
		},
		{
			ID:    "PROGRAM-005",
			Title: "A program without its required programName is rejected with 400",
			Refs:  []string{"openadr3.yaml#createProgram", "Definition: Required and optional properties", "Definition: Message validation"},
			Run: func(t *suite.T) {
				for _, tc := range []struct {
					name string
					body any
				}{
					{"no programName", object{"targets": []string{"a"}}},
					{"not JSON", "programName=x"},
					{"wrong JSON type", object{"programName": 7}},
				} {
					ex := t.Call(t.BL, "POST", "/programs", tc.body)
					if ex.Status == 201 {
						t.Delete(t.BL, "/programs/"+str(t, t.Object(ex.JSON()), "id"))
						t.Errorf("%s: the VTN created a program", tc.name)
						continue
					}
					t.ExpectProblem(ex, 400, "createProgram")
				}
			},
		},
		{
			ID:    "PROGRAM-006",
			Title: "Deleting a program returns it, after which it is gone",
			Refs:  []string{"openadr3.yaml#deleteProgram", "Definition: Response Codes and Errors"},
			Run: func(t *suite.T) {
				p := createProgram(t, "program-006", nil)
				id := str(t, p, "id")
				deleted := t.Object(t.Expect(t.Call(t.BL, "DELETE", "/programs/"+id, nil), 200, "deleteProgram"))
				if deleted["id"] != id {
					t.Errorf("DELETE returned %v, want the deleted program", deleted)
				}
				t.ExpectProblem(t.Call(t.BL, "GET", "/programs/"+id, nil), 404, "searchProgramByProgramId")
				t.ExpectProblem(t.Call(t.BL, "DELETE", "/programs/"+id, nil), 404, "deleteProgram")
			},
		},
		{
			ID:    "PROGRAM-007",
			Title: "skip and limit page through a search; limit is at most 50",
			Refs:  []string{"openadr3.yaml#searchAllPrograms", "Definition: Response Filtering"},
			Run: func(t *suite.T) {
				// The targets parameter narrows the search to this scenario's
				// programs, whatever else the VTN holds.
				target := t.Name("program-007")
				var want []string
				for _, n := range []string{"a", "b", "c"} {
					want = append(want, str(t, createProgram(t, "program-007-"+n, object{"targets": []string{target}}), "id"))
				}
				query := "/programs?targets=" + target
				all := ids(t, t.List(t.Expect(t.Call(t.BL, "GET", query, nil), 200, "searchAllPrograms")))
				if len(all) != 3 {
					t.Fatalf("the search by target returned %v, want the three programs %v", all, want)
				}
				first := ids(t, t.List(t.Expect(t.Call(t.BL, "GET", query+"&limit=2", nil), 200, "searchAllPrograms")))
				rest := ids(t, t.List(t.Expect(t.Call(t.BL, "GET", query+"&skip=2&limit=2", nil), 200, "searchAllPrograms")))
				if len(first) != 2 || len(rest) != 1 {
					t.Fatalf("limit=2 returned %d programs and skip=2 %d, want 2 and 1", len(first), len(rest))
				}
				for _, id := range want {
					if !contains(append(first, rest...), id) {
						t.Errorf("program %s is on neither page (%v, %v)", id, first, rest)
					}
				}
				none := t.List(t.Expect(t.Call(t.BL, "GET", query+"&skip=3", nil), 200, "searchAllPrograms"))
				if len(none) != 0 {
					t.Errorf("skip=3 of 3 returned %d programs", len(none))
				}
				t.ExpectProblem(t.Call(t.BL, "GET", query+"&limit=51", nil), 400, "searchAllPrograms")
				t.ExpectProblem(t.Call(t.BL, "GET", query+"&skip=-1", nil), 400, "searchAllPrograms")
			},
		},
	}
}

// --- events

var prices = []object{
	{"id": 0, "payloads": []object{{"type": "PRICE", "values": []any{0.17}}}},
	{"id": 1, "payloads": []object{{"type": "PRICE", "values": []any{0.21}}}},
}

func events() []suite.Scenario {
	return []suite.Scenario{
		{
			ID:    "EVENT-001",
			Title: "An event is created for a program and returned as it was sent",
			Refs:  []string{"openadr3.yaml#createEvent", "openadr3.yaml#searchEventsByID", "Definition: Object Metadata"},
			Run: func(t *suite.T) {
				programID := str(t, createProgram(t, "event-001", nil), "id")
				e := createEvent(t, programID, object{
					"eventName":         "prices",
					"priority":          1,
					"intervalPeriod":    object{"start": "2030-01-01T00:00:00Z", "duration": "PT1H"},
					"intervals":         prices,
					"reportDescriptors": []object{{"payloadType": "USAGE"}},
				})
				if e["objectType"] != "EVENT" || e["programID"] != programID {
					t.Errorf("event = %v", e)
				}
				got := t.Object(t.Expect(t.Call(t.VEN, "GET", "/events/"+str(t, e, "id"), nil), 200, "searchEventsByID"))
				intervals, _ := got["intervals"].([]any)
				if len(intervals) != 2 {
					t.Fatalf("the VEN reads %d intervals, want 2", len(intervals))
				}
				values := t.Object(t.Object(intervals[1])["payloads"].([]any)[0])["values"].([]any)
				if len(values) != 1 || values[0] != 0.21 {
					t.Errorf("interval 1 carries %v, want [0.21]", values)
				}
				period := t.Object(got["intervalPeriod"])
				if d, _ := period["duration"].(string); !sameDuration(d, "PT1H") {
					t.Errorf("intervalPeriod.duration = %v, want a duration of one hour such as PT1H", period["duration"])
				}
			},
		},
		{
			ID:    "EVENT-002",
			Title: "The programID parameter restricts an event search to one program",
			Refs:  []string{"openadr3.yaml#searchAllEvents", "Definition: Response Filtering"},
			Run: func(t *suite.T) {
				one := str(t, createProgram(t, "event-002-one", nil), "id")
				two := str(t, createProgram(t, "event-002-two", nil), "id")
				e1 := str(t, createEvent(t, one, nil), "id")
				e2 := str(t, createEvent(t, one, nil), "id")
				e3 := str(t, createEvent(t, two, nil), "id")
				for _, c := range []*probe.Client{t.BL, t.VEN} {
					got := ids(t, t.List(t.Expect(t.Call(c, "GET", "/events?programID="+one, nil), 200, "searchAllEvents")))
					if len(got) != 2 || !contains(got, e1) || !contains(got, e2) || contains(got, e3) {
						t.Errorf("%s: events of program one = %v, want %s and %s", c.Name, got, e1, e2)
					}
				}
			},
		},
		{
			ID:    "EVENT-003",
			Title: "An event without its required programID is rejected with 400",
			Refs:  []string{"openadr3.yaml#createEvent", "Definition: Required and optional properties"},
			Run: func(t *suite.T) {
				ex := t.Call(t.BL, "POST", "/events", object{"eventName": "orphan"})
				if ex.Status == 201 {
					t.Delete(t.BL, "/events/"+str(t, t.Object(ex.JSON()), "id"))
				}
				t.ExpectProblem(ex, 400, "createEvent")
			},
		},
	}
}

// --- reports

func report(eventID, clientName string) object {
	return object{
		"eventID":    eventID,
		"clientName": clientName,
		"reportName": "usage",
		"resources": []object{{
			"resourceName": "meter-1",
			"intervals":    []object{{"id": 0, "payloads": []object{{"type": "USAGE", "values": []any{3.5}}}}},
		}},
	}
}

func reports() []suite.Scenario {
	return []suite.Scenario{
		{
			ID:    "REPORT-001",
			Title: "A VEN creates a report; the VTN records the client ID of its token",
			Refs:  []string{"openadr3.yaml#createReport", "openadr3.yaml#searchReportsByReportID", "Definition: VEN created object privacy"},
			Run: func(t *suite.T) {
				programID := str(t, createProgram(t, "report-001", nil), "id")
				eventID := str(t, createEvent(t, programID, nil), "id")
				body := report(eventID, t.Name("ven"))
				// A VEN cannot choose whose report this is.
				body["clientID"] = "someone-else"
				r := create(t, t.VEN, "reports", "createReport", body)
				if r["objectType"] != "REPORT" || r["eventID"] != eventID {
					t.Errorf("report = %v", r)
				}
				if r["clientID"] == "someone-else" {
					t.Errorf("the VTN took clientID from the request; it SHALL write the client ID of the token")
				}
				if t.VENClientID != "" && r["clientID"] != t.VENClientID {
					t.Errorf("clientID = %v, want %s, the client ID the VEN authenticated with", r["clientID"], t.VENClientID)
				}
				id := str(t, r, "id")
				t.Expect(t.Call(t.VEN, "GET", "/reports/"+id, nil), 200, "searchReportsByReportID")
				got := t.Object(t.Expect(t.Call(t.BL, "GET", "/reports/"+id, nil), 200, "searchReportsByReportID"))
				if got["reportName"] != "usage" {
					t.Errorf("Business Logic reads reportName %v", got["reportName"])
				}
			},
		},
		{
			ID:    "REPORT-002",
			Title: "Report searches filter by eventID, programID and clientName",
			Refs:  []string{"openadr3.yaml#searchAllReports", "Definition: Response Filtering"},
			Run: func(t *suite.T) {
				programID := str(t, createProgram(t, "report-002", nil), "id")
				other := str(t, createProgram(t, "report-002-other", nil), "id")
				eventID := str(t, createEvent(t, programID, nil), "id")
				otherEvent := str(t, createEvent(t, other, nil), "id")
				mine := str(t, create(t, t.VEN, "reports", "createReport", report(eventID, t.Name("ven"))), "id")
				theirs := str(t, create(t, t.VEN, "reports", "createReport", report(otherEvent, t.Name("ven-b"))), "id")
				for query, want := range map[string]string{
					"eventID=" + eventID:                                    mine,
					"programID=" + programID:                                mine,
					"clientName=" + t.Name("ven-b"):                         theirs,
					"programID=" + other + "&clientName=" + t.Name("ven-b"): theirs,
				} {
					got := ids(t, t.List(t.Expect(t.Call(t.BL, "GET", "/reports?"+query, nil), 200, "searchAllReports")))
					if len(got) != 1 || got[0] != want {
						t.Errorf("reports?%s = %v, want only %s", query, got, want)
					}
				}
				// Filters are additive: both have to match.
				none := t.List(t.Expect(t.Call(t.BL, "GET", "/reports?programID="+programID+"&clientName="+t.Name("ven-b"), nil), 200, "searchAllReports"))
				if len(none) != 0 {
					t.Errorf("a search for program one and the client name of the other report returned %d reports", len(none))
				}
			},
		},
		{
			ID:    "REPORT-003",
			Title: "A VEN sees and changes only its own reports",
			Refs:  []string{"openadr3.yaml#searchAllReports", "openadr3.yaml#updateReport", "openadr3.yaml#deleteReport", "Definition: VEN created object privacy"},
			Run: func(t *suite.T) {
				if t.VEN2 == nil {
					t.Skipf("the run has no second VEN")
				}
				programID := str(t, createProgram(t, "report-003", nil), "id")
				eventID := str(t, createEvent(t, programID, nil), "id")
				id := str(t, create(t, t.VEN, "reports", "createReport", report(eventID, t.Name("ven"))), "id")

				listed := ids(t, t.List(t.Expect(t.Call(t.VEN2, "GET", "/reports?eventID="+eventID, nil), 200, "searchAllReports")))
				if contains(listed, id) {
					t.Errorf("the second VEN's search lists the first VEN's report")
				}
				// The specification asks for an empty set in searches and
				// leaves the status of a read by ID open: 403 and 404 both
				// keep the object private.
				for _, ex := range []*probe.Exchange{
					t.Call(t.VEN2, "GET", "/reports/"+id, nil),
					t.Call(t.VEN2, "PUT", "/reports/"+id, report(eventID, t.Name("ven-2"))),
					t.Call(t.VEN2, "DELETE", "/reports/"+id, nil),
				} {
					if ex.Status != 403 && ex.Status != 404 {
						t.Errorf("%s %s by the second VEN: status %d, want 403 or 404; body: %s", ex.Method, ex.URL, ex.Status, ex.ResponseBody)
					}
				}
				still := t.Object(t.Expect(t.Call(t.VEN, "GET", "/reports/"+id, nil), 200, "searchReportsByReportID"))
				if still["clientName"] != t.Name("ven") {
					t.Errorf("the report was changed by the second VEN: %v", still)
				}
			},
		},
	}
}

// --- vens and resources

func vens() []suite.Scenario {
	return []suite.Scenario{
		{
			ID:    "VEN-001",
			Title: "A VEN registers itself and a resource; both carry its client ID and no other VEN sees them",
			Refs: []string{"openadr3.yaml#createVen", "openadr3.yaml#createResource", "openadr3.yaml#searchVens",
				"openadr3.yaml#searchVenResources", "Definition: VEN created object privacy", "Definition: Object names"},
			Run: func(t *suite.T) {
				ven := create(t, t.VEN, "vens", "createVen", object{"objectType": "VEN_VEN_REQUEST", "venName": t.Name("ven-001")})
				venID := str(t, ven, "id")
				if ven["objectType"] != "VEN" {
					t.Errorf("ven.objectType = %v, want VEN (see deviation SPEC-310-001)", ven["objectType"])
				}
				if t.VENClientID != "" && ven["clientID"] != t.VENClientID {
					t.Errorf("ven.clientID = %v, want %s, the client ID of the token", ven["clientID"], t.VENClientID)
				}
				res := create(t, t.VEN, "resources", "createResource", object{
					"objectType": "VEN_RESOURCE_REQUEST", "resourceName": "battery", "venID": venID,
				})
				if res["objectType"] != "RESOURCE" || res["venID"] != venID {
					t.Errorf("resource = %v", res)
				}

				own := ids(t, t.List(t.Expect(t.Call(t.VEN, "GET", "/vens?venName="+t.Name("ven-001"), nil), 200, "searchVens")))
				if len(own) != 1 || own[0] != venID {
					t.Errorf("the VEN's search by venName returns %v, want %s", own, venID)
				}
				all := ids(t, t.List(t.Expect(t.Call(t.BL, "GET", "/resources?venID="+venID, nil), 200, "searchVenResources")))
				if len(all) != 1 || all[0] != res["id"] {
					t.Errorf("Business Logic finds resources %v for the ven, want %v", all, res["id"])
				}

				// venName is unique within a VTN, resourceName within a ven.
				dup := t.Call(t.BL, "POST", "/vens", object{"objectType": "BL_VEN_REQUEST", "venName": t.Name("ven-001"), "clientID": "another-client"})
				if dup.Status == 201 {
					t.Delete(t.BL, "/vens/"+str(t, t.Object(dup.JSON()), "id"))
				}
				t.ExpectProblem(dup, 409, "createVen")
				dup = t.Call(t.VEN, "POST", "/resources", object{"objectType": "VEN_RESOURCE_REQUEST", "resourceName": "battery", "venID": venID})
				if dup.Status == 201 {
					t.Delete(t.VEN, "/resources/"+str(t, t.Object(dup.JSON()), "id"))
				}
				t.ExpectProblem(dup, 409, "createResource")

				if t.VEN2 == nil {
					return
				}
				theirs := ids(t, t.List(t.Expect(t.Call(t.VEN2, "GET", "/vens", nil), 200, "searchVens")))
				if contains(theirs, venID) {
					t.Errorf("a second VEN's search lists the first VEN's ven object")
				}
				theirs = ids(t, t.List(t.Expect(t.Call(t.VEN2, "GET", "/resources?venID="+venID, nil), 200, "searchVenResources")))
				if len(theirs) != 0 {
					t.Errorf("a second VEN's search lists the first VEN's resources: %v", theirs)
				}
			},
		},
	}
}

// --- targeting

func targeting() []suite.Scenario {
	return []suite.Scenario{
		{
			ID:    "TARGET-001",
			Title: "A targeted program is visible only to a VEN that was granted the target and asks for it",
			Refs: []string{"openadr3.yaml#searchAllPrograms", "openadr3.yaml#createVen",
				"Definition: program and event objects - targeting"},
			Run: func(t *suite.T) {
				if t.VENClientID == "" {
					t.Skipf("the run was not told the VEN's client ID, which a BL_VEN_REQUEST needs")
				}
				granted, withheld := t.Name("granted"), t.Name("withheld")
				programID := str(t, createProgram(t, "target-001", object{"targets": []string{granted, withheld}}), "id")
				search := func(c *probe.Client, query string) []object {
					return t.List(t.Expect(t.Call(c, "GET", "/programs"+query, nil), 200, "searchAllPrograms"))
				}

				// Before any grant, the VEN cannot see it, with or without naming the target.
				if contains(ids(t, search(t.VEN, "")), programID) {
					t.Errorf("a VEN that names no targets sees the targeted program")
				}
				if contains(ids(t, search(t.VEN, "?targets="+granted)), programID) {
					t.Errorf("a VEN that was granted nothing sees the program by naming its target")
				}

				create(t, t.BL, "vens", "createVen", object{
					"objectType": "BL_VEN_REQUEST", "venName": t.Name("target-001-ven"),
					"clientID": t.VENClientID, "targets": []string{granted},
				})

				var seen object
				for _, p := range search(t.VEN, "?targets="+granted+"&targets="+withheld) {
					if p["id"] == programID {
						seen = p
					}
				}
				if seen == nil {
					t.Fatalf("after the grant, the VEN does not see the program when naming its target")
				}
				// Target hiding: only targets that were requested and granted are shown.
				shown, _ := seen["targets"].([]any)
				for _, target := range shown {
					if target == withheld {
						t.Errorf("the VEN is shown the target %q, which it was not granted", withheld)
					}
				}
				// Business Logic is not restricted.
				if !contains(ids(t, search(t.BL, "?targets="+withheld)), programID) {
					t.Errorf("Business Logic does not find the program by one of its targets")
				}
			},
		},
		{
			ID:    "TARGET-002",
			Title: "A VEN that was granted a target still has to name it to see a program that carries it",
			Refs:  []string{"openadr3.yaml#searchAllPrograms", "Definition: program and event objects - targeting"},
			Run: func(t *suite.T) {
				if t.VENClientID == "" {
					t.Skipf("the run was not told the VEN's client ID, which a BL_VEN_REQUEST needs")
				}
				// "If ... the intersection of request targets and ven targets
				// is empty ... an empty set is returned", and read operations
				// "for an object that has targets must include matching
				// targets in the query".
				target := t.Name("target-002")
				programID := str(t, createProgram(t, "target-002", object{"targets": []string{target}}), "id")
				ven := t.Call(t.BL, "POST", "/vens", object{
					"objectType": "BL_VEN_REQUEST", "venName": t.Name("target-002-ven"),
					"clientID": t.VENClientID, "targets": []string{target},
				})
				if ven.Status != 201 {
					t.Fatalf("Business Logic could not create the ven that grants the target: %s", ven)
				}
				t.Delete(t.BL, "/vens/"+str(t, t.Object(ven.JSON()), "id"))

				unnamed := ids(t, t.List(t.Expect(t.Call(t.VEN, "GET", "/programs", nil), 200, "searchAllPrograms")))
				if contains(unnamed, programID) {
					t.Errorf("the VEN sees the targeted program without naming the target in its search")
				}
				named := ids(t, t.List(t.Expect(t.Call(t.VEN, "GET", "/programs?targets="+target, nil), 200, "searchAllPrograms")))
				if !contains(named, programID) {
					t.Errorf("the VEN does not see the program when it names the granted target")
				}
			},
		},
	}
}

// --- the basic workflow

func workflow() []suite.Scenario {
	return []suite.Scenario{
		{
			ID:    "WORKFLOW-001",
			Title: "Program, event, report: Business Logic publishes, a VEN discovers and reports, Business Logic reads",
			Refs: []string{"openadr3.yaml#createProgram", "openadr3.yaml#createEvent", "openadr3.yaml#searchAllPrograms",
				"openadr3.yaml#searchAllEvents", "openadr3.yaml#createReport", "openadr3.yaml#searchAllReports", "User Guide: 6.1 High Level Workflow"},
			Run: func(t *suite.T) {
				// 1 and 2: Business Logic creates a program and an event.
				marker := t.Name("workflow-001")
				program := createProgram(t, "workflow-001", nil)
				programID := str(t, program, "id")
				event := createEvent(t, programID, object{
					"eventName":         marker,
					"intervalPeriod":    object{"start": "2030-01-01T00:00:00Z", "duration": "PT1H"},
					"intervals":         prices,
					"reportDescriptors": []object{{"payloadType": "USAGE"}},
				})

				// 3: the VEN discovers the program.
				var discovered bool
				for skip := 0; !discovered; skip += 50 {
					page := t.List(t.Expect(t.Call(t.VEN, "GET", "/programs?limit=50&skip="+strconv.Itoa(skip), nil), 200, "searchAllPrograms"))
					discovered = contains(ids(t, page), programID)
					if len(page) < 50 {
						break
					}
				}
				if !discovered {
					t.Fatalf("the VEN does not find program %s in a search of all programs", programID)
				}

				// 4: the VEN retrieves the program's events.
				found := t.List(t.Expect(t.Call(t.VEN, "GET", "/events?programID="+programID, nil), 200, "searchAllEvents"))
				if len(found) != 1 || found[0]["id"] != event["id"] || found[0]["eventName"] != marker {
					t.Fatalf("the VEN finds %v, want the one event %v", ids(t, found), event["id"])
				}
				eventID := str(t, found[0], "id")

				// 5: the VEN submits a report.
				sent := create(t, t.VEN, "reports", "createReport", report(eventID, marker))

				// 6: Business Logic retrieves it.
				got := t.List(t.Expect(t.Call(t.BL, "GET", "/reports?programID="+programID, nil), 200, "searchAllReports"))
				if len(got) != 1 || got[0]["id"] != sent["id"] {
					t.Fatalf("Business Logic finds reports %v for the program, want %v", ids(t, got), sent["id"])
				}
				resources, _ := got[0]["resources"].([]any)
				if len(resources) != 1 {
					t.Fatalf("the report has %d resources, want 1", len(resources))
				}
				interval := t.Object(t.Object(resources[0])["intervals"].([]any)[0])
				values := t.Object(interval["payloads"].([]any)[0])["values"].([]any)
				if len(values) != 1 || values[0] != 3.5 {
					t.Errorf("the reported usage reads %v, want [3.5]", values)
				}
			},
		},
	}
}
