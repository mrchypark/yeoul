# Kubernetes NoPVC DR Validation

Date: 2026-11-02

Result: lifecycle validation passed on the published Yeoul v0.14.0 image. This
is a single-node graceful lifecycle test, not partition-fencing or HA evidence.

## Baseline

- Cluster: local dory k3s, Kubernetes `v1.36.2+k3s1`, one Ready arm64 node
  `12d749af5d4f`, node UID `81e626c8-7f86-4e82-9e12-c110ff4dcb96`.
- Yeoul image index:
  `ghcr.io/mrchypark/yeoul@sha256:30c841b2e7479bd683d2ae1a82165a46a48f1c8bb7062436eeaafd247fb8f55c`.
  The registry advertised `linux/arm64` child manifest
  `sha256:18eb5dd8b462cdc419f8124ac47445975dae4199568f9a18929b34efd339459a`.
- Rhiza Go SDK: `github.com/mrchypark/rhiza v0.21.2` from `go.mod`.
- S3 emulator: Floci
  `docker.io/floci/floci@sha256:2b60d1b3843084a366cc05d86f089f8e54271b4b62094ea6706831bbea277a00`,
  in a separate Pod and Service from Yeoul.
- API endpoint used by both bootstrap and recovery:
  `floci-s3.yeoul-dr-validation-20261102.svc:4566`.

The standard manifests in `deploy/kubernetes/` were rendered and applied with
only the Kustomize image placeholder replaced by the pinned Yeoul digest above.
Init and main used the same image, database path, backup Secret, and S3
identity. The workload had one replica, `Recreate`, no PVC, `emptyDir` for
local data, non-root execution, read-only root filesystem, and no ServiceAccount
token. The Secret values and private kubeconfig are intentionally not retained.

Manifest image substitution, applied to both init and main:

```diff
- image: registry.example.invalid/yeoul@sha256:REPLACE_WITH_PUSHED_IMAGE_DIGEST
+ image: ghcr.io/mrchypark/yeoul@sha256:30c841b2e7479bd683d2ae1a82165a46a48f1c8bb7062436eeaafd247fb8f55c
```

No repository Deployment or Service manifest was changed for the test.

## Lifecycle Evidence

1. A controlled bootstrap Pod used `--create` with the same image, security
   context, backup configuration, and endpoint as the standard Deployment.
   Its Pod UID was `6bedbe2d-ae4d-4c55-ab1e-a172c9ece586`; main container ID was
   `38d854987f5373a1d8f1b1e7ba1e24dda35e4a3de0e74c15d5aed5e1a819dbeb`.
2. Authenticated HTTP `POST /v1/episodes` returned `201`; `GET` of that episode
   returned `200` and contained `k8s-pod-lifecycle-recovery-marker`.
3. The bootstrap Pod was deleted with a 60-second grace period. Before
   deploying the standard workload, CRI reported the captured bootstrap
   container was not running (`old_container_running=0`).
4. Standard Pod `yeoul-589678476d-srzsp`, UID
   `88061174-71fb-4b13-850e-aaad03f8f8ae`, restored successfully. Init
   container ID was
   `36dfa006f0885faf3c40ff8abf09cc497c8ed5afcb0440377525e5505707a216`, exit
   code `0`, finished `2026-11-02T02:52:40Z`; main container
   `bc8230b238654505a02948ff3b07358d0ec25128f26c86a432008e5da10d91c2` started
   `2026-11-02T02:52:41Z`. Init completion preceded main startup.
5. The recovered marker returned HTTP `200`. A second authenticated HTTP write
   returned `201`; its read returned `200` and contained
   `k8s-pod-same-container-restart-marker`.
6. CRI gracefully stopped only the recorded main container with a 60-second
   timeout. Kubelet restarted main in the same Pod UID: restart count became
   `1`, old container was not running, replacement container was running, and
   the init container ID and finish timestamp were unchanged. Both markers
   remained readable over HTTP (`200`).
7. Deployment was scaled to zero; the old Pod was awaited to deletion. Before
   scaling back to one, CRI reported the old main container
   `148873543367f80c95b6994bf068fc40fb3edf96f168b57445093846f5927106` was not
   running and there were zero Yeoul Pods.
8. Replacement Pod `yeoul-589678476d-mvvl7`, UID
   `f89b5073-00ed-4698-b88b-e49ff0c1b5b6`, had a new `emptyDir`. Its restore
   init ID was
   `a014e60646fef47ca951510faf608d1117ce382aa9aafe7dbe118b47313d2975`, exit
   code `0`, finished `2026-11-02T02:54:38Z`; new main container
   `1af7cb961cf63762ec0facdaaf516bb4b101320e732680dc50128c73608fbe4c` started
   `2026-11-02T02:54:39Z`. Both stored markers returned HTTP `200` after recovery.

Sanitized CRI/API observations: `old_main_CRI_running=0`,
`new_main_CRI_running=1`, same-Pod `restartCount=1`, unchanged init ID and
`finishedAt`, zero Yeoul Pods before replacement, and HTTP results `201`,
`200`, `201`, `200`, `200`, `200` for the writes and reads described above.
No auth token, backup JSON, or kubeconfig contents are included here.

## Cleanup and Limits

Namespace `yeoul-dr-validation-20261102` was deleted and verified NotFound.
The host and in-node port-forward listeners on ports `14566` and `14567` were
verified absent; the host Service port-forward listener was absent as well.
The private temp directory containing kubeconfig, test Secret source files,
and response files was removed. Pulled image layers remain in the shared k3s
cache; they were left intact.

This proves the graceful single-node lifecycle only. It does not prove recovery
is safe through a partition: `replicas: 1`, `Recreate`, Pod deletion, and
`emptyDir` do not fence an unreachable old node. Manually prove the old process
stopped or fence that node out of band before starting a replacement. The
published image is scratch-based and has no shell, so a direct in-container
`/tmp` write probe was not performed. Both init and main started and recovered
with the configured mounts and read-only root filesystem.

Source checks run after the final opt-in guard: `GOWORK=off go test
./pkg/yeoul -run '^TestKubernetesDRProvisionBucket$' -count=1 -v` (skipped by
default as intended) and `GOWORK=off go test ./pkg/yeoul` (passed).
