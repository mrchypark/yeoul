# Rhiza v0.23.0 Upgrade Validation

Date: 2026-11-06

## Version and Cross-Version Data

The root module selects `github.com/mrchypark/rhiza v0.23.0`; `go.sum`
records `h1:RFLhLytoi2Q84MD4WkA9Z8T7wvUp7LPj4nxIMFBSaNw=`.

`TestRhiza0212DatabaseOpensWithCurrentSDK` passed. Its isolated fixture is
created with Yeoul v0.14.0 and Rhiza v0.21.2, closed, then opened by the
current checkout. It verifies episode and entity data, entity and fact
revisions, active/superseded fact state, and provenance. The fixture helper
checks its selected Rhiza version and refuses a non-absent target path.

Command:

```sh
GOWORK=off go test ./pkg/yeoul \
  -run '^TestRhiza0212DatabaseOpensWithCurrentSDK$' -count=1 -v
```

Result: PASS.

## NoPVC Handoff

`TestSingleNodeKubernetesManifestContract` passed. It verifies the declared
init/main configuration: restore-only against the shared `/data` emptyDir,
and main opens that same path without restore or create flags. This static
manifest check does not observe container completion ordering at runtime. No
HAOperator or Kubernetes resources were changed or deployed.

Command:

```sh
GOWORK=off go test ./scripts/ci \
  -run '^TestSingleNodeKubernetesManifestContract$' -count=1 -v
```

Result: PASS (declared manifest contract only; not a live Kubernetes handoff
test).
The 2026-11-02 report remains the historical v0.21.2 runtime validation and
does not qualify v0.23.0.

## Provider Recovery

The pinned Floci harness was invoked with
`scripts/ci/test-rhiza-object-recovery.sh`. It exited before starting any
provider or creating containers: Docker API unavailable. The active `dory`
context points to the missing `~/.dory/dory.sock`; `default` points to the
missing `/var/run/docker.sock`. Inventory showed no reachable local Docker
runtime. The empty temporary harness directory was inspected and removed; no
containers or other harness resources remain.

| Provider | v0.23.0 contract status |
| --- | --- |
| S3 / Floci | Not run: harness stopped before provider setup |
| GCS / Floci-GCP | Not run: harness stopped before provider setup |
| Azure / Floci-AZ | Not run: harness stopped before provider setup |

Therefore actual SDK-adapter remote full-loss recovery, restored HTTP writes,
revision/provenance oracle over remote recovery, and all-provider qualification
remain unverified for v0.23.0. No provider pass is claimed. Kubernetes
runtime qualification was not run. Provider qualification is required in the
arm64 CI job before merge; append its run link and provider evidence here after
that job passes.

`TestKubernetesDRProvisionBucket` is an opt-in provisioning helper, not a
cleanup test. It creates a new bucket and intentionally leaves it available to
an external Kubernetes lifecycle test. The caller owns cleanup: use a
disposable emulator and isolated test namespace, then delete the bucket (or
delete the namespace containing the disposable emulator) after the lifecycle
run. This helper was not invoked for this validation.

## Other Local Checks

The following focused tests passed:

```sh
GOWORK=off go test ./pkg/yeoul -run \
  '^(TestNativeBindingRefusalRequiresExactCauseUnderStorageFailure|TestGCSEmulatorServiceAccountUsesLocalTokenURI|TestGCSEmulatorServiceAccountRejectsNonlocalEndpoint)$' \
  -count=1 -v
```

The implementer separately reported root `go test -race`, `go vet`, native,
and Windows verification passing; those results are implementer-reported and
were not rerun as part of this verifier pass.

## Mistake Candidates

- `tool`: one cleanup command was run from the fixture module directory while
  using a repository-relative script path. It failed before changing files;
  rerunning from the repository root succeeded. Task-local and corrected; no
  lasting control change indicated.
