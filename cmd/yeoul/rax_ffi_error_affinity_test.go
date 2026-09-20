//go:build cgo && (darwin || linux)

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// writeThreadLocalErrorFixtureLib builds a C library whose rax operations store
// their failure message in thread-local storage, mirroring Rax's thread-local
// error slot. rax_last_error returns the calling thread's message, or the
// sentinel "unpinned native error read" when the calling thread never observed
// a failure.
func writeThreadLocalErrorFixtureLib(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "fixture.c")
	src := []byte(`#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

static _Thread_local char thread_error[256];
static _Thread_local int thread_error_set = 0;

static void record_error(const char *prefix, const char *text) {
    snprintf(thread_error, sizeof(thread_error), "%s:%s", prefix, text == NULL ? "" : text);
    thread_error_set = 1;
}

int rax_ingest_docs(const char *store, const unsigned char *jsonl, size_t len, char **out) {
    (void)store; (void)jsonl; (void)len; (void)out;
    record_error("ingest", store);
    return -1;
}

int rax_search(const char *store, const char *mode, const char *text, const char *filter, int top_k, int with_metadata, char **out) {
    (void)store; (void)mode; (void)filter; (void)top_k; (void)with_metadata; (void)out;
    /* Yield so the Go scheduler can move goroutines across OS threads. */
    usleep(200);
    record_error("search", text);
    return -1;
}

int rax_search_doc_ids(const char *store, const char *mode, const char *text, const char *filter, int top_k, char **out) {
    (void)store; (void)mode; (void)filter; (void)top_k; (void)out;
    usleep(200);
    record_error("search", text);
    return -1;
}

void rax_string_free(char *value) { free(value); }

const char *rax_last_error(void) {
    if (!thread_error_set) {
        return "unpinned native error read";
    }
    return thread_error;
}

int rax_open_read_only(const char *store, void **out_handle) {
    (void)store; (void)out_handle;
    return 0;
}

int rax_handle_search_doc_ids(void *handle, const char *mode, const char *text, const char *filter, int top_k, char **out) {
    (void)handle; (void)mode; (void)filter; (void)top_k; (void)out;
    usleep(200);
    record_error("search", text);
    return -1;
}

void rax_handle_close(void *handle) { (void)handle; }
`)
	if err := os.WriteFile(srcPath, src, 0o644); err != nil {
		t.Fatalf("write fixture source: %v", err)
	}
	outPath := filepath.Join(dir, "librax_thread_error")
	cmd := exec.Command("cc", "-shared", "-fPIC", "-o", outPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile fixture: %v\n%s", err, out)
	}
	return outPath
}

// TestRaxFFIErrorCopyKeepsThreadAffinity verifies RET-02: the failing operation
// and its error copy happen within one native call, so concurrent failing calls
// with distinct per-thread messages retain their own error under scheduler
// pressure.
func TestRaxFFIErrorCopyKeepsThreadAffinity(t *testing.T) {
	libPath := writeThreadLocalErrorFixtureLib(t)
	storePath := filepath.Join(t.TempDir(), "fixture.rax")

	previousProcs := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previousProcs)

	const (
		workers    = 64
		iterations = 32
	)
	var wg sync.WaitGroup
	errs := make(chan string, workers*iterations)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				query := "worker-" + strconv.Itoa(worker) + "-call-" + strconv.Itoa(i)
				_, err := raxFFISearchText(libPath, storePath, query, 10)
				if err == nil {
					errs <- query + " => <nil error>"
					continue
				}
				if want := "search:" + query; err.Error() != want {
					errs <- query + " => " + err.Error()
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errs)

	failures := make([]string, 0, 4)
	for failure := range errs {
		failures = append(failures, failure)
		if len(failures) >= 4 {
			break
		}
	}
	if len(failures) > 0 {
		t.Fatalf("native error copy lost thread affinity, e.g. %s", strings.Join(failures, "; "))
	}
}
