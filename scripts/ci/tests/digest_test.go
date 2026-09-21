package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These tests exercise the committed runtime-digest pins through the verifier
// that both setup-ladybug.sh and stage-runtime.sh call before they extract or
// stage native bytes. They use a synthetic asset with a synthetic pin file, so
// no network access and no real credential or release asset is involved.

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	// scripts/ci/tests/digest_test.go -> repo root
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
}

func runVerifier(t *testing.T, env []string, args ...string) (string, error) {
	t.Helper()
	root := repoRoot(t)
	// The verifier is a shell script, which Windows cannot execute directly (it
	// reports "%1 is not a valid Win32 application"). Running it through the
	// shell keeps the same committed verifier under test on every OS instead of
	// skipping the check where that OS cannot fork the script.
	script := filepath.Join(root, "scripts", "ci", "verify-runtime-digest.sh")
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeSyntheticPins(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime-digests.txt")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write pins: %v", err)
	}
	return path
}

// TestVerifyRuntimeDigestRejectsUnpinnedAsset pins the fail-closed half of the
// T-08 contract: an asset with no committed digest pin is refused rather than
// staged on trust.
func TestVerifyRuntimeDigestRejectsUnpinnedAsset(t *testing.T) {
	dir := t.TempDir()
	asset := filepath.Join(dir, "liblbug-osx-arm64.tar.gz")
	if err := os.WriteFile(asset, []byte("authentic runtime bytes"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	// A synthetic pin file that names a different asset, so this one is unpinned.
	pins := writeSyntheticPins(t, "v0.17.0 liblbug-linux-x86_64.tar.gz deadbeef\n")
	out, err := runVerifier(t, []string{"YEOUL_RUNTIME_DIGESTS=" + pins}, "v0.17.0", "liblbug-osx-arm64.tar.gz", asset)
	if err == nil {
		t.Fatalf("expected an unpinned asset to be rejected, got: %s", out)
	}
	if !strings.Contains(out, "no committed digest pin") {
		t.Fatalf("expected a missing-pin rejection, got: %s", out)
	}
}

func TestVerifyRuntimeDigestAcceptsMatchingAndRejectsMismatch(t *testing.T) {
	dir := t.TempDir()
	asset := filepath.Join(dir, "rax-ffi-v0.4.4-macos-arm64.tar.gz")
	if err := os.WriteFile(asset, []byte("authentic rax bytes"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	// Compute the genuine digest with the same tool the verifier uses, then pin it.
	digest := sha256Of(t, asset)
	pins := writeSyntheticPins(t, "v0.4.4 rax-ffi-v0.4.4-macos-arm64.tar.gz "+digest+"\n")
	env := []string{"YEOUL_RUNTIME_DIGESTS=" + pins}

	out, err := runVerifier(t, env, "v0.4.4", "rax-ffi-v0.4.4-macos-arm64.tar.gz", asset)
	if err != nil {
		t.Fatalf("expected the pinned asset to verify, got err=%v out=%s", err, out)
	}

	// Replace the bytes under the same asset name: must be rejected before staging.
	if err := os.WriteFile(asset, []byte("replaced rax bytes"), 0o644); err != nil {
		t.Fatalf("replace asset: %v", err)
	}
	out, err = runVerifier(t, env, "v0.4.4", "rax-ffi-v0.4.4-macos-arm64.tar.gz", asset)
	if err == nil {
		t.Fatalf("expected replaced bytes to be rejected, got: %s", out)
	}
	if !strings.Contains(out, "digest mismatch") {
		t.Fatalf("expected a digest-mismatch rejection, got: %s", out)
	}
}

func sha256Of(t *testing.T, path string) string {
	t.Helper()
	for _, tool := range [][]string{{"sha256sum"}, {"shasum", "-a", "256"}, {"openssl", "dgst", "-sha256"}} {
		cmd := exec.Command(tool[0], append(tool[1:], path)...)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		fields := strings.Fields(string(out))
		if len(fields) == 0 {
			continue
		}
		return strings.ToLower(fields[0])
	}
	t.Skip("no SHA-256 tool available")
	return ""
}

// TestDownloadPathsVerifyBeforeStaging pins the wiring, not just the verifier:
// both download scripts must invoke verify-runtime-digest.sh on the freshly
// downloaded archive before they extract or stage it. Without this, the pins
// would exist but nothing would enforce them.
func TestDownloadPathsVerifyBeforeStaging(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		script     string
		verifyCall string
		staging    string
	}{
		{
			script:     filepath.Join(root, "scripts", "ci", "setup-ladybug.sh"),
			verifyCall: `"${script_dir}/verify-runtime-digest.sh" "${release_ref}" "${candidate}" "${archive_path}"`,
			staging:    "tar -xzf",
		},
		{
			script:     filepath.Join(root, "scripts", "ci", "stage-runtime.sh"),
			verifyCall: `"${script_dir}/verify-runtime-digest.sh" "${rax_version}" "${asset}" "${archive}"`,
			staging:    `unzip -q "${archive}"`,
		},
	}
	for _, tc := range cases {
		data, err := os.ReadFile(tc.script)
		if err != nil {
			t.Fatalf("read %s: %v", tc.script, err)
		}
		body := string(data)
		verifyAt := strings.Index(body, tc.verifyCall)
		if verifyAt < 0 {
			t.Fatalf("%s does not verify the downloaded asset with %s", tc.script, tc.verifyCall)
		}
		stageAt := strings.Index(body, tc.staging)
		if stageAt < 0 {
			t.Fatalf("%s is missing its staging step %q", tc.script, tc.staging)
		}
		if verifyAt > stageAt {
			t.Fatalf("%s stages bytes before verifying them", tc.script)
		}
	}
}
