#!/bin/sh
set -eu

image=${YEOUL_CONTAINER_IMAGE:-yeoul:ci}
platform=${YEOUL_CONTAINER_PLATFORM:-linux/amd64}
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/yeoul-smoke.XXXXXX")
name=${tmpdir##*/}
volume=$name
port=
volume_created=0
container_created=0

cleanup() {
	status=$?
	trap - EXIT HUP INT TERM
	if [ "$container_created" -eq 1 ] && docker container inspect "$name" >/dev/null 2>&1; then
		if docker container inspect --format '{{.State.Running}}' "$name" | grep -q true; then
			docker stop --time 15 "$name" >/dev/null || { printf '%s\n' 'failed to stop smoke container during cleanup' >&2; status=1; }
		fi
		docker rm "$name" >/dev/null || { printf '%s\n' 'failed to remove smoke container during cleanup' >&2; status=1; }
	fi
	if [ "$volume_created" -eq 1 ]; then
		docker volume rm "$volume" >/dev/null || { printf '%s\n' 'failed to remove smoke volume during cleanup' >&2; status=1; }
	fi
	rmdir "$tmpdir" || { printf '%s\n' 'failed to remove smoke temporary directory' >&2; status=1; }
	exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

if docker container inspect "$name" >/dev/null 2>&1 || docker volume inspect "$volume" >/dev/null 2>&1; then
	printf 'refusing to use pre-existing Docker resource named %s\n' "$name" >&2
	exit 1
fi

if docker image inspect "$image" >/dev/null 2>&1; then
	image_platform=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image")
	if [ "$image_platform" != "$platform" ]; then
		printf 'image platform is %s, requested %s\n' "$image_platform" "$platform" >&2
		exit 1
	fi
else
	docker build --platform "$platform" -t "$image" .
fi
user=$(docker image inspect --format '{{.Config.User}}' "$image")
case "$user" in
	''|0|root|0:*)
		printf 'container must run as a non-root user (configured user: %s)\n' "$user" >&2
		exit 1
		;;
esac
docker volume create "$volume" >/dev/null
volume_created=1

if docker run --rm --read-only --tmpfs /tmp:rw,noexec,nosuid,size=16m --cap-drop=ALL \
	--security-opt=no-new-privileges:true --mount "type=volume,src=$volume,dst=/data" \
	-e YEOUL_HTTP_TOKEN=smoke-token "$image" --db /data/yeoul.ltdb --listen 0.0.0.0:8080 2>/dev/null; then
	printf '%s\n' 'container unexpectedly created a missing database' >&2
	exit 1
fi

if output=$(docker run --rm --read-only --tmpfs /tmp:rw,noexec,nosuid,size=16m --cap-drop=ALL \
	--security-opt=no-new-privileges:true --mount "type=volume,src=$volume,dst=/data" \
	-e YEOUL_HTTP_TOKEN= "$image" --db /data/yeoul.ltdb --listen 0.0.0.0:8080 --create 2>&1); then
	printf '%s\n' 'container unexpectedly started without a required token' >&2
	exit 1
fi
printf '%s\n' "$output" | grep -F 'YEOUL_HTTP_TOKEN is required' >/dev/null || {
	printf 'missing-token run failed for the wrong reason: %s\n' "$output" >&2
	exit 1
}

docker run -d --name "$name" --read-only --tmpfs /tmp:rw,noexec,nosuid,size=16m --cap-drop=ALL \
	--security-opt=no-new-privileges:true --mount "type=volume,src=$volume,dst=/data" \
	-p 127.0.0.1::8080 -e YEOUL_HTTP_TOKEN=smoke-token "$image" --db /data/yeoul.ltdb --listen 0.0.0.0:8080 --create >/dev/null
container_created=1
port=$(docker port "$name" 8080/tcp | head -n 1 | sed 's/.*://')
[ -n "$port" ] || { printf '%s\n' 'Docker did not publish the container port' >&2; docker logs "$name" >&2; exit 1; }
attempt=0
until curl --fail --silent --show-error -H 'Authorization: Bearer smoke-token' \
	"http://127.0.0.1:$port/v1/health" >/dev/null 2>&1; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge 30 ]; then
		docker logs "$name" >&2
		printf '%s\n' 'container health check timed out' >&2
		exit 1
	fi
	sleep 1
done

status=$(curl --silent --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:$port/v1/health")
[ "$status" = 401 ] || { printf 'unauthenticated health status = %s, want 401\n' "$status" >&2; exit 1; }

curl --fail --silent --show-error -H 'Authorization: Bearer smoke-token' \
	-H 'Content-Type: application/json' \
	-d '{"space_id":"default","episode":{"id":"ep-container-smoke","kind":"note","content":"container persistence smoke"}}' \
	"http://127.0.0.1:$port/v1/episodes" >/dev/null
curl --fail --silent --show-error -H 'Authorization: Bearer smoke-token' \
	"http://127.0.0.1:$port/v1/episodes/ep-container-smoke?space_id=default" | grep -F 'container persistence smoke' >/dev/null

docker stop --time 15 "$name" >/dev/null
docker rm "$name" >/dev/null
container_created=0
docker run -d --name "$name" --read-only --tmpfs /tmp:rw,noexec,nosuid,size=16m --cap-drop=ALL \
	--security-opt=no-new-privileges:true --mount "type=volume,src=$volume,dst=/data" \
	-p 127.0.0.1::8080 -e YEOUL_HTTP_TOKEN=smoke-token "$image" >/dev/null
container_created=1
port=$(docker port "$name" 8080/tcp | head -n 1 | sed 's/.*://')
[ -n "$port" ] || { printf '%s\n' 'Docker did not republish the container port' >&2; docker logs "$name" >&2; exit 1; }
attempt=0
until curl --fail --silent --show-error -H 'Authorization: Bearer smoke-token' \
	"http://127.0.0.1:$port/v1/health" >/dev/null 2>&1; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge 30 ]; then
		docker logs "$name" >&2
		printf '%s\n' 'container failed to reopen its persisted database' >&2
		exit 1
	fi
	sleep 1
done

curl --fail --silent --show-error -H 'Authorization: Bearer smoke-token' \
	"http://127.0.0.1:$port/v1/episodes/ep-container-smoke?space_id=default" | grep -F 'container persistence smoke' >/dev/null
