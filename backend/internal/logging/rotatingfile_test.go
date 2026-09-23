package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRotatingFileWritesToDatedFile(t *testing.T) {
	dir := t.TempDir()
	rf := NewRotatingFile(dir, 0)
	defer rf.Close()

	if _, err := rf.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	want := filepath.Join(dir, "vndl-"+today+".log")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("expected %s to exist and contain the write: %v", want, err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("file content = %q, want %q", data, "hello\n")
	}
}

func TestRotatingFileAppendsAcrossWrites(t *testing.T) {
	dir := t.TempDir()
	rf := NewRotatingFile(dir, 0)
	defer rf.Close()

	rf.Write([]byte("a\n"))
	rf.Write([]byte("b\n"))

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, "vndl-"+today+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a\nb\n" {
		t.Fatalf("content = %q, want %q", data, "a\nb\n")
	}
}

func TestRotatingFilePrunesOldFiles(t *testing.T) {
	dir := t.TempDir()

	old := filepath.Join(dir, "vndl-2000-01-01.log")
	recent := filepath.Join(dir, "vndl-"+time.Now().UTC().Format("2006-01-02")+".log")
	if err := os.WriteFile(old, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recent, []byte("recent"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A file that doesn't match the expected naming pattern must survive
	// pruning untouched — this cleanup only ever touches its own files.
	other := filepath.Join(dir, "not-a-log-file.txt")
	if err := os.WriteFile(other, []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}

	rf := &RotatingFile{dir: dir, retentionDays: 30}
	rf.pruneOld()

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("expected the old dated log file to be pruned")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Error("expected the recent dated log file to survive pruning")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("expected the unrelated file to survive pruning untouched")
	}
}
