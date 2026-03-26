# Chrome Pilot 设计文档

> 一个让 Claude Code 操控用户现有 Chrome 浏览器的工具，复用登录态和配置。

## 1. 问题与动机

现有的浏览器 MCP 工具（如 Playwright MCP）都会启动新的 Chrome 实例，导致登录态、Cookie、扩展、配置全部丢失。对于需要操作内部系统（Jira、Sentry、运维平台等）的场景，每次都要重新登录，不可用。

Chrome Pilot 通过 Chrome Extension 直接连接用户正在使用的 Chrome，保留所有已有状态。

## 2. 架构

```
Claude Code
  | bash: chrome-pilot <cmd>
Go CLI (Cobra)
  | JSON-RPC 2.0 over Unix Socket
Go Daemon
  |  SnapshotStore (全量存储 + 分层检索 + 增量 diff)
  |  TempFileManager (截图自动清理)
  |  Token Auth
  | WebSocket (localhost:9333, token 验证)
Chrome Extension (Manifest V3)
  |  offscreen.js (持久 WebSocket 连接 + 心跳保活)
  |  background.js (Chrome API + content script 注入)
  |  content.js (DOM 操作 + data-attribute ref + 可访问性树生成)
```

### 数据流

```
CLI ---Unix Socket---> Daemon ---WebSocket---> Extension (offscreen.js)
                                                  |
                                         chrome.runtime.sendMessage
                                                  |
                                            background.js
                                                  |
                                      chrome.scripting.executeScript
                                                  |
                                             content.js
                                          (DOM 操作 / snapshot)
                                                  |
                                          原路返回结果
```

### 组件职责

| 组件 | 职责 |
|------|------|
| **Go CLI** | 用户/Skill 入口，Cobra 命令解析，JSON 输出 |
| **Go Daemon** | 双通道监听（Unix Socket + WebSocket），命令路由，SnapshotStore，临时文件管理 |
| **offscreen.js** | 持久 WebSocket 客户端，命令转发，心跳保活 |
| **background.js** | Chrome API 调用（tabs/cookies/downloads），content script 注入调度，chrome.debugger 协调（console/network/dialog/upload） |
| **content.js** | DOM 操作执行，可访问性树生成，data-attribute ref 管理 |
| **popup** | 连接状态指示灯，端口配置 |

## 3. Chrome Extension 设计

### Manifest V3

```json
{
  "manifest_version": 3,
  "name": "Chrome Pilot",
  "permissions": ["tabs", "activeTab", "scripting", "cookies", "debugger", "downloads", "offscreen", "alarms"],
  "host_permissions": ["<all_urls>"],
  "background": { "service_worker": "background.js" },
  "action": { "default_popup": "popup/popup.html" }
}
```

### MV3 Service Worker 休眠问题

Manifest V3 的 Service Worker 约 30 秒无活动后被 Chrome 终止，WebSocket 连接随之断开。

解决方案：**Offscreen Document 持有 WebSocket 连接**。

- `chrome.offscreen.createDocument()` 创建隐藏页面，持有 WebSocket 客户端
- Offscreen Document 通过 `chrome.runtime.sendMessage` 与 Service Worker 通信
- 20 秒心跳 keep-alive
- `chrome.alarms` 每 30 秒检测 Offscreen Document 存活，不存在则重建

```
offscreen.js:
  setInterval(() => ws.send(ping), 20000)

background.js:
  chrome.alarms.create('check-offscreen', { periodInMinutes: 0.5 })
  onAlarm → if (!hasDocument()) → createOffscreenDocument()
```

### 命令执行的两条路径

Extension 内部有两条不同的执行路径，按命令类型分发：

**路径 A：Content Script 注入（大多数命令）**

```
snapshot / dom.click / dom.type / dom.hover / dom.eval / page.wait / page.content ...
  → chrome.scripting.executeScript 注入 content.js
  → content.js 操作 DOM，通过 return 返回结果
```

**路径 B：chrome.debugger API（特殊命令）**

以下命令无法通过 content script 实现，必须使用 `chrome.debugger`：

| 命令 | 原因 | 使用的 CDP 方法 |
|------|------|----------------|
| `page console` | 需要在页面加载前拦截 console 调用 | `Runtime.consoleAPICalled` 事件 |
| `page network` | 需要在页面加载前拦截网络请求 | `Network.requestWillBeSent` / `Network.responseReceived` 事件 |
| `page dialog` | 原生弹窗（alert/confirm/prompt）阻塞主线程，content script 无法执行 | `Page.javascriptDialogOpening` 事件 + `Page.handleJavaScriptDialog` |
| `dom upload` | 浏览器安全策略禁止通过 JS 设置 file input 的值 | `DOM.setFileInputFiles` |

debugger 的 attach/detach 策略：
- 首次调用上述命令时，`chrome.debugger.attach` 到目标 tab
- console 和 network 需要持续监听，attach 后保持到 tab 关闭或用户显式 detach
- dialog 和 upload 是一次性操作，执行完毕后不主动 detach（复用已有连接）
- Chrome 会在 tab 顶部显示 "正在调试此浏览器" 提示条，这是不可避免的

```
收到 chrome.runtime.message（来自 offscreen.js）
  |
  +--> tab.* / cookie.*
  |     直接调 Chrome API (chrome.tabs / chrome.cookies)
  |
  +--> page.screenshot
  |     chrome.tabs.captureVisibleTab (视口截图)
  |     或注入 content script (全页/元素截图)
  |
  +--> page.console / page.network / page.dialog / dom.upload
  |     chrome.debugger API (CDP 协议)
  |
  +--> snapshot / dom.* (其余)
        chrome.scripting.executeScript 注入 content.js
```

### Content Script 注入策略

动态注入（不常驻），通过 `chrome.scripting.executeScript` 按需注入到目标 tab。

不可注入的页面（`chrome://`、`chrome-extension://`、Chrome Web Store）返回明确错误。

### Ref 持久化

Ref 通过 `data-` attribute 存储在 DOM 元素上：

```javascript
// snapshot 时
element.setAttribute('data-cp-ref', 'e1');

// 后续命令时（任何注入上下文都能读到）
document.querySelector('[data-cp-ref="e1"]').click();
```

- `data-` attribute 是真实 DOM 属性，跨 `chrome.scripting.executeScript` 注入上下文持久
- 页面导航时 DOM 自然清除
- SPA 路由切换可能导致 DOM 节点重建，`data-cp-ref` 随之丢失。**任何导航（包括 SPA 路由切换）后都应重新 snapshot**
- 跨 snapshot 稳定性由 Daemon 维护：存储 `(role+name) → ref` 映射，新 snapshot 对相同特征的元素复用相同 ref ID

### 连接生命周期

```
Extension 启动 → offscreen.js 尝试连接 ws://localhost:9333
  成功 → 发送 token 认证 → popup 绿灯
  失败 → 1s/2s/4s/8s...30s 退避重试

Daemon 未启动 → Extension 安静重试，不报错
Daemon 启动后 → 自动连上

Offscreen Document 被回收 → chrome.alarms 检测 → 重建 → 重连
```

## 4. Go Daemon 设计

### 双通道监听

```go
type Daemon struct {
    rpcServer    *rpc.Server       // Unix Socket <- CLI
    wsServer     *bridge.WSServer  // WebSocket <- Extension
    snapStore    *snapshot.Store   // 快照存储与检索
    tmpManager   *tmpfile.Manager  // 临时文件管理
    session      *SessionState     // 当前工作 tab 等状态
    idleTimer    *time.Timer       // 空闲超时 30min
}
```

### 命令处理流

```
CLI 调用 "dom click --ref E5"
  -> sockutil.Call("dom.click", {ref: "E5"})
  -> Unix Socket -> Daemon
  -> Daemon 检查 Extension 是否已连接
  -> Daemon 通过 WebSocket 转发 {method: "dom.click", ref: "E5", tabId: workingTab}
  -> Extension content.js: querySelector('[data-cp-ref="E5"]').click()
  -> 结果原路返回 -> CLI JSON 输出
```

### 请求/响应匹配

每个命令带唯一 ID，Daemon 维护 `map[id]chan *Response`。Extension 返回时按 ID 匹配。超时 10 秒。

### SessionState

```go
type SessionState struct {
    workingTabID int  // 首次 snapshot 时锁定，后续命令沿用
}
```

所有页面/DOM 命令支持 `--tab` 参数。`--tab` 接受 Chrome 内部 tab ID（稳定标识，从 `tab list` 的 `id` 字段获取）。不传时用 workingTabID，workingTabID 未设时用 active tab。CLI 中 `tab select`/`tab close` 使用位置 index 作为便捷方式，Daemon 内部通过 `tab list` 结果转换为 tab ID。

### WebSocket Token 认证

- Daemon 启动时生成随机 token，写入 `~/.chrome-pilot/token`（文件权限 `0600`）
- Extension 连接后首条消息带 token
- 验证失败直接断开
- 威胁模型：防止本地其他进程误连，不防御同用户下的恶意程序（与所有本地 CLI 工具的安全边界一致）

### 多连接策略

同时只接受一个 Extension 连接。第二个连接到达时拒绝并返回错误：`"another extension already connected"`。用户可通过 `chrome-pilot status` 查看当前连接来源。

### 文件路径

| 文件 | 路径 |
|------|------|
| Unix Socket | `~/.chrome-pilot/chrome-pilot.sock` |
| PID 文件 | `~/.chrome-pilot/chrome-pilot.pid` |
| 配置 | `~/.chrome-pilot/config.yaml` |
| 日志 | `~/.chrome-pilot/chrome-pilot.log` |
| Token | `~/.chrome-pilot/token` |
| 临时文件 | `~/.chrome-pilot/tmp/` |

### 配置

```yaml
ws_port: 9333
idle_timeout: 30m
socket_path: ~/.chrome-pilot/chrome-pilot.sock
log_level: info
tmp_max_age: 24h
tmp_max_size: 500MB
```

## 5. Snapshot 分层检索

核心设计：Daemon 存储完整快照，Claude 通过检索命令按需获取部分内容，避免大页面撑爆上下文。

### 工作流

```
snapshot(摘要) -> Claude 判断目标区域 -> 展开/检索 -> 操作 -> snapshot(增量 diff)
```

### 摘要返回

```bash
$ chrome-pilot snapshot
{
  "snapshotId": "snap_001",
  "url": "https://jira.internal.com/board",
  "title": "Sprint Board - Jira",
  "stats": { "totalNodes": 2103, "interactable": 87 },
  "landmarks": [
    { "role": "navigation", "name": "Main Nav", "ref": "e1", "children": 12 },
    { "role": "main", "name": "", "ref": "e2", "children": 1847 },
    { "role": "complementary", "name": "Sidebar", "ref": "e3", "children": 244 }
  ],
  "headings": ["Sprint Board", "To Do (12)", "In Progress (5)", "Done (23)"]
}
```

摘要 fallback：无 landmark 时返回 top-level 可交互元素列表（前 20 个）；都没有则返回 DOM 结构前 3 层。

### 检索命令

```bash
chrome-pilot snapshot --ref e2              # 展开指定子树
chrome-pilot snapshot --ref e2 --depth 2    # 限制深度
chrome-pilot snapshot --search "Submit"     # 文本搜索
chrome-pilot snapshot --role button         # 按 role 过滤
chrome-pilot snapshot --interactable        # 只看可交互元素
```

### 增量 Diff

参考 Playwright MCP 的 incremental snapshot 模式：

- Daemon 保存 current + previous 两份快照
- 新 snapshot 与 previous 按 ref 逐节点对比
- 返回 diff 摘要：哪些区域变了、新增/消失的元素

```bash
$ chrome-pilot snapshot   # 操作后再次拍快照
{
  "snapshotId": "snap_002",
  "baseId": "snap_001",
  "changed": [
    { "ref": "e15", "description": "dialog 'Confirm Delete' appeared", "children": 5 },
    { "ref": "e2", "description": "main: 3 nodes changed" }
  ]
}
```

### Snapshot 存储

```go
type Store struct {
    mu   sync.Mutex
    tabs map[int]*TabSnapshot  // tabID -> 该 tab 的快照数据
}

type TabSnapshot struct {
    current  *Snapshot
    previous *Snapshot
    index    map[string]*Node  // ref -> node 快速检索
    lastUsed time.Time
}
```

- 只保留 current + previous，不累积
- Tab 关闭 → 自动清除
- 任何导航（包括同域跳转、SPA 路由切换）→ 自动清除。Extension 通过 `chrome.tabs.onUpdated` 监听 URL 变化，通知 Daemon 清除对应 tab 的快照
- 超过 10 分钟未访问 → 自动过期

### Snapshot Info / Clear

```bash
$ chrome-pilot snapshot info
{
  "current": { "id": "snap_002", "url": "...", "nodes": 2103, "age": "3m20s" },
  "previous": { "id": "snap_001", "nodes": 1987, "age": "5m10s" }
}

$ chrome-pilot snapshot clear
{"cleared": "snap_002"}
```

## 6. 完整命令集

### 连接管理

| 命令 | 说明 |
|------|------|
| `start` | 启动 daemon（已运行则报 "already running"；检测到 stale PID 文件自动清理后启动） |
| `stop` | 停止 daemon |
| `status` | daemon + extension 连接状态 |

### Tab 管理

| 命令 | 说明 |
|------|------|
| `tab list` | 列出所有 tab（id, title, url, active） |
| `tab new <url>` | 新开 tab |
| `tab select <index>` | 切换 tab |
| `tab close [index]` | 关闭 tab（默认当前 tab） |

### 页面操作

| 命令 | 说明 |
|------|------|
| `page navigate <url>` | 导航 |
| `page back` | 后退 |
| `page screenshot [--full] [--ref X] [--file path]` | 截图，存文件返回路径 |
| `page wait --text X / --text-gone X / --time N` | 等待条件 |
| `page console [--level info\|warning\|error]` | 控制台日志（按 level 过滤） |
| `page network [--include-static]` | 网络请求（默认过滤静态资源） |
| `page resize --width W --height H` | 调整窗口 |
| `page dialog --accept [--text "..."]` | 处理弹窗 |
| `page close` | 关闭页面 |
| `page content [--format html\|text]` | 获取页面内容 |

### 快照

| 命令 | 说明 |
|------|------|
| `snapshot` | 拍快照，返回摘要 |
| `snapshot --ref E1 [--depth N]` | 展开指定子树 |
| `snapshot --search "text"` | 文本搜索 |
| `snapshot --role button` | 按 role 过滤 |
| `snapshot --interactable` | 只看可交互元素 |
| `snapshot info` | 查看快照状态 |
| `snapshot clear` | 清除快照缓存 |

### DOM 交互（基于 ref）

| 命令 | 说明 |
|------|------|
| `dom click --ref E1 [--button left\|right\|middle] [--double] [--mod Shift]` | 点击 |
| `dom type --ref E1 --text "..." [--slowly] [--submit]` | 输入文本 |
| `dom hover --ref E1` | 悬停 |
| `dom drag --start-ref E1 --end-ref E2` | 拖拽 |
| `dom key <key>` | 按键（Enter, Escape, ArrowDown...） |
| `dom select --ref E1 --values "v1,v2"` | 下拉选择 |
| `dom fill --fields '<json>'` | 批量填表 |
| `dom upload --paths "path1,path2"` | 文件上传（通过 chrome.debugger `DOM.setFileInputFiles`） |
| `dom eval --js "() => ..." [--ref E1]` | 执行 JS |

### 数据读取

| 命令 | 说明 |
|------|------|
| `cookie list [--domain X]` | 列出 Cookie |
| `cookie get --name X --domain X` | 获取指定 Cookie |

### 清理

| 命令 | 说明 |
|------|------|
| `clean` | 清理所有临时文件 |
| `clean --before 3d` | 清理指定时间前的 |
| `clean --dry-run` | 预览清理量 |

所有页面/DOM/快照命令支持 `--tab <tabID>` 指定目标 tab（tab ID 从 `tab list` 的 `id` 字段获取）。

## 7. WebSocket 协议

### 请求 (Daemon -> Extension)

```json
{
  "id": "req_abc123",
  "method": "dom.click",
  "params": { "tabId": 123, "ref": "E5" }
}
```

### 响应 (Extension -> Daemon)

```json
{
  "id": "req_abc123",
  "result": { "success": true },
  "error": null
}
```

### 认证 (Extension -> Daemon, 首条消息)

```json
{
  "method": "auth",
  "params": { "token": "随机token" }
}
```

### 心跳 (双向)

```json
{ "method": "ping" }
```

### Extension 主动事件 (Extension -> Daemon)

Extension 通过 `chrome.tabs.onUpdated` / `chrome.tabs.onRemoved` 监听 tab 变化，主动通知 Daemon：

```json
{
  "id": null,
  "event": "tab.navigated",
  "data": { "tabId": 123, "url": "https://new-url.com" }
}
```

```json
{
  "id": null,
  "event": "tab.closed",
  "data": { "tabId": 123 }
}
```

Daemon 收到后清除对应 tab 的 snapshot 缓存。

## 8. CLI 输出格式

所有命令默认 JSON 输出，供 Skill 解析：

```bash
$ chrome-pilot tab list
[
  {"index": 0, "id": 123, "title": "GitHub", "url": "https://github.com", "active": true},
  {"index": 1, "id": 124, "title": "Gmail", "url": "https://mail.google.com", "active": false}
]

$ chrome-pilot dom click --ref E5
{"success": true}

$ chrome-pilot page screenshot
{"path": "/Users/JOYY/.chrome-pilot/tmp/screenshot-20260325-163022.png"}
```

错误输出：

```bash
$ chrome-pilot dom click --ref E99
{"error": "ref E99 not found, run snapshot first"}

$ chrome-pilot snapshot
{"error": "extension not connected, check Chrome extension status"}

$ chrome-pilot page navigate "chrome://settings"
{"error": "cannot inject into chrome:// pages"}
```

## 9. 临时文件管理

截图等文件存储在 `~/.chrome-pilot/tmp/`。

自动清理：
- Daemon 启动时清理超过 24 小时的文件
- 目录超过 500MB 时清理最旧文件

手动清理：
```bash
chrome-pilot clean                  # 清理全部
chrome-pilot clean --before 3d      # 清理 3 天前
chrome-pilot clean --dry-run        # 预览
```

## 10. Skill 设计

单一 Skill：`skills/chrome-pilot/SKILL.md`。

内容分章节：
1. 前置条件检查（daemon + extension 连接）
2. 核心工作流：`snapshot(摘要) → 展开/检索 → 操作 → snapshot(增量)`
3. 命令参考（全部命令）
4. 错误处理表

### 错误处理

| 错误 | 原因 | 处理 |
|------|------|------|
| `extension not connected` | 扩展未安装/未启用 | 提示用户检查 Chrome 扩展 |
| `ref E5 not found` | ref 过期（页面已变化） | 重新 snapshot |
| `element not visible` | 元素被遮挡/隐藏 | scroll 或 wait |
| `timeout` | 操作超时 10s | 检查页面加载状态 |
| `tab not found` | tab 已关闭 | tab list 获取最新 |
| `cannot inject into chrome:// pages` | 不可注入页面 | 提示用户手动操作 |

## 11. 目录结构

```
chrome_pilot/
├── cmd/
│   ├── root.go
│   ├── start.go / stop.go / status.go
│   ├── tab.go
│   ├── page.go
│   ├── snapshot.go
│   ├── dom.go
│   ├── cookie.go
│   └── clean.go
├── internal/
│   ├── daemon/
│   │   ├── daemon.go
│   │   └── handlers.go
│   ├── rpc/
│   │   └── server.go
│   ├── bridge/
│   │   ├── wsserver.go
│   │   └── pending.go
│   ├── snapshot/
│   │   ├── store.go
│   │   ├── diff.go
│   │   └── index.go
│   ├── tmpfile/
│   │   └── manager.go
│   ├── sockutil/
│   │   └── client.go
│   └── config/
│       └── config.go
├── extension/
│   ├── manifest.json
│   ├── background.js
│   ├── offscreen.html
│   ├── offscreen.js
│   ├── content.js
│   └── popup/
│       ├── popup.html
│       └── popup.js
├── skills/
│   └── chrome-pilot/
│       └── SKILL.md
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## 12. 设计决策记录

| # | 决策 | 理由 |
|---|---|---|
| 1 | Chrome Extension + WebSocket，不用 CDP | CDP 需要 debug 模式启动 Chrome，侵入性强 |
| 2 | Extension 做薄代理，逻辑在 Daemon | 迭代快，调试方便，对齐 marki_agent 模式 |
| 3 | Offscreen Document 持有 WebSocket | 解决 MV3 Service Worker 30s 休眠问题 |
| 4 | Ref 用 data-attribute 存 DOM | 跨 content script 注入上下文持久，简单可靠 |
| 5 | Snapshot 分层检索（摘要→展开→搜索） | 解决大页面 token 爆炸问题 |
| 6 | 增量 snapshot diff | 参考 Playwright MCP，减少重复传输 |
| 7 | Screenshot 存文件返回路径 | CLI 场景不适合 base64 内联 |
| 8 | 单一 Skill | 浏览器操作天然连贯，拆分增加 Skill 切换开销 |
| 9 | Go + Cobra + JSON-RPC 2.0 + Unix Socket | 对齐 marki_agent 现有架构 |
| 10 | Token 验证 WebSocket | 防止本地其他进程误连 |
| 11 | 临时文件自动清理 + 手动 clean | 防止磁盘膨胀 |
| 12 | Snapshot 自动过期 | 防止内存膨胀 |
| 13 | 所有命令支持 --tab 参数 | 防止手动切 tab 后操作错误页面 |
| 14 | 部分命令走 chrome.debugger（console/network/dialog/upload） | content script 无法实现这些功能 |
| 15 | 单连接策略 | 避免多 Chrome profile 同时连接的歧义 |
| 16 | token 文件 0600 权限 | 安全基线 |
| 17 | 任何导航都清除 snapshot | SPA 路由切换会重建 DOM 节点，旧 ref 失效 |
