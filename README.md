# Yeoul

Yeoul (/jʌ.ul/, 여울) is a local-first Temporal Graph Memory Engine written in Go, backed by Ladybug for all durable on-disk storage, and designed to keep AI agent behavior outside the core through external skills, instructions, ontology files, episode rules, and search recipes.

한국어 요약:

여울은 Go와 Ladybug로 구현하는 로컬 우선 Temporal Graph Memory Engine이다. durable on-disk 저장소는 Ladybug만 사용하며, Core는 AI agent 로직을 포함하지 않고 agent 전용 행동은 skill, instruction, ontology, episode rule, search recipe 파일로 외부화한다.

## 왜 Yeoul인가

프로젝트 이름을 `여울`로 지은 이유는, 여울이 물이 그냥 흘러가 버리는 구간이 아니라 지형을 따라 흐름이 또렷해지고 흔적이 드러나는 구간이기 때문이다. Yeoul도 마찬가지로 대화, 사건, 결정, 수정 같은 시간 위의 흐름을 그냥 흘려보내지 않고, provenance와 함께 구조화된 memory로 남기는 엔진을 지향한다.

## Documentation

- Core and product documentation lives under [`docs/`](./docs).
- Agent usage guidance and starter policy pack live under [`agent-pack/`](./agent-pack).

## Installation

Release artifacts are published for macOS, Linux, and Windows.
`install.sh` and `install.ps1` are uploaded as GitHub Release assets, so you can execute them directly from the release URL without checking out the repository.
The installer downloads the matching archive and checksum from the same release, verifies it, and installs Yeoul under the default per-user location.
On macOS and Linux that is `~/.local/share/yeoul/<tag>` with wrapper commands in `~/.local/bin`. On Windows that is `%LOCALAPPDATA%\\Programs\\yeoul\\<tag>`, and the script adds its `bin` directory to the user `PATH`.

Latest release on macOS and Linux:

```bash
curl -fsSL https://github.com/mrchypark/yeoul/releases/latest/download/install.sh | bash
```

Specific version on macOS and Linux:

```bash
curl -fsSL https://github.com/mrchypark/yeoul/releases/download/v0.1.0/install.sh | bash
```

If you want the latest installer script but a specific Yeoul version:

```bash
curl -fsSL https://github.com/mrchypark/yeoul/releases/latest/download/install.sh | YEOUL_VERSION=v0.1.0 bash
```

Latest release on Windows PowerShell:

```powershell
irm https://github.com/mrchypark/yeoul/releases/latest/download/install.ps1 | iex
```

Specific version on Windows PowerShell:

```powershell
irm https://github.com/mrchypark/yeoul/releases/download/v0.1.0/install.ps1 | iex
```

If you want the latest installer script but a specific Yeoul version:

```powershell
$env:YEOUL_VERSION = "v0.1.0"
irm https://github.com/mrchypark/yeoul/releases/latest/download/install.ps1 | iex
```

Windows builds currently target `x64` and still require the Microsoft Visual C++ 2015-2022 Redistributable because Ladybug ships as a native shared library.

Homebrew:

```bash
brew tap mrchypark/tap
brew install yeoul
```

## Separation Rule

```text
Core는 AI를 모른다.
Agent Pack은 Core를 사용하는 규칙만 제공한다.
```
