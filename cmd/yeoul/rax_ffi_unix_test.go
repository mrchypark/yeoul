//go:build cgo && (darwin || linux)

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixtureLib builds a minimal C shared library that exports the rax FFI
// symbols. rax_open_read_only always returns non-zero so openRaxFFISearcher
// hits the error path.
func writeFixtureLib(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "fixture.c")
	src := []byte(`#include <stdlib.h>
#include <string.h>

static const char *last_error_msg = "fixture open failed";

int rax_ingest_docs(const char *store, const unsigned char *jsonl, size_t len, char **out) {
    (void)store; (void)jsonl; (void)len; (void)out;
    return 0;
}

int rax_search(const char *store, const char *mode, const char *text, const char *filter, int top_k, int with_metadata, char **out) {
    (void)store; (void)mode; (void)text; (void)filter; (void)top_k; (void)with_metadata; (void)out;
    return 0;
}

void rax_string_free(char *value) { free(value); }

const char *rax_last_error(void) { return last_error_msg; }

int rax_open_read_only(const char *store, void **out_handle) {
    (void)store; (void)out_handle;
    return -1;
}

int rax_handle_search_doc_ids(void *handle, const char *mode, const char *text, const char *filter, int top_k, char **out) {
    (void)handle; (void)mode; (void)text; (void)filter; (void)top_k; (void)out;
    return 0;
}

void rax_handle_close(void *handle) { (void)handle; }
`)
	if err := os.WriteFile(srcPath, src, 0o644); err != nil {
		t.Fatalf("write fixture source: %v", err)
	}
	outPath := filepath.Join(dir, "librax_fixture")
	cmd := exec.Command("cc", "-shared", "-fPIC", "-o", outPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile fixture: %v\n%s", err, out)
	}
	return outPath
}

// TestOpenRaxFFISearcherOpenErrorReturnsBeforeClose verifies that when
// rax_open_read_only fails, the error string is copied before the library
// is unloaded (RET-01).
func TestOpenRaxFFISearcherOpenErrorReturnsBeforeClose(t *testing.T) {
	libPath := writeFixtureLib(t)
	storePath := filepath.Join(t.TempDir(), "nonexistent.rax")

	searcher, err := openRaxFFISearcher(libPath, storePath)
	if err == nil {
		searcher.close()
		t.Fatal("expected an error from openRaxFFISearcher with a failing open")
	}
	if !strings.Contains(err.Error(), "fixture open failed") {
		t.Fatalf("expected error to contain fixture message, got: %v", err)
	}
}

// TestOpenRaxFFISearcherOpenErrorSubprocess runs the open-error path in a
// child process so a use-after-free crash kills only the subprocess.
func TestOpenRaxFFISearcherOpenErrorSubprocess(t *testing.T) {
	if os.Getenv("YEOUL_TEST_FFI_OPEN_ERROR") == "1" {
		libPath := os.Getenv("YEOUL_TEST_FFI_LIB")
		storePath := os.Getenv("YEOUL_TEST_FFI_STORE")
		_, err := openRaxFFISearcher(libPath, storePath)
		if err == nil {
			os.Exit(2)
		}
		if !strings.Contains(err.Error(), "fixture open failed") {
			os.Exit(2)
		}
		os.Exit(0)
	}
	libPath := writeFixtureLib(t)
	storePath := filepath.Join(t.TempDir(), "nonexistent.rax")

	cmd := exec.Command(os.Args[0], "-test.run=TestOpenRaxFFISearcherOpenErrorSubprocess", "-test.v")
	cmd.Env = append(os.Environ(),
		"YEOUL_TEST_FFI_OPEN_ERROR=1",
		"YEOUL_TEST_FFI_LIB="+libPath,
		"YEOUL_TEST_FFI_STORE="+storePath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed (possible use-after-free): %v\noutput: %s", err, out)
	}
}
