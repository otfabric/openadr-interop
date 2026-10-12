// Package spec holds the pinned OpenAPI document of each OpenADR revision
// under test and validates messages against it.
//
// It is the reference the conformance assertions are made against, and has
// no knowledge of any implementation under test.
package spec

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/getkin/kin-openapi/openapi3"
)

//go:embed 3.1.0/openadr3.yaml
var v310 []byte

// SHA-256 of the pinned documents; see PROVENANCE.md next to each.
const v310SHA256 = "135bc74af2f733338b55afa95df3cb3a1185867e8e41a7d16cdb2dbfaeb3b475"

// Doc is one revision's OpenAPI document.
type Doc struct {
	// Version is the OpenADR revision, such as "3.1.0".
	Version string
	// SHA256 is the checksum of the document the assertions were made against.
	SHA256 string

	doc *openapi3.T
	ops map[string]*Operation
}

// Operation is one operation of the API.
type Operation struct {
	ID     string
	Method string
	// Path is the path template, such as /programs/{programID}.
	Path string
	// Scopes are the OAuth2 scopes the document requires for the operation.
	Scopes []string

	op *openapi3.Operation
}

// Load returns the document of an OpenADR revision.
func Load(version string) (*Doc, error) {
	if version != "3.1.0" {
		return nil, fmt.Errorf("spec: OpenADR %s is not pinned in this repository", version)
	}
	if sum := sha256.Sum256(v310); hex.EncodeToString(sum[:]) != v310SHA256 {
		return nil, fmt.Errorf("spec: 3.1.0/openadr3.yaml does not match its pinned checksum")
	}
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(v310)
	if err != nil {
		return nil, fmt.Errorf("spec: loading the OpenADR %s document: %w", version, err)
	}
	d := &Doc{Version: version, SHA256: v310SHA256, doc: doc, ops: map[string]*Operation{}}
	if err := d.applyDeviations(); err != nil {
		return nil, err
	}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			o := &Operation{ID: op.OperationID, Method: method, Path: path, op: op}
			if op.Security != nil {
				for _, req := range *op.Security {
					o.Scopes = append(o.Scopes, req["oAuth2ClientCredentials"]...)
				}
			}
			d.ops[op.OperationID] = o
		}
	}
	return d, nil
}

// Deviation is a place where the assertions knowingly depart from the
// letter of the OpenAPI document, because the document cannot be met as
// written. Every deviation is listed in spec/DEVIATIONS.md and in each report.
type Deviation struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

// Deviations returns the deviations applied to this document.
func (d *Doc) Deviations() []Deviation {
	return []Deviation{{
		ID: "SPEC-310-002",
		Summary: "createVen defines no 409 response, while the Definition requires venName to be unique within a VTN " +
			"and lists 409 Conflict for POST. A 409 from createVen is accepted and checked against the conflict response " +
			"the document defines for the other create operations.",
	}, {
		ID: "SPEC-310-001",
		Summary: "The ven and resource schemas are allOf objectMetadata, whose objectType is VEN or RESOURCE, " +
			"and BlVenRequest or BlResourceRequest, whose objectType is the request discriminator. No value satisfies both. " +
			"Responses are checked with the request discriminator's enum dropped, so objectType has to be VEN or RESOURCE.",
	}}
}

// applyDeviations rewrites the schemas the deviations concern.
func (d *Doc) applyDeviations() error {
	// SPEC-310-001
	for object, request := range map[string]string{"ven": "BlVenRequest", "resource": "BlResourceRequest"} {
		ref, ok := d.doc.Components.Schemas[object]
		if !ok || ref.Value == nil {
			return fmt.Errorf("spec: schema %q is missing", object)
		}
		patched := false
		for i, part := range ref.Value.AllOf {
			if part.Ref != "#/components/schemas/"+request || part.Value == nil {
				continue
			}
			// A copy: the request schema itself stays as the document has it.
			relaxed := *part.Value
			relaxed.Properties = make(openapi3.Schemas, len(part.Value.Properties))
			for name, prop := range part.Value.Properties {
				relaxed.Properties[name] = prop
			}
			relaxed.Properties["objectType"] = openapi3.NewSchemaRef("", openapi3.NewStringSchema())
			ref.Value.AllOf[i] = openapi3.NewSchemaRef("", &relaxed)
			patched = true
		}
		if !patched {
			return fmt.Errorf("spec: schema %q no longer has the shape deviation SPEC-310-001 was written for", object)
		}
	}
	// SPEC-310-002
	create, conflict := d.doc.Paths.Find("/vens"), d.doc.Components.Responses["conflict"]
	if create == nil || create.Post == nil || conflict == nil {
		return fmt.Errorf("spec: the document no longer has the shape deviation SPEC-310-002 was written for")
	}
	if create.Post.Responses.Status(409) != nil {
		return fmt.Errorf("spec: createVen defines a 409 response; deviation SPEC-310-002 is obsolete")
	}
	create.Post.Responses.Set("409", conflict)
	return nil
}

// Operation returns the operation with the given operationId.
func (d *Doc) Operation(id string) (*Operation, bool) {
	op, ok := d.ops[id]
	return op, ok
}

// Operations returns every operation, ordered by path and method.
func (d *Doc) Operations() []*Operation {
	out := make([]*Operation, 0, len(d.ops))
	for _, op := range d.ops {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// ecma compiles patterns as the ECMA 262 regular expressions OpenAPI
// specifies; the duration pattern uses a lookahead Go's regexp lacks.
func ecma(expr string) (openapi3.RegexMatcher, error) {
	re, err := regexp2.Compile(expr, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	return matcher{re}, nil
}

type matcher struct{ re *regexp2.Regexp }

func (m matcher) MatchString(s string) bool {
	ok, err := m.re.MatchString(s)
	return err == nil && ok
}

func validate(schema *openapi3.Schema, body []byte) error {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return fmt.Errorf("the body is not JSON: %w", err)
	}
	err := schema.VisitJSON(value,
		openapi3.VisitAsResponse(),
		openapi3.MultiErrors(),
		openapi3.EnableFormatValidation(),
		openapi3.SetSchemaRegexCompiler(ecma),
	)
	if err == nil {
		return nil
	}
	return fmt.Errorf("the body does not match the schema: %s", flatten(err))
}

// flatten renders a validation error as one line per fault, without the
// dump of the schema and value kin-openapi appends.
func flatten(err error) string {
	var lines []string
	var walk func(error)
	walk = func(err error) {
		switch e := err.(type) {
		case openapi3.MultiError:
			for _, sub := range e {
				walk(sub)
			}
		case *openapi3.SchemaError:
			where := strings.Join(e.JSONPointer(), "/")
			if where == "" {
				where = "(root)"
			}
			if e.Origin != nil {
				lines = append(lines, where+": "+e.Reason+" ["+flatten(e.Origin)+"]")
			} else {
				lines = append(lines, where+": "+e.Reason)
			}
		default:
			// Other errors of the library append a dump of schema and value.
			msg, _, _ := strings.Cut(err.Error(), "\nSchema:")
			lines = append(lines, msg)
		}
	}
	walk(err)
	return strings.Join(lines, "; ")
}

// ValidateResponse checks the body of a response with the given status
// against what the document defines for the operation. It fails if the
// document defines no such response.
func (d *Doc) ValidateResponse(operationID string, status int, body []byte) error {
	op, ok := d.ops[operationID]
	if !ok {
		return fmt.Errorf("spec: no operation %q in OpenADR %s", operationID, d.Version)
	}
	ref := op.op.Responses.Status(status)
	if ref == nil || ref.Value == nil {
		var defined []string
		for code := range op.op.Responses.Map() {
			defined = append(defined, code)
		}
		sort.Strings(defined)
		return fmt.Errorf("the document defines no %d response for %s (it defines %s)", status, operationID, strings.Join(defined, ", "))
	}
	media := ref.Value.Content.Get("application/json")
	if media == nil || media.Schema == nil {
		// A response without a body schema, such as the callback's 200.
		return nil
	}
	return validate(media.Schema.Value, body)
}

// ValidateSchema checks a JSON value against a schema of the document's
// components, such as "problem".
func (d *Doc) ValidateSchema(name string, body []byte) error {
	ref, ok := d.doc.Components.Schemas[name]
	if !ok {
		return fmt.Errorf("spec: no schema %q in OpenADR %s", name, d.Version)
	}
	return validate(ref.Value, body)
}

// Statuses returns the response codes the document defines for an operation.
func (d *Doc) Statuses(operationID string) []int {
	op, ok := d.ops[operationID]
	if !ok {
		return nil
	}
	var out []int
	for code := range op.op.Responses.Map() {
		if n, err := strconv.Atoi(code); err == nil {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}
