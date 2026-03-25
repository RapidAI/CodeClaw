# Effective Code Statistics Report

## Scope

- Scope: tracked source files from `git ls-files`
- Excluded: common docs, assets, build artifacts, caches, and generated-code paths such as `.git`, `build`, `dist`, `docs`, `data`, `vendor`, `wailsjs`, and `intermediates`
- Effective LOC: non-empty, non-comment lines only
- Generated on: `2026-03-24`

## Overview

- Source files counted: `851`
- Effective LOC counted: `176,800`

## By Code Kind

| Code Kind | Files | Effective LOC |
|---|---|---|
| Production | 703 | 148,380 |
| Test | 148 | 28,420 |

## By Business Category

| Category | Files | Effective LOC |
|---|---|---|
| Backend/Core | 687 | 142,457 |
| Frontend | 70 | 24,136 |
| Native/Engine | 32 | 7,124 |
| Tools/Scripts | 22 | 2,025 |
| Mobile | 40 | 1,058 |

## Business Category x Code Kind

| Category | Code Kind | Files | Effective LOC |
|---|---|---|---|
| Backend/Core | Production | 543 | 114,374 |
| Backend/Core | Test | 144 | 28,083 |
| Frontend | Production | 67 | 23,882 |
| Frontend | Test | 3 | 254 |
| Mobile | Production | 40 | 1,058 |
| Native/Engine | Production | 31 | 7,041 |
| Native/Engine | Test | 1 | 83 |
| Tools/Scripts | Production | 22 | 2,025 |

## By Language

| Language | Files | Effective LOC |
|---|---|---|
| Go | 645 | 138,636 |
| TypeScript/TSX | 35 | 17,607 |
| TypeScript | 50 | 5,394 |
| C++ | 14 | 3,265 |
| HTML | 11 | 3,161 |
| C | 1 | 2,475 |
| Batch | 18 | 1,242 |
| CSS | 2 | 1,041 |
| C/C Header | 15 | 810 |
| JavaScript | 11 | 740 |
| Python | 4 | 608 |
| Shell | 7 | 563 |
| Objective-C | 2 | 465 |
| XML | 18 | 262 |
| YAML | 6 | 172 |
| Kotlin | 3 | 156 |
| PowerShell | 5 | 112 |
| Swift | 4 | 91 |

## By Top-Level Directory

| Directory | Files | Effective LOC |
|---|---|---|
| gui | 306 | 87,078 |
| hub | 161 | 31,717 |
| corelib | 118 | 17,034 |
| hubcenter | 95 | 15,096 |
| tui | 51 | 11,718 |
| RapidSpeech.cpp | 32 | 7,124 |
| tmp-weixin | 33 | 3,437 |
| mobile | 43 | 1,812 |
| (root) | 5 | 1,056 |
| openclaw-bridge | 6 | 370 |
| cmd | 1 | 358 |

## Largest Files Top 20

| File | Language | Effective LOC |
|---|---|---|
| `gui/frontend/src/App.tsx` | TypeScript/TSX | 6,537 |
| `gui/app.go` | Go | 4,894 |
| `gui/im_message_handler.go` | Go | 3,961 |
| `RapidSpeech.cpp/rapidspeech/src/frontend/fftsg.c` | C | 2,475 |
| `gui/platform_windows.go` | Go | 1,600 |
| `gui/android_pwa_shell.go` | Go | 1,417 |
| `hub/internal/store/sqlite/repositories_stub.go` | Go | 1,408 |
| `gui/remote_session_manager.go` | Go | 1,271 |
| `tui/commands/clawnet.go` | Go | 1,214 |
| `gui/remote_hub_client.go` | Go | 1,202 |
| `hub/internal/ws/handlers_machine.go` | Go | 1,179 |
| `tui/agent_tools.go` | Go | 1,178 |
| `gui/app_clawnet.go` | Go | 1,132 |
| `corelib/weixin/gateway.go` | Go | 1,117 |
| `hub/internal/qqbot/plugin.go` | Go | 1,115 |
| `gui/frontend/src/components/remote/SkillsManagementPanel.tsx` | TypeScript/TSX | 1,087 |
| `hub/internal/feishu/notifier.go` | Go | 1,041 |
| `gui/frontend/src/App.css` | CSS | 1,002 |
| `hub/internal/feishu/webhook.go` | Go | 1,000 |
| `gui/internal/systray/systray_windows.go` | Go | 948 |
