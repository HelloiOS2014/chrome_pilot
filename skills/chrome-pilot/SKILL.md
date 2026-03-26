---
name: chrome-pilot
description: Control the user's Chrome browser - navigate, snapshot, interact with elements, extract data. Preserves login state and configuration.
---

## Prerequisites

Before using any command, check connection:
```bash
chrome-pilot status
```

If daemon not running: `chrome-pilot start`
If extension not connected: prompt user to check Chrome extension.

## Core Workflow

```
snapshot (summary) → expand/search relevant area → operate (click/type) → snapshot (verify)
```

1. Take snapshot to see page structure (returns summary with landmarks + headings)
2. Expand specific areas using --ref or search by --search/--role
3. Use refs from snapshot to interact with elements
4. Re-snapshot after operations to verify results

## Command Reference

### Connection
| Command | Description |
|---------|-------------|
| `chrome-pilot start` | Start daemon (auto-starts if needed) |
| `chrome-pilot stop` | Stop daemon |
| `chrome-pilot status` | Show daemon + extension status |

### Tabs
| Command | Description |
|---------|-------------|
| `chrome-pilot tab list` | List all open tabs |
| `chrome-pilot tab new <url>` | Open new tab |
| `chrome-pilot tab select <index>` | Switch to tab |
| `chrome-pilot tab close [index]` | Close tab |

### Snapshot (Layered Retrieval)
| Command | Description |
|---------|-------------|
| `chrome-pilot snapshot` | Capture snapshot, return summary |
| `chrome-pilot snapshot --ref E1` | Expand subtree of ref E1 |
| `chrome-pilot snapshot --ref E1 --depth 2` | Expand with depth limit |
| `chrome-pilot snapshot --search "text"` | Search elements by text |
| `chrome-pilot snapshot --role button` | Filter by ARIA role |
| `chrome-pilot snapshot --interactable` | Show all interactive elements |
| `chrome-pilot snapshot info` | Show snapshot cache status |
| `chrome-pilot snapshot clear` | Clear snapshot cache |

### DOM Interaction (ref-based)
| Command | Description |
|---------|-------------|
| `chrome-pilot dom click --ref E1 [--button left] [--double]` | Click element |
| `chrome-pilot dom type --ref E1 --text "..." [--slowly] [--submit]` | Type text |
| `chrome-pilot dom hover --ref E1` | Hover over element |
| `chrome-pilot dom drag --start-ref E1 --end-ref E2` | Drag and drop |
| `chrome-pilot dom key <key>` | Press keyboard key |
| `chrome-pilot dom select --ref E1 --values "v1,v2"` | Select dropdown option |
| `chrome-pilot dom fill --fields '<json>'` | Fill multiple form fields |
| `chrome-pilot dom upload --paths "path1,path2"` | Upload files |
| `chrome-pilot dom eval --js "() => ..." [--ref E1]` | Execute JavaScript |

### Page Operations
| Command | Description |
|---------|-------------|
| `chrome-pilot page navigate <url>` | Navigate to URL |
| `chrome-pilot page back` | Go back |
| `chrome-pilot page screenshot [--full] [--ref E1] [--file path]` | Take screenshot |
| `chrome-pilot page wait --text "..." / --time N` | Wait for condition |
| `chrome-pilot page console [--level info]` | Console messages |
| `chrome-pilot page network [--include-static]` | Network requests |
| `chrome-pilot page content [--format html\|text]` | Get page content |
| `chrome-pilot page resize --width W --height H` | Resize window |
| `chrome-pilot page dialog --accept [--text "..."]` | Handle dialog |
| `chrome-pilot page close` | Close page |

### Data
| Command | Description |
|---------|-------------|
| `chrome-pilot cookie list [--domain X]` | List cookies |
| `chrome-pilot cookie get --name X --domain X` | Get cookie |

### Cleanup
| Command | Description |
|---------|-------------|
| `chrome-pilot clean` | Clean all temp files |
| `chrome-pilot clean --before 3d` | Clean files older than 3 days |
| `chrome-pilot clean --dry-run` | Preview cleanup |

All page/DOM/snapshot commands support `--tab <tabID>` to target a specific tab.

## Error Handling

| Error | Cause | Action |
|-------|-------|--------|
| `extension not connected` | Extension not installed/enabled | Prompt user to check Chrome extension |
| `ref E5 not found` | Ref expired (page changed) | Re-run `snapshot` |
| `element not visible` | Element hidden/obscured | Try scroll or wait |
| `timeout` | Operation timed out (10s) | Check page loading state |
| `tab not found` | Tab was closed | Run `tab list` for current state |
| `cannot inject into chrome:// pages` | Protected page | Prompt user to navigate manually |

## Typical Scenarios

### Extract data from internal system
```bash
chrome-pilot page navigate "https://internal.company.com/dashboard"
chrome-pilot page wait --text "Dashboard"
chrome-pilot snapshot
chrome-pilot snapshot --search "table"
chrome-pilot dom eval --js "() => {
  const rows = document.querySelectorAll('table tbody tr');
  return Array.from(rows).map(r => ({
    name: r.cells[0].textContent,
    value: r.cells[1].textContent
  }));
}"
```

### Fill and submit a form
```bash
chrome-pilot snapshot
chrome-pilot dom type --ref e3 --text "John Doe"
chrome-pilot dom type --ref e4 --text "john@example.com"
chrome-pilot dom click --ref e7
chrome-pilot page wait --text "Success"
```
