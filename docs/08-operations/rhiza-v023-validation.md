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

### Local Runtime

The local attempt invoked `scripts/ci/test-rhiza-object-recovery.sh`, which
exited before provider setup because the Docker API was unavailable. The active
`dory` context points to the missing `~/.dory/dory.sock`; `default` points to
the missing `/var/run/docker.sock`. The empty temporary harness directory was
inspected and removed. No local provider tests or containers ran.

### ARM64 CI Qualification

The pinned Floci provider-recovery job passed on PR merge commit
`c3a2862e83f43c847ec568ce70827ff623f29adb`, whose tree matches PR head
`9736661236ac5cef22460b6dd676c60ae0181193`, using an Ubuntu 24.04 ARM64 runner
and Go 1.27.3. [Run 42136940380, provider-recovery job 127627377777](https://github.com/mrchypark/yeoul/actions/runs/42136940380/job/127627377777)
ran `GOWORK=off timeout 600 scripts/ci/test-rhiza-object-recovery.sh` with
`DOCKER_CONTEXT=default`.

The harness summary was:

```text
s3-baseline=0 s3-legacy=0 gcs-baseline=0 gcs-legacy=0 azure-baseline=0 azure-legacy=0
```

For each provider, `TestRhizaObjectStoreFullLossRecovery` passed against its
pinned Floci service through the provider SDK adapter, along with
`TestRhiza020ArchiveCompatibility`. The full-loss test restored after local
root loss, verified the stored Yeoul snapshot including lifecycle revisions
and provenance, performed and read back an HTTP write, and verified it again
after recovery. The harness also passed the missing-archive and read-only
guard tests for each provider. The job log showed no skipped test results.
S3, GCS, and Azure provider recovery are qualified by this CI run for this
exact head.

This CI evidence does not qualify a live v0.23.0 Kubernetes deployment or
init-to-main runtime handoff.

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
