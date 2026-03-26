# Chrome Pilot

让 Claude Code 操控你现有的 Chrome 浏览器，保留登录态和所有配置。

## 为什么需要这个？

现有的浏览器 MCP 工具（如 Playwright MCP）都会启动**新的** Chrome 实例，导致：
- 登录态丢失，内部系统（Jira、Sentry、运维平台等）每次都要重新登录
- Cookie、扩展、配置全部丢失
- 无法操作需要 VPN/SSO 认证的页面

Chrome Pilot 通过 Chrome Extension 直接连接你**正在使用的** Chrome，一切状态原样保留。

## 架构

```
Claude Code
  │ bash: chrome-pilot <command>
  ▼
Go CLI (Cobra)
  │ JSON-RPC 2.0 / Unix Socket
  ▼
Go Daemon (:9333)
  │ WebSocket (token 自动认证)
  ▼
Chrome Extension (Manifest V3)
  ├─ offscreen.js  → 持久 WebSocket 连接
  ├─ background.js → Chrome API 调用
  └─ content.js    → DOM 操作 / 可访问性快照
```

## 安装

### 一键安装（推荐）

```bash
curl -fsSL https://raw.githubusercontent.com/HelloiOS2014/chrome_pilot/main/install.sh | bash
```

自动检测平台，从 [GitHub Releases](https://github.com/HelloiOS2014/chrome_pilot/releases/latest) 下载预编译包，交互式引导完成安装。无需 Go 环境。

指定版本：`bash install.sh --version v0.2.0`

卸载：`bash install.sh --uninstall`

### 从源码构建（开发者）

```bash
git clone git@github.com:HelloiOS2014/chrome_pilot.git
cd chrome_pilot
go build -o chrome-pilot .
sudo cp chrome-pilot /usr/local/bin/
cp -R skills/chrome-pilot ~/.claude/skills/
# Chrome → chrome://extensions → 开发者模式 → 加载已解压的扩展程序 → 选择 extension/ 目录
```

## 快速开始

```bash
# 1. 启动守护进程
chrome-pilot start

# 2. 在 Chrome 加载 extension/ 目录（首次安装后自动连接）

# 3. 检查连接
chrome-pilot status
# {"daemon":"running","pid":12345,"extension":"connected","ws_port":9333}

# 4. 获取页面快照
chrome-pilot snapshot
# 返回页面结构摘要（landmarks、headings、可交互元素数量）

# 5. 展开并操作
chrome-pilot snapshot --ref e2          # 展开某个区域
chrome-pilot dom click --ref e5         # 点击元素
chrome-pilot dom type --ref e3 --text "hello"  # 输入文本

# 6. 停止
chrome-pilot stop
```

## 命令参考

### 连接管理

| 命令 | 说明 |
|------|------|
| `start` | 启动 daemon（后台运行） |
| `start --foreground` | 前台运行 daemon |
| `stop` | 停止 daemon |
| `status` | 查看 daemon + extension 连接状态 |

### Tab 管理

| 命令 | 说明 |
|------|------|
| `tab list` | 列出所有 tab |
| `tab new <url>` | 新开 tab |
| `tab select <index>` | 切换 tab |
| `tab close [index]` | 关闭 tab |

### 快照（分层检索）

| 命令 | 说明 |
|------|------|
| `snapshot` | 拍快照，返回摘要 |
| `snapshot --ref E1` | 展开指定子树 |
| `snapshot --ref E1 --depth 2` | 限制展开深度 |
| `snapshot --search "text"` | 文本搜索 |
| `snapshot --role button` | 按 ARIA role 过滤 |
| `snapshot --interactable` | 只看可交互元素 |
| `snapshot info` | 查看快照缓存状态 |
| `snapshot clear` | 清除快照缓存 |

### DOM 交互（基于 ref）

| 命令 | 说明 |
|------|------|
| `dom click --ref E1 [--button left\|right] [--double]` | 点击 |
| `dom type --ref E1 --text "..." [--slowly] [--submit]` | 输入文本 |
| `dom hover --ref E1` | 悬停 |
| `dom drag --start-ref E1 --end-ref E2` | 拖拽 |
| `dom key <key>` | 按键 |
| `dom select --ref E1 --values "v1,v2"` | 下拉选择 |
| `dom fill --fields '<json>'` | 批量填表 |
| `dom upload --paths "path1,path2"` | 文件上传 |
| `dom eval --js "() => ..." [--ref E1]` | 执行 JS |

### 页面操作

| 命令 | 说明 |
|------|------|
| `page navigate <url>` | 导航 |
| `page back` | 后退 |
| `page screenshot [--full] [--ref E1] [--file path]` | 截图 |
| `page wait --text "..." / --time N` | 等待条件 |
| `page console [--level info]` | 控制台日志 |
| `page network [--include-static]` | 网络请求 |
| `page content [--format html\|text]` | 获取页面内容 |
| `page resize --width W --height H` | 调整窗口 |
| `page dialog --accept [--text "..."]` | 处理弹窗 |
| `page close` | 关闭页面 |

### 数据读取

| 命令 | 说明 |
|------|------|
| `cookie list [--domain X]` | 列出 Cookie |
| `cookie get --name X --domain X` | 获取指定 Cookie |

### 清理

| 命令 | 说明 |
|------|------|
| `clean` | 清理所有临时文件（截图等） |
| `clean --before 3d` | 清理 3 天前的文件 |
| `clean --dry-run` | 预览清理量 |

所有页面/DOM/快照命令支持 `--tab <tabID>` 指定目标 tab。

## 核心工作流

```
snapshot(摘要) → 展开/检索目标区域 → 操作元素 → snapshot(验证结果)
```

1. `snapshot` 返回页面结构摘要，不会返回全部内容（避免 token 爆炸）
2. 用 `--ref`/`--search`/`--role` 按需展开需要的部分
3. 用 ref 标识（如 `e1`, `e5`）操作具体元素
4. 操作后再次 `snapshot` 查看增量变化

## Chrome Extension

### 加载方法

1. 打开 `chrome://extensions`
2. 开启**开发者模式**
3. 点击**加载已解压的扩展程序**
4. 选择本项目的 `extension/` 目录

### 工作原理

- **offscreen.js** — 持久化 WebSocket 连接（解决 MV3 Service Worker 30s 休眠问题）
- **background.js** — 命令路由，调用 Chrome API（tabs/cookies/scripting）
- **content.js** — 注入页面执行 DOM 操作，生成可访问性快照，通过 `data-cp-ref` 属性标记元素

Token 自动认证：Extension 从 `http://localhost:9333/token` 自动获取，无需手动配置。

## 设计文档

详细的架构设计和决策记录：[`docs/superpowers/specs/2026-03-25-chrome-pilot-design.md`](docs/superpowers/specs/2026-03-25-chrome-pilot-design.md)
