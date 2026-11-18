package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YusufKer/worklog/internal/store"
)

func TestAdd(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "wl")
	var out strings.Builder
	err := add([]string{"-dir", dir, "-config", noConfig(t), "-date", "tomorrow", "-notes", "-", "Call", "the", "bank"},
		strings.NewReader("Ask about fees\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimSpace(out.String())
	td, err := store.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	tomorrow := time.Now().AddDate(0, 0, 1).Format(store.DateLayout)
	if td.Title != "Call the bank" || td.Date != tomorrow || td.Notes != "Ask about fees\n" {
		t.Errorf("got title %q date %q notes %q", td.Title, td.Date, td.Notes)
	}
	if filepath.Base(path) != tomorrow+"-call-the-bank.md" {
		t.Errorf("file name %s", filepath.Base(path))
	}

	// Stdin is only read with "-notes -"; no title is an error; -h is not.
	out.Reset()
	if err := add([]string{"-dir", dir, "-config", noConfig(t), "-notes", "flag", "Second"}, blockingReader{}, &out); err != nil {
		t.Fatal(err)
	}
	if td, _ := store.Load(strings.TrimSpace(out.String())); td.Notes != "flag" {
		t.Errorf("notes = %q, want the -notes flag", td.Notes)
	}
	if err := add([]string{"-dir", dir, "-config", noConfig(t)}, nil, &out); err == nil {
		t.Error("no title should be an error")
	}
	if err := add([]string{"-h"}, nil, &out); err != nil {
		t.Errorf("-h returned %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("%d files in data dir, want 2", len(entries))
	}
}

// blockingReader fails the test if anything reads from it.
type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) { panic("stdin was read without -notes -") }
