package spec

import (
	"strings"
	"testing"
)

func load(t *testing.T) *Doc {
	t.Helper()
	d, err := Load("3.1.0")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestLoad(t *testing.T) {
	d := load(t)
	if n := len(d.Operations()); n != 45 {
		t.Errorf("%d operations, want the 45 of OpenADR 3.1.0", n)
	}
	op, ok := d.Operation("createReport")
	if !ok || op.Method != "POST" || op.Path != "/reports" || len(op.Scopes) != 1 || op.Scopes[0] != "write_reports" {
		t.Errorf("createReport = %+v", op)
	}
	if _, err := Load("2.0b"); err == nil {
		t.Error("Load of an unpinned revision succeeded")
	}
}

const program = `{"id":"p1","createdDateTime":"2023-06-15T09:30:00Z","modificationDateTime":"2023-06-15T09:30:00Z","objectType":"PROGRAM","programName":"ResTOU"`

func TestValidateResponse(t *testing.T) {
	d := load(t)
	valid := []struct {
		op     string
		status int
		body   string
	}{
		{"createProgram", 201, program + `}`},
		{"createProgram", 201, program + `,"targets":null,"intervalPeriod":{"start":"2023-06-15T09:30:00Z","duration":"P1Y2M3DT4H5M6.5S"}}`},
		{"searchAllPrograms", 200, `[]`},
		{"searchAllPrograms", 200, `[` + program + `}]`},
		{"searchProgramByProgramId", 404, `{"title":"Not Found","status":404}`},
		{"searchAllEvents", 200, `[{"id":"e1","createdDateTime":"2023-06-15T09:30:00Z","modificationDateTime":"2023-06-15T09:30:00Z","objectType":"EVENT","programID":"p1",
			"intervals":[{"id":0,"payloads":[{"type":"PRICE","values":[0.17,"a",true,{"x":1,"y":2}]}]}]}]`},
		{"getAuthServerInfo", 200, `{"tokenURL":"https://vtn.example/auth/token"}`},
	}
	for _, tc := range valid {
		if err := d.ValidateResponse(tc.op, tc.status, []byte(tc.body)); err != nil {
			t.Errorf("%s %d %s: %v", tc.op, tc.status, tc.body, err)
		}
	}

	invalid := []struct {
		op     string
		status int
		body   string
		want   string // part of the error
	}{
		{"createProgram", 201, `{"programName":"ResTOU"}`, "id"},
		{"createProgram", 201, strings.Replace(program, `"ResTOU"`, `""`, 1) + `}`, "programName"},
		{"createProgram", 201, strings.Replace(program, `"p1"`, `"p 1"`, 1) + `}`, "id"},
		{"createProgram", 201, strings.Replace(program, `"PROGRAM"`, `"PROGRAMME"`, 1) + `}`, "objectType"},
		{"createProgram", 201, strings.Replace(program, `09:30:00Z","mod`, `09:30","mod`, 1) + `}`, "createdDateTime"},
		// The lookahead of the duration pattern: "PT" and "P" are not durations.
		{"createProgram", 201, program + `,"intervalPeriod":{"duration":"PT"}}`, "duration"},
		{"createProgram", 201, program + `,"intervalPeriod":{"duration":"1h"}}`, "duration"},
		{"createProgram", 201, `not json`, "not JSON"},
		{"searchAllPrograms", 200, program + `}`, "array"},
		{"searchAllEvents", 200, `[{"id":"e1","createdDateTime":"2023-06-15T09:30:00Z","modificationDateTime":"2023-06-15T09:30:00Z","objectType":"EVENT","programID":"p1","intervals":[{"payloads":[]}]}]`, "id"},
		{"createProgram", 418, `{}`, "defines no 418 response"},
		{"getAuthServerInfo", 200, `{}`, "tokenURL"},
		{"noSuchOperation", 200, `{}`, "no operation"},
	}
	for _, tc := range invalid {
		err := d.ValidateResponse(tc.op, tc.status, []byte(tc.body))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %d %s: error %v, want one about %q", tc.op, tc.status, tc.body, err, tc.want)
		}
	}
}

func TestValidateSchema(t *testing.T) {
	d := load(t)
	if err := d.ValidateSchema("problem", []byte(`{"title":"Forbidden","status":403,"detail":"no"}`)); err != nil {
		t.Error(err)
	}
	if err := d.ValidateSchema("problem", []byte(`{"status":"403"}`)); err == nil {
		t.Error("a problem with a string status passed")
	}
	if err := d.ValidateSchema("nothing", []byte(`{}`)); err == nil {
		t.Error("an unknown schema passed")
	}
}

// SPEC-310-001: the ven and resource schemas cannot be met as written.
func TestDeviationVENObjectType(t *testing.T) {
	d := load(t)
	const meta = `"id":"v1","createdDateTime":"2023-06-15T09:30:00Z","modificationDateTime":"2023-06-15T09:30:00Z"`
	ven := `{` + meta + `,"objectType":"VEN","clientID":"c","venName":"v"}`
	if err := d.ValidateResponse("createVen", 201, []byte(ven)); err != nil {
		t.Errorf("a ven with objectType VEN: %v", err)
	}
	resource := `{` + meta + `,"objectType":"RESOURCE","clientID":"c","venID":"v1","resourceName":"r"}`
	if err := d.ValidateResponse("createResource", 201, []byte(resource)); err != nil {
		t.Errorf("a resource with objectType RESOURCE: %v", err)
	}
	// The rest of both schemas still applies.
	for _, body := range []string{
		strings.Replace(ven, `"VEN"`, `"BL_VEN_REQUEST"`, 1),
		strings.Replace(ven, `,"clientID":"c"`, ``, 1),
		strings.Replace(ven, `,"venName":"v"`, ``, 1),
	} {
		if err := d.ValidateResponse("createVen", 201, []byte(body)); err == nil {
			t.Errorf("%s passed", body)
		}
	}
	// SPEC-310-002
	if err := d.ValidateResponse("createVen", 409, []byte(`{"title":"Conflict","status":409}`)); err != nil {
		t.Errorf("a 409 from createVen: %v", err)
	}
	if len(d.Deviations()) != 2 {
		t.Errorf("deviations = %v", d.Deviations())
	}
}
