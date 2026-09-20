#!/usr/bin/env sh

# The version-pinned migration helper is built from the current source tree with
# a separate module file (go.legacy-v0131.mod), because it must link the
# Ladybug v0.13.1 reader instead of the current engine. Any new import in
# pkg/yeoul that the pinned module file does not require breaks only that build,
# so it is checked here for every release target rather than discovered during
# packaging.

set -eu

module_file="go.legacy-v0131.mod"

if [ ! -f "${module_file}" ]; then
  printf 'legacy helper module file %s is missing\n' "${module_file}" >&2
  exit 1
fi

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  target_os=${target%%/*}
  target_arch=${target##*/}
  if ! GOWORK=off GOOS="${target_os}" GOARCH="${target_arch}" \
    go list -modfile="${module_file}" -deps ./cmd/yeoul >/dev/null 2>&1; then
    printf 'legacy helper module cannot resolve %s dependencies; run:\n' "${target}" >&2
    printf '  GOWORK=off GOOS=%s GOARCH=%s go list -modfile=%s -deps ./cmd/yeoul\n' \
      "${target_os}" "${target_arch}" "${module_file}" >&2
    exit 1
  fi
done

printf 'legacy helper module resolves every release target\n'
