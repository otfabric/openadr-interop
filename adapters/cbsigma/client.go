// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/cB-Sigma/openadr/oadr3"
)

// The kinds of object of the contract the library knows, in the order of
// their operations in the capability document. It has no resource groups.
var objectKinds = []string{"program", "event", "report", "ven", "resource", "subscription"}

var verbs = []string{"list", "get", "create", "update", "delete"}

// The object type of the library, and the path of its collection.
var kindTypes = map[string]struct{ typ, path string }{
	"program":      {oadr3.TypeProgram, "/programs"},
	"event":        {oadr3.TypeEvent, "/events"},
	"report":       {oadr3.TypeReport, "/reports"},
	"ven":          {oadr3.TypeVEN, "/vens"},
	"resource":     {oadr3.TypeResource, "/resources"},
	"subscription": {oadr3.TypeSubscription, "/subscriptions"},
}

// offers reports whether a role has an operation. The client of the
// library is one for both roles; the operations follow the scopes of
// OpenADR 3.1.0 (vtn.RoleScopes): Business Logic does not write reports, a
// VEN does not write programs or events.
func offers(role, kind, verb string) bool {
	if verb == "list" || verb == "get" {
		return true
	}
	if role == "bl" {
		return kind != "report"
	}
	return kind != "program" && kind != "event"
}

func operationName(verb, kind string) string {
	if verb == "list" {
		return "list-" + kind + "s"
	}
	return verb + "-" + kind
}

func operationsOf(role string) []string {
	var ops []string
	for _, kind := range objectKinds {
		for _, verb := range verbs {
			if offers(role, kind, verb) {
				ops = append(ops, operationName(verb, kind))
			}
		}
	}
	// listen-webhook writes a subscription, which both roles may.
	return append(ops, "get-notifiers", "get-topics", "listen-webhook")
}

// The search filters of each kind of object, besides --skip and --limit,
// and their query parameters.
var listFilters = map[string][]string{
	"program":      {"targets"},
	"event":        {"targets", "program-id"},
	"report":       {"program-id", "event-id", "client-name"},
	"ven":          {"targets", "ven-name"},
	"resource":     {"targets", "ven-id", "resource-name"},
	"subscription": {"program-id", "client-name"},
}

var queryNames = map[string]string{
	"program-id": "programID", "event-id": "eventID", "client-name": "clientName",
	"ven-name": "venName", "ven-id": "venID", "resource-name": "resourceName",
}

// The topic scopes of get-topics: the path below /notifiers/mqtt/topics,
// with {id} for the object a scope is about.
var topicScopes = map[string]struct{ of, path string }{
	"programs":       {"", "/programs"},
	"program":        {"program", "/programs/{id}"},
	"events":         {"", "/events"},
	"program-events": {"program", "/programs/{id}/events"},
	"reports":        {"", "/reports"},
	"subscriptions":  {"", "/subscriptions"},
	"vens":           {"", "/vens"},
	"ven":            {"ven", "/vens/{id}"},
	"resources":      {"", "/resources"},
	"ven-events":     {"ven", "/vens/{id}/events"},
	"ven-programs":   {"ven", "/vens/{id}/programs"},
	"ven-resources":  {"ven", "/vens/{id}/resources"},
}

// objectID is the objectID pattern of OpenADR 3.1.0.
var objectID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

type clientRequest struct {
	operation, verb, kind string
	url, tokenURL         string
	timeout               time.Duration
	who                   fixtureClient
	anonymous             bool
	id                    string
	body                  oadr3.Object
	query                 url.Values
	paged                 bool
	scope                 string
	webhook               *webhookRequest
}

func parseClient(args []string) (*clientRequest, error) {
	if len(args) == 0 {
		return nil, usagef("client needs an operation")
	}
	r := &clientRequest{operation: args[0], query: url.Values{}}
	known := []string{"url", "as", "anonymous", "fixture", "token-url", "timeout-ms"}
	notifier := false
	switch r.operation {
	case "get-notifiers":
		notifier = true
	case "get-topics":
		notifier = true
		known = append(known, "scope", "id")
	case "listen-webhook":
		notifier = true
		known = append(known, webhookFlags...)
	default:
		verb, kind, ok := strings.Cut(r.operation, "-")
		if verb == "list" {
			kind, ok = strings.CutSuffix(kind, "s")
		}
		if !ok || !contains(verbs, verb) || !contains(objectKinds, kind) || operationName(verb, kind) != r.operation {
			return nil, usagef("unknown operation %q", r.operation)
		}
		r.verb, r.kind = verb, kind
		switch verb {
		case "list":
			known = append(append(known, "skip", "limit"), listFilters[kind]...)
		case "get", "delete":
			known = append(known, "id")
		case "create":
			known = append(known, "body")
		case "update":
			known = append(known, "id", "body")
		}
	}
	given, err := parseFlags(args[1:], known...)
	if err != nil {
		return nil, err
	}
	required := []string{"url"}
	if notifier {
		if r.operation == "get-topics" {
			required = append(required, "scope")
		}
		if r.operation == "listen-webhook" {
			required = append(required, "callback-url")
		}
	} else {
		for _, f := range []string{"id", "body"} {
			if contains(known, f) {
				required = append(required, f)
			}
		}
	}
	for _, f := range required {
		if _, ok := given[f]; !ok {
			return nil, usagef("--%s is required", f)
		}
	}
	_, as := given["as"]
	_, anonymous := given["anonymous"]
	if as == anonymous {
		return nil, usagef("one of --as and --anonymous is required")
	}
	r.url = trimSlash(given["url"])
	if !httpURL.MatchString(r.url) {
		return nil, usagef("--url: %q is not an http or https URL", given["url"])
	}
	if v, ok := given["token-url"]; ok {
		if !httpURL.MatchString(v) {
			return nil, usagef("--token-url: %q is not an http or https URL", v)
		}
		r.tokenURL = v
	}
	ms, err := number("timeout-ms", valueOr(given, "timeout-ms", "10000"), 1, 600000)
	if err != nil {
		return nil, err
	}
	r.timeout = time.Duration(ms) * time.Millisecond

	if v, ok := given["scope"]; ok {
		scope, known := topicScopes[v]
		if !known {
			return nil, usagef("--scope: %q is not a topic scope", v)
		}
		_, hasID := given["id"]
		if (scope.of == "") == hasID {
			if scope.of == "" {
				return nil, usagef("--scope %s takes no --id", v)
			}
			return nil, usagef("--scope %s takes the ID of a %s", v, scope.of)
		}
		r.scope = v
	}
	if v, ok := given["id"]; ok {
		if !objectID.MatchString(v) {
			return nil, usagef("--id: %q is not an object ID", v)
		}
		r.id = v
	}
	if v, ok := given["body"]; ok {
		var obj map[string]any
		if err := json.Unmarshal([]byte(v), &obj); err != nil || obj == nil {
			return nil, usagef("--body must be a JSON object")
		}
		r.body = obj
	}
	if r.verb == "list" {
		for _, f := range listFilters[r.kind] {
			v, ok := given[f]
			switch {
			case !ok:
			case f == "targets":
				targets := strings.Split(v, ",")
				if contains(targets, "") {
					return nil, usagef("--targets: an empty target")
				}
				r.query["targets"] = targets
			case strings.HasSuffix(f, "-id") && !objectID.MatchString(v):
				return nil, usagef("--%s: %q is not an object ID", f, v)
			default:
				r.query.Set(queryNames[f], v)
			}
		}
		_, skip := given["skip"]
		_, limit := given["limit"]
		if skip || limit {
			s, err := number("skip", valueOr(given, "skip", "0"), 0, 1<<31-1)
			if err != nil {
				return nil, err
			}
			l, err := number("limit", valueOr(given, "limit", "50"), 0, 1<<31-1)
			if err != nil {
				return nil, err
			}
			r.query.Set("skip", fmt.Sprint(s))
			r.query.Set("limit", fmt.Sprint(l))
			r.paged = true
		}
	}

	if r.operation == "listen-webhook" {
		if anonymous {
			return nil, usagef("listen-webhook needs a client of the fixture: --as")
		}
		if r.webhook, err = parseWebhook(given); err != nil {
			return nil, err
		}
	}

	if anonymous {
		role := given["anonymous"]
		if role != "bl" && role != "ven" {
			return nil, usagef("--anonymous: %q is not bl or ven", role)
		}
		if r.tokenURL != "" {
			return nil, usagef("--anonymous takes no --token-url")
		}
		r.who, r.anonymous = fixtureClient{Name: "anonymous", Role: role}, true
	} else {
		f, err := loadFixture(valueOr(given, "fixture", fixtureRoot+"/baseline/fixture.json"))
		if err == nil {
			err = served(f)
		}
		if err != nil {
			return nil, usagef("--fixture: %v", err)
		}
		who, ok := f.client(given["as"])
		if !ok {
			return nil, usagef("--as: the fixture has no client named %q", given["as"])
		}
		r.who = who
	}
	if !contains(operationsOf(r.who.Role), r.operation) {
		return nil, usagef("%s is not available to a %s client of cB-Sigma openadr", r.operation, r.who.Role)
	}
	return r, nil
}

type resultError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type resultDocument struct {
	SchemaVersion string       `json:"schemaVersion"`
	Adapter       string       `json:"adapter"`
	Operation     string       `json:"operation"`
	Client        string       `json:"client"`
	Role          string       `json:"role"`
	OK            bool         `json:"ok"`
	Error         *resultError `json:"error"`
	Authenticated bool         `json:"authenticated"`
	Status        *int         `json:"status"`
	Response      any          `json:"response"`
	Problem       any          `json:"problem"`
	ElapsedMs     int64        `json:"elapsedMs"`
}

func runClient(args []string) int {
	r, err := parseClient(args)
	if err != nil {
		return usage(err.Error())
	}
	doc, code := execute(r)
	b, _ := json.Marshal(doc)
	fmt.Println(string(b))
	return code
}

// The phases of an operation, which decide its error code.
type phase int

const (
	phaseRequest phase = iota // the library checks the body: nothing sent
	phaseToken                // the token endpoint
	phaseAPI                  // the operation itself
)

type failure struct {
	phase  phase
	status int // the VTN's answer to the operation, 0 for none
	err    error
}

func (f *failure) Error() string { return f.err.Error() }
func (f *failure) Unwrap() error { return f.err }

func execute(r *clientRequest) (resultDocument, int) {
	doc := resultDocument{
		SchemaVersion: schemaVersion, Adapter: adapterName, Operation: r.operation,
		Client: r.who.Name, Role: r.who.Role,
	}
	started := time.Now()
	run := call
	if r.webhook != nil {
		run = listenWebhook
	}
	response, authenticated, status, err := run(r)
	doc.ElapsedMs = time.Since(started).Milliseconds()
	doc.Authenticated = authenticated
	if status != 0 {
		doc.Status = &status
	}
	if err == nil {
		doc.OK, doc.Response = true, response
		return doc, exitOK
	}
	code := classify(err)
	message := err.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	doc.Error = &resultError{Code: code, Message: message}
	var he *oadr3.HTTPError
	if code == "http-error" && errors.As(err, &he) {
		var problem map[string]any
		if json.Unmarshal([]byte(he.Body), &problem) == nil && problem != nil {
			doc.Problem = problem
		}
	}
	if code == "connect-failed" {
		return doc, exitConnect
	}
	return doc, exitFailed
}

// classify maps what the library returned to error.code of the contract.
func classify(err error) string {
	var f *failure
	if !errors.As(err, &f) {
		return "failed"
	}
	if f.phase == phaseRequest {
		return "invalid-request"
	}
	var dns *net.DNSError
	var op *net.OpError
	if errors.As(err, &dns) || errors.As(err, &op) && op.Op == "dial" {
		return "connect-failed"
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	if f.phase == phaseToken {
		return "auth-failed"
	}
	var he *oadr3.HTTPError
	if errors.As(err, &he) {
		return "http-error"
	}
	var syntax *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if f.status/100 == 2 || errors.As(err, &syntax) || errors.As(err, &typeErr) {
		// A success the library could not decode.
		return "invalid-response"
	}
	return "failed"
}

// call performs the operation with the library's client: a token first
// (unless anonymous), then the request. It gives the decoded answer,
// whether a token was issued and the status of the VTN's answer.
func call(r *clientRequest) (any, bool, int, error) {
	var body oadr3.Object
	if r.body != nil {
		typ := kindTypes[r.kind].typ
		in := oadr3.Clone(r.body)
		// The request form of vens and resources is the library's
		// business: the one of the client's role, unless the body names one.
		if _, named := in["objectType"]; !named && (typ == oadr3.TypeVEN || typ == oadr3.TypeResource) {
			prefix := "BL_"
			if r.who.Role == "ven" {
				prefix = "VEN_"
			}
			in["objectType"] = prefix + typ + "_REQUEST"
		}
		checked, err := oadr3.CheckRequest(typ, in)
		if err != nil {
			return nil, false, 0, &failure{phase: phaseRequest, err: err}
		}
		body = checked
	}
	cfg := oadr3.ClientConfig{BaseURL: r.url, TokenURL: r.tokenURL, Timeout: r.timeout}
	if !r.anonymous {
		cfg.ClientID, cfg.Secret = r.who.ClientID, r.who.ClientSecret
	}
	c := oadr3.NewClient(cfg)
	ctx := context.Background()
	authenticated := false
	if !r.anonymous {
		if _, err := c.Token(ctx); err != nil {
			return nil, false, 0, &failure{phase: phaseToken, err: err}
		}
		authenticated = true
	}

	var method, path string
	switch {
	case r.operation == "get-notifiers":
		method, path = http.MethodGet, "/notifiers"
	case r.operation == "get-topics":
		method, path = http.MethodGet, "/notifiers/mqtt/topics"+strings.ReplaceAll(topicScopes[r.scope].path, "{id}", r.id)
	case r.verb == "list" && !r.paged:
		// Every page, as an application reads a collection.
		list, err := c.All(ctx, kindTypes[r.kind].path, r.query)
		if err != nil {
			return nil, authenticated, statusOf(err), &failure{phase: phaseAPI, status: statusOf(err), err: err}
		}
		if list == nil {
			list = []oadr3.Object{}
		}
		return list, authenticated, http.StatusOK, nil
	case r.verb == "list":
		method, path = http.MethodGet, kindTypes[r.kind].path
	case r.verb == "get":
		method, path = http.MethodGet, kindTypes[r.kind].path+"/"+r.id
	case r.verb == "create":
		method, path = http.MethodPost, kindTypes[r.kind].path
	case r.verb == "update":
		method, path = http.MethodPut, kindTypes[r.kind].path+"/"+r.id
	case r.verb == "delete":
		method, path = http.MethodDelete, kindTypes[r.kind].path+"/"+r.id
	}
	var query url.Values
	if r.verb == "list" {
		query = r.query
	}
	var reqBody any
	if body != nil {
		reqBody = body
	}
	var out any
	if r.verb == "list" {
		var list []oadr3.Object
		status, err := c.Do(ctx, method, path, query, reqBody, &list)
		if err != nil {
			return nil, authenticated, status, &failure{phase: phaseAPI, status: status, err: err}
		}
		if list == nil {
			list = []oadr3.Object{}
		}
		return list, authenticated, status, nil
	}
	var obj oadr3.Object
	status, err := c.Do(ctx, method, path, query, reqBody, &obj)
	if err != nil {
		return nil, authenticated, status, &failure{phase: phaseAPI, status: status, err: err}
	}
	out = obj
	return out, authenticated, status, nil
}

// statusOf is the status of the VTN's error answer, 0 for none. All gives
// no status of its own.
func statusOf(err error) int {
	var he *oadr3.HTTPError
	if errors.As(err, &he) {
		return he.Status
	}
	return 0
}
