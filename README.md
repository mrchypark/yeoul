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

## Local Development

Requirements: Go 1.27+.

LatticeDB is the only storage engine used by current releases.

Then build, vet, and test normally:

```bash
go build ./...
go vet ./...
go test ./...
```

Yeoul search runs fully in-process over LatticeDB and requires no external
retrieval runtime or native retrieval library.

## Existing Databases

Current releases use LatticeDB only and do not read or migrate native Ladybug
databases. If you have a genuine, unmigrated Ladybug database, use a compatible
older Yeoul release to migrate it before upgrading. Keep the timestamped
`.ladybug-backup-*` copy until counts, search, revisions, and lifecycle state
are verified. Never initialize or force-create a database at a path that
contains existing data.

The path extension does not identify the database format: a migrated LatticeDB
directory may still be named with a `.lbug` suffix. Do not migrate or rename an
already-migrated database just because of its extension.

## Separation Rule

```text
Core는 AI를 모른다.
Agent Pack은 Core를 사용하는 규칙만 제공한다.
```
