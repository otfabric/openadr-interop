package scenarios

import (
	"regexp"
	"strings"
	"testing"

	"github.com/otfabric/openadr-interop/conformance/spec"
)

// Every scenario is identifiable and traceable to the specification.
func TestCatalogue(t *testing.T) {
	doc, err := spec.Load("3.1.0")
	if err != nil {
		t.Fatal(err)
	}
	id := regexp.MustCompile(`^[A-Z]+-\d{3}$`)
	seen := map[string]bool{}
	for _, sc := range All() {
		if !id.MatchString(sc.ID) || seen[sc.ID] {
			t.Errorf("scenario ID %q is malformed or used twice", sc.ID)
		}
		seen[sc.ID] = true
		if sc.Title == "" || sc.Run == nil {
			t.Errorf("%s has no title or no body", sc.ID)
		}
		if len(sc.Refs) == 0 {
			t.Errorf("%s names no part of the specification", sc.ID)
		}
		for _, ref := range sc.Refs {
			if op, ok := strings.CutPrefix(ref, "openadr3.yaml#"); ok {
				if _, found := doc.Operation(op); !found {
					t.Errorf("%s refers to %s, which is not an operation of OpenADR %s", sc.ID, ref, doc.Version)
				}
			} else if !strings.HasPrefix(ref, "Definition: ") && !strings.HasPrefix(ref, "User Guide: ") {
				t.Errorf("%s: reference %q names neither an operation nor a section of the Definition or User Guide", sc.ID, ref)
			}
		}
	}
}

func TestSameDuration(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"PT1H", "PT1H", true},
		{"PT1H", "P0Y0M0DT1H0M0S", true},
		{"P1W", "P7D", true},
		{"PT0S", "-P0D", true},
		{"PT1.50S", "PT1.5S", true},
		{"PT1H", "PT60M", false},
		{"PT1H", "-PT1H", false},
		{"PT1H", "PT2H", false},
		{"PT1H", "1h", false},
		{"", "", false},
	} {
		if got := sameDuration(tc.a, tc.b); got != tc.want {
			t.Errorf("sameDuration(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
