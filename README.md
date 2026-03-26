# Chrome Pilot

Control your existing Chrome browser from Claude Code — preserving login sessions, cookies, and all configurations.

[中文文档](README_CN.md)

## Why?

Existing browser MCP tools (e.g., Playwright MCP) launch a **new** Chrome instance, which means:
- Login sessions are lost — internal systems (Jira, Sentry, admin panels) require re-authentication every time
- Cookies, extensions, and settings are gone
- Pages behind VPN/SSO are inaccessible

Chrome Pilot connects directly to your **running** Chrome via a Chrome Extension. Everything stays as-is.

## Architecture

```
Claude Code
  │ bash: chrome-pilot <command>
  ▼
Go CLI (Cobra)
  │ JSON-RPC 2.0 / Unix Socket
  ▼
Go Daemon (:9333)
  │ WebSocket (auto token auth)
  ▼
Chrome Extension (Manifest V3)
  ├─ offscreen.js  → persistent WebSocket connection
  ├─ background.js → Chrome API calls
  └─ content.js    → DOM operations / accessibility snapshots
```

## Install

### One-liner (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/HelloiOS2014/chrome_pilot/main/install.sh | bash
```

Auto-detects platform, downloads pre-built binaries from [GitHub Releases](https://github.com/HelloiOS2014/chrome_pilot/releases/latest), and guides you through setup. No Go required.

Options: `bash install.sh --version v0.2.0` | `bash install.sh --uninstall`

### Build from source (developers)

```bash
git clone git@github.com:HelloiOS2014/chrome_pilot.git
cd chrome_pilot
go build -o chrome-pilot .
sudo cp chrome-pilot /usr/local/bin/
cp -R skills/chrome-pilot ~/.claude/skills/
# Chrome → chrome://extensions → Developer mode → Load unpacked → select extension/ directory
```

## Quick Start

```bash
# 1. Start the daemon
chrome-pilot start

# 2. Load the extension in Chrome (auto-connects after first install)

# 3. Check connection
chrome-pilot status
# {"daemon":"running","pid":12345,"extension":"connected","ws_port":9333}

# 4. Take a page snapshot
chrome-pilot snapshot
# Returns page structure summary (landmarks, headings, interactable element count)

# 5. Explore and interact
chrome-pilot snapshot --ref e2          # expand a region
chrome-pilot dom click --ref e5         # click an element
chrome-pilot dom type --ref e3 --text "hello"  # type text

# 6. Stop
chrome-pilot stop
```

## Command Reference

### Connection

| Command | Description |
|---------|-------------|
| `start` | Start daemon (background) |
| `start --foreground` | Start daemon in foreground |
| `stop` | Stop daemon |
| `status` | Show daemon + extension connection status |

### Tabs

| Command | Description |
|---------|-------------|
| `tab list` | List all open tabs |
| `tab new <url>` | Open a new tab |
| `tab select <index>` | Switch to a tab |
| `tab close [index]` | Close a tab |

### Snapshot (layered retrieval)

| Command | Description |
|---------|-------------|
| `snapshot` | Capture snapshot, return summary |
| `snapshot --ref E1` | Expand subtree |
| `snapshot --ref E1 --depth 2` | Expand with depth limit |
| `snapshot --search "text"` | Search by text |
| `snapshot --role button` | Filter by ARIA role |
| `snapshot --interactable` | Show interactable elements only |
| `snapshot info` | Show snapshot cache status |
| `snapshot clear` | Clear snapshot cache |

### DOM Interaction (ref-based)

| Command | Description |
|---------|-------------|
| `dom click --ref E1 [--button left\|right] [--double]` | Click |
| `dom type --ref E1 --text "..." [--slowly] [--submit]` | Type text |
| `dom hover --ref E1` | Hover |
| `dom drag --start-ref E1 --end-ref E2` | Drag and drop |
| `dom key <key>` | Press key |
| `dom select --ref E1 --values "v1,v2"` | Select dropdown option |
| `dom fill --fields '<json>'` | Fill multiple form fields |
| `dom upload --paths "path1,path2"` | Upload files |
| `dom eval --js "() => ..." [--ref E1]` | Execute JavaScript |

### Page Operations

| Command | Description |
|---------|-------------|
| `page navigate <url>` | Navigate |
| `page back` | Go back |
| `page screenshot [--full] [--ref E1] [--file path]` | Screenshot |
| `page wait --text "..." / --time N` | Wait for condition |
| `page console [--level info]` | Console messages |
| `page network [--include-static]` | Network requests |
| `page content [--format html\|text]` | Get page content |
| `page resize --width W --height H` | Resize window |
| `page dialog --accept [--text "..."]` | Handle dialog |
| `page close` | Close page |

### Data

| Command | Description |
|---------|-------------|
| `cookie list [--domain X]` | List cookies |
| `cookie get --name X --domain X` | Get a cookie |

### Cleanup

| Command | Description |
|---------|-------------|
| `clean` | Clean all temp files |
| `clean --before 3d` | Clean files older than 3 days |
| `clean --dry-run` | Preview cleanup |

All page/DOM/snapshot commands support `--tab <tabID>` to target a specific tab.

## Core Workflow

```
snapshot (summary) → expand/search target area → interact → snapshot (verify)
```

1. `snapshot` returns a page structure summary, not the full content (avoids token explosion)
2. Use `--ref`/`--search`/`--role` to drill into the parts you need
3. Use ref identifiers (e.g., `e1`, `e5`) to interact with specific elements
4. Re-snapshot after operations to see incremental changes

## Chrome Extension

### Loading

1. Open `chrome://extensions`
2. Enable **Developer mode**
3. Click **Load unpacked**
4. Select the `~/.chrome-pilot/extension/` directory (or `extension/` if building from source)

### How It Works

- **offscreen.js** — Persistent WebSocket connection (solves MV3 Service Worker 30s sleep issue)
- **background.js** — Command routing, Chrome API calls (tabs/cookies/scripting)
- **content.js** — Injected into pages for DOM operations, accessibility snapshots, element refs via `data-cp-ref` attributes

Token auto-auth: Extension fetches token from `http://localhost:9333/token` automatically. No manual configuration needed.

## Design Document

Detailed architecture and decision log: [`docs/superpowers/specs/2026-03-25-chrome-pilot-design.md`](docs/superpowers/specs/2026-03-25-chrome-pilot-design.md)
