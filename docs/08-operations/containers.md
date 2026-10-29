# Container Image

Yeoul publishes the `yeould` HTTP service as a multi-platform image for Linux
amd64 and arm64. Pull an explicit release tag; floating `latest` tags are not
published:

```sh
docker pull ghcr.io/mrchypark/yeoul:v0.11.1
```

Use the alternate explicit tag `0.11.1` if that convention fits your deploy
system. The image contains only the `yeould` binary, runs as UID/GID 65532, and
uses a read-only-root-compatible scratch filesystem.

## Run

The default command listens on `0.0.0.0:8080` and opens
`/data/yeoul.ltdb` without `--create`. On first use, create an empty persistent
volume and start once with the explicit `--create` flag. Stop the service with
Ctrl+C after initialization, then start it normally without `--create`:

Pass the bearer token with a protected env file, not in the image or command
line. For example, create `yeoul.env` with restrictive permissions and this
entry:

```text
YEOUL_HTTP_TOKEN=replace-with-a-long-random-secret
```

```sh
chmod 600 yeoul.env
docker volume create yeoul-data
docker run --rm --name yeould-init \
  --publish 127.0.0.1:8080:8080 \
  --env-file ./yeoul.env \
  --mount type=volume,src=yeoul-data,dst=/data \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m \
  --cap-drop=ALL \
  --security-opt=no-new-privileges:true \
  ghcr.io/mrchypark/yeoul:v0.11.1 \
  --db /data/yeoul.ltdb --listen 0.0.0.0:8080 --create

docker run -d --name yeould \
  --publish 127.0.0.1:8080:8080 \
  --env-file ./yeoul.env \
  --mount type=volume,src=yeoul-data,dst=/data \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m \
  --cap-drop=ALL \
  --security-opt=no-new-privileges:true \
  ghcr.io/mrchypark/yeoul:v0.11.1
```

For a bind mount, make its host directory writable by UID/GID 65532 before
starting the container, for example with `chown 65532:65532 ./yeoul-data`.
Keep the database on persistent storage; `/tmp` is disposable and is not a
database location. One service process must exclusively own a writable Yeoul
database. Do not mount one writable database into multiple containers or
processes.

Do not initialize against an existing or unmigrated user database. `--create`
allows creation when the selected path is missing; verify that the volume and
database path are the intended new target first.

The example publishes only on host loopback. To allow remote clients, choose an
appropriate network boundary and retain bearer authentication. `yeould` serves
HTTP, not TLS; terminate TLS at a trusted reverse proxy or platform ingress
before exposing traffic beyond a trusted local network. Do not treat the bearer
token as transport encryption.

When Rhiza object-store backup is configured, the service makes outbound HTTPS
requests to the selected provider. Permit egress to the configured object-store
endpoint and ensure the image contains trusted CA roots for TLS verification;
the scratch base image does not provide a CA bundle by itself. Without remote
backup, the service has no other outbound network dependency. SQLite's
embedded WASM runtime may create temporary files; keep `/tmp` writable as in
the examples. No shell or utilities are included in the image, so use container
logs and HTTP health requests rather than `docker exec` shell commands for
routine inspection.

The examples use Docker's read-only root filesystem, tmpfs, dropped Linux
capabilities, and `no-new-privileges` options. See Docker's documentation for
[run-time security options](https://docs.docker.com/engine/containers/run/),
[tmpfs mounts](https://docs.docker.com/engine/storage/tmpfs/), and
[volume mounts](https://docs.docker.com/engine/storage/volumes/).

The Dockerfile pins the official Go builder's multi-platform image index by
digest and builds a static target-architecture binary. CI publishes
`linux/amd64` and `linux/arm64` images for explicit `v<version>` and
`<version>` tags only; it does not publish `latest`.
