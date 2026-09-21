package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A failed replacement must never truncate the previous export: the payload is
// staged privately and published with a rename only after a complete write.
func TestWritePrivateFileKeepsPreviousExportOnPartialWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.json")
	previous := []byte(`{"episodes":[{"id":"ep-previous"}]}`)
	if err := os.WriteFile(path, previous, 0o600); err != nil {
		t.Fatalf("write previous export: %v", err)
	}

	original := writePrivateFileContents
	t.Cleanup(func() { writePrivateFileContents = original })
	writePrivateFileContents = func(file *os.File, data []byte) error {
		if _, err := file.Write(data[:len(data)/2]); err != nil {
			return err
		}
		return errors.New("injected partial write failure")
	}

	if err := writePrivateFileStaged(path, []byte(`{"episodes":[{"id":"ep-replacement"}]}`)); err == nil {
		t.Fatal("expected the injected partial write to fail the export")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read export after failure: %v", err)
	}
	if string(got) != string(previous) {
		t.Fatalf("expected the previous export to stay intact, got %q", string(got))
	}
	assertNoExportStagingLeftovers(t, dir, filepath.Base(path))
}

func TestWritePrivateFilePublishesCompleteReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "export.json")
	if err := os.WriteFile(path, []byte("previous"), 0o600); err != nil {
		t.Fatalf("write previous export: %v", err)
	}
	replacement := `{"episodes":[{"id":"ep-replacement"}]}`
	if err := writePrivateFileStaged(path, []byte(replacement)); err != nil {
		t.Fatalf("write replacement export: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replacement export: %v", err)
	}
	if string(got) != replacement {
		t.Fatalf("expected the replacement export, got %q", string(got))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat replacement export: %v", err)
	}
	// Windows has no POSIX permission bits, so Mode().Perm() always reports
	// 0666 there and the 0600 assertion is not expressible. The staging path
	// still runs on Windows to cover the durability behavior, so only the mode
	// assertion is skipped; every other check above and below stays in force.
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("expected replacement export mode 0600, got %o", got)
		}
	}
	assertNoExportStagingLeftovers(t, dir, filepath.Base(path))
}

func assertNoExportStagingLeftovers(t *testing.T, dir, base string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read export directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "."+base+".tmp-") {
			t.Fatalf("expected no export staging leftovers, found %q", entry.Name())
		}
	}
}
