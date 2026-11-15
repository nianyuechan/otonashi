package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDBPathUsesTempDirectory(t *testing.T) {
	dir := t.TempDir()
	got := dbPath(dir, "wal-normal")
	want := filepath.Join(dir, "diag-wal-normal.db")
	if got != want {
		t.Fatalf("dbPath = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, ".db") {
		t.Fatalf("dbPath = %q, missing .db suffix", got)
	}
}
