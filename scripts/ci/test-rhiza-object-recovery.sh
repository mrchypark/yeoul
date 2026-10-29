#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
run_dir=$(mktemp -d "${TMPDIR:-/tmp}/yeoul-rhiza-recovery.XXXXXX")
run_id=${run_dir##*/}
created_ids=

cleanup() {
	status=$?
	trap - EXIT HUP INT TERM
	failed=0
	if [ "$status" -ne 0 ]; then
		for id in $created_ids; do
			name=$(docker inspect --format '{{.Name}}' "$id" 2>/dev/null | sed 's#^/##' || true)
			if [ -n "$name" ]; then docker logs "$id" >"$run_dir/container-$name.log" 2>&1 || true; fi
		done
	fi
	for id in $created_ids; do
		owner=$(docker inspect --format '{{index .Config.Labels "io.yeoul.recovery-run"}}' "$id" 2>/dev/null || true)
		if [ "$owner" != "$run_id" ]; then
			echo "refusing to remove container $id: recovery ownership label did not match" >&2
			failed=1
		elif ! docker rm -f "$id" >/dev/null 2>&1; then
			echo "failed to clean up task-owned container $id" >&2
			failed=1
		fi
	done
	if remaining=$(docker ps --all --filter "label=io.yeoul.recovery-run=$run_id" --format '{{.ID}}' 2>/dev/null); then
		if [ -n "$remaining" ]; then
			echo "task-owned containers remain after cleanup: $remaining" >&2
			failed=1
		fi
	else
		echo "could not verify cleanup of task-owned containers for run $run_id" >&2
		failed=1
	fi
	echo "preserving provider baseline/legacy logs in $run_dir" >&2
	if [ "$failed" -ne 0 ]; then
		exit 1
	fi
	exit "$status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

command -v docker >/dev/null 2>&1 || { echo "docker is required" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "go is required" >&2; exit 1; }

if ! docker version --format '{{.Server.Version}}' >/dev/null 2>&1; then
	echo "Docker API is unavailable; no provider tests were run" >&2
	exit 2
fi
docker_context=$(docker context show)
expected_docker_context=${YEOUL_RECOVERY_EXPECTED_DOCKER_CONTEXT:-socktainer}
if [ "$docker_context" != "$expected_docker_context" ]; then
	echo "refusing to run provider tests with Docker context $docker_context; expected $expected_docker_context" >&2
	exit 2
fi

assert_unused_name() {
	name=$1
	existing=$(docker ps --all --filter "name=^/$name$" --format '{{.Names}}') || {
		echo "could not check Docker name collision for $name" >&2
		exit 2
	}
	if [ -n "$existing" ]; then
		echo "refusing to start: Docker container name already exists: $name" >&2
		exit 2
	fi
}

start_container() {
	name=$1
	port=$2
	image=$3
	assert_unused_name "$name"
	id=$(docker run --pull=missing --detach --name "$name" \
		--label io.yeoul.recovery-test=true --label "io.yeoul.recovery-run=$run_id" \
		-p "127.0.0.1:$port:$port" "$image") || return 1
	created_ids="$created_ids $id"
}

check_port() {
	port=$1
	status=$(curl --silent --show-error --max-time 1 --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:$port/" 2>/dev/null || true)
	if [ -n "$status" ] && [ "$status" != "000" ]; then
		echo "refusing to use occupied loopback port $port (HTTP $status)" >&2
		exit 2
	fi
}

wait_http() {
	name=$1
	port=$2
	count=0
	while [ "$count" -lt 60 ]; do
		status=$(curl --silent --max-time 2 --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:$port/" 2>/dev/null || true)
		if [ -n "$status" ] && [ "$status" != "000" ]; then
			return
		fi
		count=$((count + 1))
		sleep 1
	done
	echo "Floci container $name did not become reachable on 127.0.0.1:$port" >&2
	docker logs "$name" >&2 || true
	exit 1
}

check_port 4566
check_port 4588
check_port 4577

start_container "yeoul-rhiza-recovery-s3-$run_id" 4566 \
	docker.io/floci/floci@sha256:2b60d1b3843084a366cc05d86f089f8e54271b4b62094ea6706831bbea277a00
start_container "yeoul-rhiza-recovery-gcs-$run_id" 4588 \
	docker.io/floci/floci-gcp@sha256:3defb6e4b037c9ea0555e719c38ea5840159c15145a1872dc651b0c6bc9b2bbb
start_container "yeoul-rhiza-recovery-azure-$run_id" 4577 \
	docker.io/floci/floci-az@sha256:6a48e35207f041f05aeede4ecba6291e0b26a0f18675a82d6465ddb8fbef00ce

wait_http "yeoul-rhiza-recovery-s3-$run_id" 4566
wait_http "yeoul-rhiza-recovery-gcs-$run_id" 4588
wait_http "yeoul-rhiza-recovery-azure-$run_id" 4577

cd "$repo_root"
GOWORK=off go build -o "$run_dir/yeould" ./cmd/yeould
results=
failed=0
run_provider() {
	provider=$1
	endpoint=$2
	bucket=yeoul-test
	if [ "$provider" = s3 ]; then
		provider_env="YEOUL_RECOVERY_PROVIDER=$provider YEOUL_RECOVERY_S3_ENDPOINT=$endpoint YEOUL_RECOVERY_S3_BUCKET=$bucket"
	elif [ "$provider" = gcs ]; then
		provider_env="YEOUL_RECOVERY_PROVIDER=$provider YEOUL_RECOVERY_GCS_ENDPOINT=$endpoint YEOUL_RECOVERY_GCS_BUCKET=$bucket"
	else
		provider_env="YEOUL_RECOVERY_PROVIDER=$provider YEOUL_RECOVERY_AZURE_ENDPOINT=$endpoint YEOUL_RECOVERY_AZURE_BUCKET=$bucket"
	fi
	run_case baseline '^TestRhiza(ObjectStoreFullLossRecovery|RestoreIfMissingRejectsMissingRemoteArchive|BackupAndRestoreRejectReadOnlyBeforeOpeningPath)$' "${provider_env} YEOULD_TEST_BINARY=$run_dir/yeould"
	run_case legacy '^TestRhiza020ArchiveCompatibility$' "${provider_env} YEOUL_RECOVERY_LEGACY=1 YEOUL_LEGACY_FIXTURE_HELPER=$repo_root/scripts/rhiza-legacy-fixture"
}

run_case() {
	phase=$1
	pattern=$2
	env_values=$3
	log="$run_dir/$provider-$phase.log"
	# Values are fixed harness assignments above; word splitting passes them as env tokens.
	set -- $env_values
	env GOWORK=off "$@" go test ./pkg/yeoul -run "$pattern" -count=1 -v >"$log" 2>&1
	result=$?
	cat "$log"
	results="$results $provider-$phase=$result"
	if [ "$result" -ne 0 ]; then failed=1; fi
}

set +e
run_provider s3 http://127.0.0.1:4566
run_provider gcs http://127.0.0.1:4588
run_provider azure http://127.0.0.1:4577
set -e
printf 'Provider results:%s\n' "$results"
exit "$failed"
