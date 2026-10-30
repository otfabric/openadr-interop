package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKnownReadsTheListedFindings(t *testing.T) {
	known, err := loadKnown("../../known/openleadr.tsv", "v0.2.8")
	if err != nil {
		t.Fatal(err)
	}
	if len(known) != 3 {
		t.Fatalf("got %d findings, want 3", len(known))
	}
}

func TestLoadKnownRequiresEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known.tsv")
	if err := os.WriteFile(path, []byte("v1\tID-1\ttext\tsummary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKnown(path, "v1"); err == nil {
		t.Fatal("a line without an evidence URL was accepted")
	}
}
