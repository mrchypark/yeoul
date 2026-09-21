# Yeoul

Yeoul (/jʌ.ul/, 여울) is a local-first Temporal Graph Memory Engine written in Go, backed by LatticeDB for durable on-disk storage, and designed to keep AI agent behavior outside the core through external skills, instructions, ontology files, episode rules, and search recipes.

한국어 요약:

여울은 Go와 LatticeDB로 구현하는 로컬 우선 Temporal Graph Memory Engine이다. durable on-disk 저장소는 LatticeDB를 기본으로 사용하며, Core는 AI agent 로직을 포함하지 않고 agent 전용 행동은 skill, instruction, ontology, episode rule, search recipe 파일로 외부화한다.

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
Releases from `v0.5.1` on must ship the version-pinned Ladybug migration helper; earlier tags ship only the binaries, and they install without one.
An explicit version always wins over the `latest` release metadata.

Windows builds currently target `x64`. Release archives include a version-pinned
Ladybug migration helper and runtime; they are used only when converting an
existing database.

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

## Local Development

Requirements: Go 1.27+ and the `gcc` toolchain (cgo for the legacy migration reader).

LatticeDB is the default storage engine. The Ladybug runtime is retained only
to build and test automatic migration from legacy databases, and must be staged
into the Go module cache once per machine:

```bash
bash scripts/ci/setup-ladybug.sh darwin arm64   # darwin arm64|amd64, linux amd64|arm64, windows amd64
```

`setup-ladybug.sh` and the release `stage-runtime.sh` verify each downloaded
native archive against the committed SHA-256 pins in
`scripts/ci/runtime-digests.txt` before extracting or staging it. An asset with
no pin, or bytes that do not match its pin, is refused, so rebuilding the same
Yeoul source cannot silently accept a replaced upstream binary. Add or update a
pin only when intentionally moving to a new upstream release.

Then build, vet, and test normally:

```bash
go build ./...
go vet ./...
go test ./...
```

Yeoul search runs fully in-process over LatticeDB and requires no external
retrieval runtime or native retrieval library.

## Database Migration

A writable open of an existing Ladybug database with the default driver
automatically converts it to LatticeDB. Yeoul writes and verifies a staging
database first, keeps the original as a timestamped `.ladybug-backup-*`
sibling, and then installs the verified Lattice database at the original path.
New databases use the standard `.ltdb` extension. An existing `.lbug` path
remains valid after in-place migration for backward compatibility.

A read-only open is a no-mutation open. It never converts a legacy database:
when the default driver cannot read the database it reports
`YEOUL_NOT_SUPPORTED: database requires migration before it can be opened
read-only` and leaves every source file and format unchanged. Inspection and
backup callers therefore cannot trigger a conversion by accident. To convert a
database from a read-only caller, run `yeoul admin migrate-db` explicitly, or
open through the embedded API with `Config.AllowMigration` set.
Yeoul v0.5.2 and later use the bundled Ladybug v0.13.1 helper for databases
created by Yeoul v0.2.2. Migration fails without modifying the source database
when that helper or its matching runtime is unavailable. Do not use Yeoul
v0.5.0 or v0.5.1 to migrate a pristine v0.2.2 database; v0.5.1 does not isolate
the helper runtime from Linux Homebrew's inherited library path.
Stop other Yeoul processes before migrating a database; migration requires
exclusive ownership of the database path.

Run the same operation explicitly with:

```bash
yeoul admin migrate-db --db "$HOME/.local/share/yeoul/work-memory.lbug" --json
```

After verification and while all Yeoul processes are stopped, the migrated
directory may be renamed to `work-memory.ltdb`. Update `YEOUL_DB` at the same
time; never initialize a new empty `.ltdb` database while the legacy path still
contains the authoritative data.

## Separation Rule

```text
Core는 AI를 모른다.
Agent Pack은 Core를 사용하는 규칙만 제공한다.
```
