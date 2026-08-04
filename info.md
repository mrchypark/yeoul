# Disk cleanup audit

- last_full_scan: Sun Jul 5 11:52:28 KST 2026
- os: macOS 26.2 25C56
- disk_summary: `/System/Volumes/Data` 460Gi total, 420Gi used, 1.3Gi available, 100% capacity
- method: read-only `df`, `du`, `find`, `ls`, `tmutil`; no cleanup executed
- cleanup_executed: Sun Jul 5 2026; user-requested deletion of item 3 (`~/.colima`) and item 5 development caches/build artifacts; after cleanup `/System/Volumes/Data` 383Gi used, 39Gi available, 91% capacity

## /System/Volumes/Data/private/var/folders/r4/bygqtzg554v3dh6zd8z41zgw0000gn/X/com.google.Chrome.code_sign_clone
- size: 31G
- classification: safe
- purpose: Chrome helper temporary code-sign clone data under macOS per-user temp/cache area.
- owner_or_related_app: Google Chrome
- created_by: Chrome/macOS temporary execution support
- current_usage: likely temporary; only risky while Chrome or helpers are running
- risk_if_removed: Chrome may need restart; deleting while Chrome is active can break current sessions
- evidence: path under `/private/var/folders/.../X`; `du` showed 31G; name is `com.google.Chrome.code_sign_clone`
- local_docs_or_code_checked: none
- web_sources_checked: none
- cleanup_advice: quit Chrome first; prefer reboot before manual removal
- verify_commands: `du -sh "/System/Volumes/Data/private/var/folders/r4/bygqtzg554v3dh6zd8z41zgw0000gn/X/com.google.Chrome.code_sign_clone"`
- suggested_manual_commands: `mv "/System/Volumes/Data/private/var/folders/r4/bygqtzg554v3dh6zd8z41zgw0000gn/X/com.google.Chrome.code_sign_clone" ~/.Trash/`
- last_checked: Sun Jul 5 2026
- confidence: high

## /System/Volumes/Data/private/var/folders/r4/bygqtzg554v3dh6zd8z41zgw0000gn/T/patch-db-service-sync.patch-data.*
- size: about 3.5G
- classification: caution
- purpose: temporary patch DB sync working directories.
- owner_or_related_app: local patch/db service tooling
- created_by: patch-db-service-sync process
- current_usage: unknown; files dated Jul 2 and include `data.db` / `data.db.tmp`
- risk_if_removed: active sync work could be lost if still running
- evidence: `find` found several 600M-1.0G DB temp files in `/private/var/folders/.../T`
- local_docs_or_code_checked: none
- web_sources_checked: none
- cleanup_advice: confirm no related process is running, then move exact old temp directories to Trash
- verify_commands: `du -sh "/System/Volumes/Data/private/var/folders/r4/bygqtzg554v3dh6zd8z41zgw0000gn/T/patch-db-service-sync.patch-data."*`
- suggested_manual_commands: `mv "/System/Volumes/Data/private/var/folders/r4/bygqtzg554v3dh6zd8z41zgw0000gn/T/patch-db-service-sync.patch-data.p5BEMT" ~/.Trash/`
- last_checked: Sun Jul 5 2026
- confidence: medium

## /Users/cypark/Documents/work/enops
- size: 178G
- classification: caution
- purpose: large local ENOPS work dataset and runtime outputs.
- owner_or_related_app: ENOPS local work
- created_by: local project workflows
- current_usage: dominant user data; `ready` 159G, `db` 12G, `analysis` 7.2G
- risk_if_removed: likely project data loss
- evidence: `du -d 2` under `Documents/work` showed `enops/ready`, `enops/db`, `enops/analysis`
- local_docs_or_code_checked: directory structure only
- web_sources_checked: none
- cleanup_advice: do not bulk-delete; archive or remove only known obsolete run outputs
- verify_commands: `du -sh "/Users/cypark/Documents/work/enops/ready" "/Users/cypark/Documents/work/enops/db" "/Users/cypark/Documents/work/enops/analysis"`
- suggested_manual_commands: `mv "/Users/cypark/Documents/work/enops/ready" ~/.Trash/`
- last_checked: Sun Jul 5 2026
- confidence: medium

## /Users/cypark/Library/Application Support/enops-monitor
- size: 21G
- classification: caution
- purpose: ENOPS monitor runtime, ready data, DB backups, reports, logs.
- owner_or_related_app: enops-monitor
- created_by: local monitor automation
- current_usage: active; files updated Jul 5 12:00
- risk_if_removed: monitor state, DB backups, ready data, and reports may be lost
- evidence: `runtime/ready` 16G, `runtime/db` 4.1G, `reports` 487M; logs and state updated today
- local_docs_or_code_checked: `ls` and `du` of runtime directories
- web_sources_checked: none
- cleanup_advice: clean only app-known old ready/report/backup data; do not delete whole directory while monitors run
- verify_commands: `du -sh "/Users/cypark/Library/Application Support/enops-monitor/runtime/ready" "/Users/cypark/Library/Application Support/enops-monitor/runtime/db/service/patch-data/backups"`
- suggested_manual_commands: `mv "/Users/cypark/Library/Application Support/enops-monitor/runtime/db/service/patch-data/backups" ~/.Trash/`
- last_checked: Sun Jul 5 2026
- confidence: high

## /Users/cypark/.colima
- size: 23G
- classification: caution
- purpose: Colima/Lima VM disk image and container runtime state.
- owner_or_related_app: Colima, Lima, Docker-compatible containers
- created_by: Colima
- current_usage: `_lima/_disks` is 21G
- risk_if_removed: deletes local containers, images, volumes, and VM state
- evidence: `du -d 2` showed `_lima/_disks` 21G
- local_docs_or_code_checked: directory structure only
- web_sources_checked: none
- cleanup_advice: inspect Docker/Colima usage first; remove only if containers/images are disposable
- verify_commands: `colima status; docker system df; du -sh "$HOME/.colima"`
- suggested_manual_commands: `colima stop && mv "$HOME/.colima" ~/.Trash/`
- last_checked: Sun Jul 5 2026
- confidence: high

## /Users/cypark/Library/Caches
- size: 2.8G
- classification: safe
- purpose: user-level application and build caches.
- owner_or_related_app: Go, Google, Codex, Colima, R, macOS apps
- created_by: installed tools and apps
- current_usage: rebuildable cache; largest are `go-build` 1.5G, Google 407M, Codex 375M, colima 258M
- risk_if_removed: apps/builds may be slower until cache is rebuilt
- evidence: `du -d 1` under `~/Library/Caches`
- local_docs_or_code_checked: none
- web_sources_checked: none
- cleanup_advice: good first cleanup target
- verify_commands: `du -sh "$HOME/Library/Caches/go-build" "$HOME/Library/Caches/Codex" "$HOME/Library/Caches/Google" "$HOME/Library/Caches/colima"`
- suggested_manual_commands: `rm -rf "$HOME/Library/Caches/go-build" "$HOME/Library/Caches/Codex" "$HOME/Library/Caches/Google" "$HOME/Library/Caches/colima"`
- last_checked: Sun Jul 5 2026
- confidence: high

## /Users/cypark/.codex
- size: 13G
- classification: caution
- purpose: Codex sessions, monitors, plugins, worktrees, temp files.
- owner_or_related_app: Codex
- created_by: Codex desktop/CLI
- current_usage: active agent history and monitors; `.tmp` 263M and `tmp` 41M are low-risk, sessions/monitors are history/state
- risk_if_removed: loss of session history, monitor state, worktrees, and plugin cache
- evidence: `monitors` 4.1G, `sessions` 3.8G, `archived_sessions` 1.3G, `sqlite` 772M
- local_docs_or_code_checked: directory structure only
- web_sources_checked: none
- cleanup_advice: remove temp files first; archive/delete sessions only if history is not needed
- verify_commands: `du -sh "$HOME/.codex/.tmp" "$HOME/.codex/tmp" "$HOME/.codex/sessions" "$HOME/.codex/archived_sessions"`
- suggested_manual_commands: `rm -rf "$HOME/.codex/.tmp" "$HOME/.codex/tmp"`
- last_checked: Sun Jul 5 2026
- confidence: high

## /Users/cypark/Documents/work build artifacts
- size: about 11G
- classification: safe
- purpose: rebuildable Rust/Node/Python project build dependencies and outputs.
- owner_or_related_app: local development projects
- created_by: cargo, npm, virtualenv, app builds
- current_usage: build artifacts: `compos/target` 5.3G, `solarsim/.../target` 3.2G, several `node_modules` folders
- risk_if_removed: projects need reinstall/rebuild
- evidence: `find` for `target`, `node_modules`, `.venv`, `build`, `dist`
- local_docs_or_code_checked: directory names only
- web_sources_checked: none
- cleanup_advice: clean per project when not actively building
- verify_commands: `find "$HOME/Documents/work" -xdev \( -name node_modules -o -name target -o -name .venv \) -type d -prune -exec du -sh {} \;`
- suggested_manual_commands: `rm -rf "$HOME/Documents/work/compos/target" "$HOME/Documents/work/solarsim/apps/solarsim-desktop/src-tauri/target"`
- last_checked: Sun Jul 5 2026
- confidence: high

## /Users/cypark/Downloads
- size: 5.7G
- classification: caution
- purpose: downloaded documents, archives, DMGs, media, and project folders.
- owner_or_related_app: browser/user downloads
- created_by: user and browser downloads
- current_usage: mixed user files
- risk_if_removed: user data loss if files are still needed
- evidence: top entries include old DMGs, ZIPs, media, and project-like directories
- local_docs_or_code_checked: directory listing by size only
- web_sources_checked: none
- cleanup_advice: move old installers/archives to Trash after review
- verify_commands: `du -sh "$HOME/Downloads"; find "$HOME/Downloads" -maxdepth 1 -type f -size +100M -ls`
- suggested_manual_commands: `mv "$HOME/Downloads/Positron-2026.02.1-5-arm64.dmg" "$HOME/Downloads/Codex.dmg" ~/.Trash/`
- last_checked: Sun Jul 5 2026
- confidence: medium

## /Users/cypark/Library/Android/sdk
- size: 4.2G
- classification: caution
- purpose: Android SDK, NDK, build tools, platforms, sources.
- owner_or_related_app: Android Studio / Android tooling
- created_by: Android SDK manager
- current_usage: `ndk` is 2.8G; build-tools 565M; platforms 333M
- risk_if_removed: Android builds can fail until components are reinstalled
- evidence: `du -d 2` under `~/Library/Android`
- local_docs_or_code_checked: directory structure only
- web_sources_checked: none
- cleanup_advice: remove unused SDK/NDK versions through Android Studio SDK Manager
- verify_commands: `du -sh "$HOME/Library/Android/sdk/ndk" "$HOME/Library/Android/sdk/build-tools" "$HOME/Library/Android/sdk/platforms"`
- suggested_manual_commands: `open -a "Android Studio"`
- last_checked: Sun Jul 5 2026
- confidence: high

## /opt/homebrew
- size: 20G
- classification: caution
- purpose: Homebrew packages, metadata, cache-adjacent share data.
- owner_or_related_app: Homebrew
- created_by: Homebrew
- current_usage: package installation tree; Cellar 13G
- risk_if_removed: installed tools break
- evidence: largest formulae include llvm, llvm@20, mingw-w64, qemu, gradle, azure-cli
- local_docs_or_code_checked: `du` only
- web_sources_checked: none
- cleanup_advice: use Homebrew dry-run first; uninstall unused packages intentionally
- verify_commands: `brew cleanup -n; brew leaves`
- suggested_manual_commands: `brew cleanup`
- last_checked: Sun Jul 5 2026
- confidence: high

## /Users/cypark/Documents/New project/azure-gcp-storage-audit/run-20260701-060143-inventory-compare
- size: 4.1G
- classification: caution
- purpose: one dated cloud storage inventory compare run output.
- owner_or_related_app: local audit workflow
- created_by: azure/gcp storage audit scripts
- current_usage: dated run output under `New project`
- risk_if_removed: audit evidence for that run is lost
- evidence: `du -d 2` under `Documents/New project`
- local_docs_or_code_checked: directory structure only
- web_sources_checked: none
- cleanup_advice: archive or delete if the inventory compare output is no longer needed
- verify_commands: `du -sh "$HOME/Documents/New project/azure-gcp-storage-audit/run-20260701-060143-inventory-compare"`
- suggested_manual_commands: `mv "$HOME/Documents/New project/azure-gcp-storage-audit/run-20260701-060143-inventory-compare" ~/.Trash/`
- last_checked: Sun Jul 5 2026
- confidence: medium

## APFS local snapshots
- size: unknown
- classification: caution
- purpose: macOS update snapshots.
- owner_or_related_app: macOS
- created_by: Software Update / Time Machine snapshot system
- current_usage: snapshots listed: two `com.apple.os.update-*` and `com.apple.os.update-MSUPrepareUpdate`
- risk_if_removed: manually deleting update snapshots can be risky; let macOS manage or use supported tools
- evidence: `tmutil listlocalsnapshots /`
- local_docs_or_code_checked: none
- web_sources_checked: none
- cleanup_advice: mention as possible hidden disk usage; do not manually remove unless necessary
- verify_commands: `tmutil listlocalsnapshots /`
- suggested_manual_commands: none
- last_checked: Sun Jul 5 2026
- confidence: medium
