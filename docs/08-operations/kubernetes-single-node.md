# Kubernetes Single-Node Rhiza

This deployment runs one Yeoul HTTP daemon with ephemeral local Rhiza storage
and a required remote object-store archive. It uses `emptyDir`, not a PVC. Pod
replacement loses the entire local `DataDir`; recovery must use the remote
archive. It does not use the Rhiza Operator or form a voter cluster.

## Init restore handoff

The init container runs Yeoul's restore-only command against
`/data/yeoul.ltdb`, using `--rhiza-backup-config`, `--restore-if-missing`, and
`--restore-only`. The command calls Rhiza `Open`, waits for recovery to finish,
then calls `Close`; it starts no listener and needs no HTTP token. It mounts the
shared `data` `emptyDir`, the backup Secret read-only at
`/tmp/config/backup.json`, and `/tmp`. A successful exit lets Kubernetes start
the main container; any open or close error blocks it.

This is the application-owned init-container pattern documented for Rhiza
v0.21.1, not a Rhiza-provided CLI or image. The main process opens the same
`DataDir` with the same Rhiza configuration and object-store access, but
without `--restore-if-missing` or `--create`. The second `Open` validates the
binding and reconciles history; it is not guaranteed to be read-only. Keep the
same ClusterID, NodeID, bucket, prefix, credentials, and other identity
settings across both opens. Do not switch to `Async`, and do not start another
process against this database or archive namespace. See the upstream
[Local recovery with an init container guide](https://github.com/mrchypark/rhiza/blob/v0.21.1/docs/local-init-container-recovery.md).

The current Yeoul Rhiza adapter fixes `NodeID` to `yeoul-local` and leaves
`ClusterID` at the Rhiza SDK default. Run the same Yeoul build in init and main
and keep the database path and backup configuration identical so both opens
use the same storage identity.

Kubernetes runs init containers for a new Pod before starting its application
containers; an ordinary main-container restart does not rerun init. If the
database unexpectedly disappears or cannot be opened after init completed,
main has no restore fallback and must fail closed. Rerun init only by creating
a replacement Pod, after proving the old writer is stopped or fencing it at
the infrastructure layer.

The manifest depends on the server CLI implementing `--restore-only` as
`Open` followed by `Close`, requiring `--restore-if-missing`, `--db`, and
`--rhiza-backup-config`, and starting no HTTP listener. Verify that CLI
contract in the built server before publishing or deploying this manifest.

## Build and prepare

Yeoul `v0.13.0` does not contain this CLI contract. Use the published
`ghcr.io/mrchypark/yeoul:v0.14.0` image only after the v0.14.0 Container
workflow has completed successfully and the registry digest has been verified.
For a later Yeoul release, first verify it still implements this CLI contract.
Pin the verified digest for both init and main containers; do not deploy by a
mutable tag alone. No native Rhiza init image is provided or required: both
containers use the Yeoul application image.

For a source-build alternative, check out the exact Yeoul release tag whose
server implements `--rhiza-backup-config`, `--restore-if-missing`, and
`--restore-only`, then build without local modifications:

```sh
BUILD_ID=$(
  find Dockerfile go.mod go.sum cmd/yeould internal pkg -type f -print |
    LC_ALL=C sort |
    while IFS= read -r file; do shasum -a 256 "$file"; done |
    shasum -a 256 | cut -c1-16
)
docker build -t "yeoul:rhiza-single-node-restore-${BUILD_ID}" .
```

Before any Kubernetes command, set and review the exact cluster context and an
already-existing namespace. The helper makes both explicit on every `kubectl`
call; do not change the machine-wide current context:

```sh
: "${KUBE_CONTEXT:?Set KUBE_CONTEXT to the reviewed cluster context}"
: "${KUBE_NAMESPACE:?Set KUBE_NAMESPACE to the reviewed existing namespace}"
k() {
  kubectl --context "$KUBE_CONTEXT" --namespace "$KUBE_NAMESPACE" "$@"
}
```

The manifests do not create a Namespace. Verify the selected namespace exists;
if it does not, prepare it separately only after reviewing the same explicit
cluster context.

Push that uniquely tagged build to a registry available to the cluster and
take the digest reported by the registry. In `kustomization.yaml`, replace
`registry.example.invalid/yeoul` with the image repository and replace
`sha256:REPLACE_WITH_PUSHED_IMAGE_DIGEST` with that digest. The base Deployment
image tag is a Kustomize transform input, not a deployable image reference;
apply only with `k apply -k deploy/kubernetes`. The transform assigns the
same immutable digest reference to init and main containers. Verify the
rendered manifest uses that same pinned reference for both. Runtime `imageID`
may report the platform-specific manifest or image identity rather than the
requested multi-platform index digest; compare it with the expected platform
image resolved from the verified registry digest, not by requiring literal
equality with the index digest. `IfNotPresent` cannot select stale content
when the image is pinned by digest.

Create a Secret containing one strict JSON object at key `backup.json`. The
keys match the exported Go fields of `yeoul.RhizaBackupConfig`:

```json
{
  "Provider": "s3",
  "Endpoint": "",
  "Bucket": "replace-with-a-new-bucket",
  "Prefix": "yeoul/single-node",
  "Region": "",
  "Insecure": false,
  "AccessKey": "replace-me",
  "SecretKey": "replace-me",
  "SessionToken": "",
  "GCSServiceAccount": "",
  "AzureStorageAccount": "",
  "AzureStorageAccountKey": "",
  "AzureConnectionString": ""
}
```

Use the provider fields that apply and leave unrelated fields empty. Supported
providers are `s3`, `gcs`, and `azure`; see [local storage](local-storage.md)
for endpoint and Azure credential constraints. Create the Secret outside this
repository, for example from a protected file:

```sh
k create secret generic yeoul-rhiza-backup \
  --from-file=backup.json=/secure/path/backup.json
```

The Secret is mounted read-only at `/tmp/config/backup.json` in both
containers. `/tmp` is a separate memory-backed `emptyDir` for the main daemon.

Create a separate Secret with the existing `YEOUL_HTTP_TOKEN` key:

```sh
k create secret generic yeoul-http-auth \
  --from-env-file=/secure/path/yeoul-http.env
```

The env file must contain `YEOUL_HTTP_TOKEN=<long-random-value>` and be
readable only by its owner. Do not commit either Secret or its source files.
Kubernetes Secret storage should be encrypted at rest and access restricted by
RBAC.

The standard Deployment expects a compatible, initialized Rhiza Local archive
at the configured remote namespace. A valid, quiesced archive created with
Rhiza `v0.21.0` can be reopened by `v0.21.1` with the same identity and backup
configuration because the v0.21.1 release does not change the runtime or
archive format. This handoff is not a conversion and does not require a fresh
archive. Preserve single-writer ownership throughout. Pre-v0.21.0 formats are
not automatically migrated; keep their matching Yeoul release and archive
unless a separately validated migration is available. The Deployment never
uses `--create` or
`--restore-if-missing` in the recurring main container. The init container
restores into the shared local `DataDir` and closes it before main starts. If
no compatible archive exists or restore fails, init must fail closed and the
main container must not start; do not change the archive prefix or permit an
empty database to be created as a fallback.

Apply the manifests after the image and both Secrets are available:

```sh
k apply -k deploy/kubernetes
k get deploy,pod,service -l app.kubernetes.io/name=yeoul
```

The Service is `ClusterIP` only; no Ingress or public LoadBalancer is created.
The bearer token is required on the non-loopback listener.

## Ownership and recovery

The Deployment is fixed at one replica and uses `Recreate` so a normal rollout
does not intentionally overlap old and new Pods. `Recreate` is not fencing.
Before starting a replacement after an unreachable node, prove the old process
is stopped or fence the old node at the infrastructure layer. A deleted or
terminating Pod object alone does not prove that its process can no longer
write. Never let two processes recover or write the same archive namespace.

The init process and main process share ownership sequentially: init must
successfully close `/data/yeoul.ltdb` before main opens that path. `emptyDir`
survives a container restart within the same Pod, but is removed when the Pod
is replaced or rescheduled. The object-store bucket and
prefix must be unique to this database, stable for its lifetime, and outside
the Pod lifecycle. Do not co-locate the only archive copy with the Pod or rely
on an emulator as production backup storage. Keep independent backups and
test recovery before relying on it. Keep Rhiza's `BeforeAck` durability mode;
do not switch this deployment to `Async`, which can lose acknowledged writes
if the whole Pod-local `DataDir` is lost before those writes reach the archive.
`BeforeAck` does not protect against provider-side loss, revoked credentials,
or a misconfigured namespace. Upstream
[`TestEmbeddedObjectStorageCheckpointAndTailTotalLoss`](https://github.com/mrchypark/rhiza/blob/v0.21.1/embedded_object_storage_checkpoint_test.go#L116)
failed once in the [S3 provider job](https://github.com/mrchypark/rhiza/actions/runs/40697711614/job/122848820051)
with `Local maintenance control changed during read`; the same test passed for
GCS and Azure in that job. The later [upstream provider run](https://github.com/mrchypark/rhiza/actions/runs/40700300657/job/122857983882)
passed, so this is an intermittent unresolved S3 qualification issue, not
proof that every S3 restore fails. Yeoul's separate three-provider recovery
tests do not clear that upstream failure or establish production-cloud parity.

The startup probe allows up to 30 minutes for the process to open or recover
the database before Kubernetes begins liveness checks. Adjust it for measured
restore times. `terminationGracePeriodSeconds` is also an operational default;
increase it if measured graceful shutdown takes longer. Readiness and liveness
use TCP because the HTTP `/v1/health`
route is protected by bearer authentication and a Kubernetes HTTP probe cannot
read the Secret environment variable into its header. The TCP readiness probe
passes once the daemon has opened its database and started listening.

Pre-v0.21.0 archive formats are not automatically migrated; keep those
installations on their matching Yeoul/Rhiza release and archive until a
separately verified migration is available. For this NoPVC deployment, a valid
and quiesced Rhiza v0.21.0 archive can restore into a new Pod's empty `DataDir`
under v0.21.1 when the same ClusterID, NodeID, bucket, prefix, credentials, and
other identity settings are used and single-writer ownership is maintained.
No new archive is required for that handoff. Create a fresh archive only for a
genuinely new database or a separately verified migration. If restore fails,
do not change the prefix or allow an empty replacement database to be created.

The current workload has no Operator, voter membership, multi-node failover,
or automated fencing. It does not need Kubernetes API access and disables
ServiceAccount token automount. `emptyDir` and object-store recovery do not
provide HA or preserve local state across Pod replacement.

### First-time Kubernetes bootstrap

Bootstrap is a separate, controlled operation and is not part of these
manifests. Only for a genuinely new Yeoul database and a new, unique, empty
remote archive namespace, start one explicitly managed process using the
pinned image and backup Secret with `--create` and without
`--restore-if-missing`. Do not run it alongside the standard Deployment or any
other owner. `yeould --create` must successfully initialize actual Yeoul state
before the remote archive is treated as a recovery source; verify a Yeoul-level
write and read it back, and confirm the resulting archive before stopping the
process. The command starts the HTTP daemon rather than exiting after creation:
send SIGTERM, wait for graceful exit, and prove it is stopped. Then start the
standard restore-only Deployment. Never put `--create` in a recurring workload
or use this procedure for an existing, partial, or uncertain archive.

```sh
k delete deployment yeoul-bootstrap --wait=true
```

Do not reuse this bootstrap procedure for an existing database, archive, or
uncertain state.

## Non-Kubernetes use

The same ownership contract applies without Kubernetes: one supervised
`yeould` process owns a durable local `DataDir`, while Rhiza performs remote
restore during `Open` when explicitly configured. Protect the JSON config and
HTTP token as files/secrets outside the database directory.

For a genuinely new database and archive namespace, create them once in a
separate controlled invocation. This is bootstrap, not an init or recurring
startup command. The command starts the long-running daemon; it does not exit
after initializing storage:

```sh
yeould --db /var/lib/yeoul/yeoul.ltdb \
  --listen 127.0.0.1:8080 \
  --rhiza-backup-config /etc/yeoul/backup.json \
  --create
```

Wait for the new database and archive to be verified. Then send SIGTERM through
the supervisor, wait for graceful exit, and confirm the process is stopped
before starting another owner. Start the regular daemon without `--create` and
with `--restore-if-missing` so a missing local directory can be recovered from
the same archive after full local data loss:

```sh
IFS= read -r YEOUL_HTTP_TOKEN < /etc/yeoul/http-token
export YEOUL_HTTP_TOKEN
yeould --db /var/lib/yeoul/yeoul.ltdb \
  --listen 127.0.0.1:8080 \
  --rhiza-backup-config /etc/yeoul/backup.json \
  --restore-if-missing
```

For a daemon outside loopback, provide `YEOUL_HTTP_TOKEN` through the
supervisor and put a trusted TLS boundary in front of the HTTP listener. Never
use `--create` for recovery, and never start a second process until the
previous owner is proven stopped.
