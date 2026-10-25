# Yeoul

Yeoul (/jʌ.ul/, 여울) is a local-first Temporal Graph Memory Engine written in Go, backed by embedded Rhiza for new durable databases and by LatticeDB for existing databases, and designed to keep AI agent behavior outside the core through external skills, instructions, ontology files, episode rules, and search recipes.

한국어 요약:

여울은 Go와 Rhiza/LatticeDB로 구현하는 로컬 우선 Temporal Graph Memory Engine이다. 새 데이터베이스는 embedded Rhiza를 사용하고 기존 LatticeDB 데이터베이스는 계속 자동으로 인식한다. Core는 AI agent 로직을 포함하지 않고 agent 전용 행동은 skill, instruction, ontology, episode rule, search recipe 파일로 외부화한다.

## 왜 Yeoul인가

프로젝트 이름을 `여울`로 지은 이유는, 여울이 물이 그냥 흘러가 버리는 구간이 아니라 지형을 따라 흐름이 또렷해지고 흔적이 드러나는 구간이기 때문이다. Yeoul도 마찬가지로 대화, 사건, 결정, 수정 같은 시간 위의 흐름을 그냥 흘려보내지 않고, provenance와 함께 구조화된 memory로 남기는 엔진을 지향한다.

## Documentation

- Core and product documentation lives under [`docs/`](./docs).
- Agent usage guidance and starter policy pack live under [`agent-pack/`](./agent-pack).
- Product-specific registration guides for Codex, Gemini CLI, and Claude Code live under [`agent-pack/integrations/`](./agent-pack/integrations).

## Installation

Release artifacts are published for macOS, Linux, and Windows.
`install.sh` and `install.ps1` are uploaded as GitHub Release assets, so you can execute them directly from the release URL without checking out the repository.
The installer downloads the matching archive and checksum from the same release, verifies the checksum with `sha256sum` or `shasum`, and installs Yeoul under the default per-user location.
Checksum verification is mandatory: when neither tool is available the installer stops before it changes an existing installation.
On macOS and Linux that is `~/.local/share/yeoul/<tag>` with wrapper commands in `~/.local/bin`. On Windows that is `%LOCALAPPDATA%\\Programs\\yeoul\\<tag>`, and the script puts that `bin` directory first on the user `PATH`, ahead of the directories of earlier versions, so a new shell runs the version you just installed.

Latest release on macOS and Linux:

```bash
curl -fsSL https://github.com/mrchypark/yeoul/releases/latest/download/install.sh | bash
```

Specific version on macOS and Linux:

```bash
curl -fsSL https://github.com/mrchypark/yeoul/releases/latest/download/install.sh | YEOUL_VERSION=v0.5.5 bash
```

Latest release on Windows PowerShell:

```powershell
irm https://github.com/mrchypark/yeoul/releases/latest/download/install.ps1 | iex
```

Specific version on Windows PowerShell:

```powershell
$env:YEOUL_VERSION = "v0.5.5"
irm https://github.com/mrchypark/yeoul/releases/latest/download/install.ps1 | iex
```

A version-pinned script URL does not pin the binary version, because each installer resolves `latest` from the release metadata when no version is given.
Pass the version explicitly for a pinned installation, including when the script itself comes from that release URL:

```bash
curl -fsSL https://github.com/mrchypark/yeoul/releases/download/v0.1.0/install.sh | YEOUL_VERSION=v0.1.0 bash
```

```powershell
$env:YEOUL_VERSION = "v0.1.0"
irm https://github.com/mrchypark/yeoul/releases/download/v0.1.0/install.ps1 | iex
```

Both installers validate the archive layout promised by the selected release.
An explicit version always wins over the `latest` release metadata.

Windows builds currently target `x64`.

Homebrew:

```bash
brew tap mrchypark/tap
brew install yeoul
```

## Agent Setup

Yeoul keeps agent behavior outside the core engine, so each coding assistant needs its own registration step.

- Codex: use repository `AGENTS.md` and, optionally, install the reusable skill in the actual directory the active host loads for `yeoul-memory`; see [`agent-pack/integrations/codex/install.md`](./agent-pack/integrations/codex/install.md)
- Gemini CLI: use repository `GEMINI.md` and optional `.gemini/commands/*.toml`
- Claude Code: use repository `CLAUDE.md` and optional `.claude/commands/*.md`

See the full product-specific guide in [`agent-pack/integrations/README.md`](./agent-pack/integrations/README.md).

For the optional `yeould` HTTP service container, see
[`Container Image`](docs/08-operations/containers.md).

## Local Development

Requirements: Go 1.27.3+.

Released Yeoul v0.10.x databases use LatticeDB. This source version defaults new
databases to embedded Rhiza in local-only mode on Linux and macOS, and continues
to open existing LatticeDB directories by their on-disk signatures. On other
platforms new databases fall back to Lattice; existing Rhiza paths fail with
`ErrNotSupported` without initialization or modification. The pinned Rhiza
package's qlog lock uses Unix-only `syscall.Flock`, so Yeoul isolates the native
dependency and disables Rhiza storage on Windows until it is portable. Rhiza is
not configured as a cluster, and Yeoul does not support mixed-version clustered
deployments.

Then build, vet, and test normally:

```bash
go build ./...
go vet ./...
go test ./...
```

Yeoul's hybrid, keyword, and semantic search modes run in-process. The opt-in
`fulltext` mode uses the selected storage backend's native text predicate.
LatticeDB uses its persistent standard-token FTS indexes; Rhiza uses the
public graph `@@` predicate and reports backend ranking as a Yeoul score, not
LatticeDB term-frequency scoring. Both differ from substring keyword search.
See [Local Storage](docs/08-operations/local-storage.md) for limits and migration.

LatticeDB v0.12.0 warns that writing vectors converts the previous HNSW format,
which is unreadable by v0.11.x. Yeoul does not write vectors as part of this
upgrade or its full-text indexing path; keep a backup before any future vector
write using v0.12.0.

## Existing Databases

Released binaries use LatticeDB and do not read or migrate native Ladybug
databases. If you have a genuine, unmigrated Ladybug database, use a compatible
older Yeoul release to migrate it before upgrading. Keep the timestamped
`.ladybug-backup-*` copy until counts, search, revisions, and lifecycle state
are verified. Never initialize or force-create a database at a path that
contains existing data.

The path extension does not identify the database format: an existing LatticeDB
directory may still be named with a `.lbug` suffix. Do not migrate or rename a
database just because of its extension. To copy LatticeDB Yeoul state into a
new Rhiza directory, use the explicit `yeoul migrate-storage` command described
in [Local Storage](docs/08-operations/local-storage.md); the source is left
unchanged and the target must be a new path.
The migration procedure and its platform/size limits are documented in
[Storage Migration](docs/08-operations/storage-migration.md).

## Separation Rule

```text
Core는 AI를 모른다.
Agent Pack은 Core를 사용하는 규칙만 제공한다.
```
