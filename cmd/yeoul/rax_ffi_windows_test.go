//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// raxWindowsFailureScenarios covers the DLL resolution failures that used to
// panic through syscall.LazyProc.Call. Each runs in a child process so that a
// regression panics the child instead of the test binary, letting the parent
// assert on a normal error instead of a crash.
var raxWindowsFailureScenarios = []struct {
	name    string
	libPath func(t *testing.T) string
}{
	{
		name: "missing_dll",
		libPath: func(t *testing.T) string {
			return filepath.Join(t.TempDir(), "librax_missing.dll")
		},
	},
	{
		name: "invalid_bytes",
		libPath: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "librax_corrupt.dll")
			if err := os.WriteFile(path, []byte("this is not a portable executable"), 0o644); err != nil {
				t.Fatalf("write corrupt dll: %v", err)
			}
			return path
		},
	},
	{
		name: "missing_exports",
		libPath: func(t *testing.T) string {
			root := os.Getenv("SystemRoot")
			if root == "" {
				root = `C:\Windows`
			}
			path := filepath.Join(root, "System32", "ntdll.dll")
			if _, err := os.Stat(path); err != nil {
				t.Skipf("no system dll available for the missing-export scenario: %v", err)
			}
			return path
		},
	},
}

func TestRaxWindowsDLLFailuresReturnErrorsNotPanics(t *testing.T) {
	for _, scenario := range raxWindowsFailureScenarios {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			libPath := scenario.libPath(t)
			storePath := filepath.Join(t.TempDir(), "store.rax")

			cmd := exec.Command(os.Args[0], "-test.run=TestRaxWindowsDLLFailureChild", "-test.v")
			cmd.Env = append(os.Environ(),
				"YEOUL_TEST_WIN_RAX_FAILURE=1",
				"YEOUL_TEST_WIN_RAX_LIB="+libPath,
				"YEOUL_TEST_WIN_RAX_STORE="+storePath,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("child exited with %v; expected a returned error, not a panic or crash:\n%s", err, out)
			}
			if strings.Contains(string(out), "panic:") {
				t.Fatalf("expected no panic from DLL resolution, got:\n%s", out)
			}
		})
	}
}

// TestRaxWindowsDLLFailureChild exercises every FFI entry point against an
// unresolvable library. It only runs in the child process spawned by the parent
// test above.
func TestRaxWindowsDLLFailureChild(t *testing.T) {
	if os.Getenv("YEOUL_TEST_WIN_RAX_FAILURE") != "1" {
		t.Skip("child-process helper; driven by TestRaxWindowsDLLFailuresReturnErrorsNotPanics")
	}
	libPath := os.Getenv("YEOUL_TEST_WIN_RAX_LIB")
	storePath := os.Getenv("YEOUL_TEST_WIN_RAX_STORE")
	requireError := func(what string, err error) {
		if err == nil {
			t.Fatalf("%s: expected an error, got nil", what)
		}
		if !strings.Contains(err.Error(), "rax ffi:") {
			t.Fatalf("%s: expected a rax ffi error, got %v", what, err)
		}
	}

	_, err := openRaxFFISearcher(libPath, storePath)
	requireError("openRaxFFISearcher", err)
	_, err = raxFFISearchText(libPath, storePath, "query", 10)
	requireError("raxFFISearchText", err)
	_, err = raxFFIIngestDocs(libPath, storePath, []byte("{}\n"))
	requireError("raxFFIIngestDocs", err)
}
