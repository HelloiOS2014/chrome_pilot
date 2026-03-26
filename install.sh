#!/usr/bin/env bash
set -euo pipefail

# ── Chrome Pilot 安装脚本 ──
# 独立运行，无需 clone 仓库或安装 Go。
# 用法:
#   curl -fsSL https://raw.githubusercontent.com/HelloiOS2014/chrome_pilot/main/install.sh | bash
#   bash install.sh
#   bash install.sh --version v0.2.0

REPO="HelloiOS2014/chrome_pilot"
VERSION=""
INSTALL_BIN="/usr/local/bin/chrome-pilot"
DATA_DIR="$HOME/.chrome-pilot"

# ── 颜色 ──
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; BOLD='\033[1m'; NC='\033[0m'
info()    { echo -e "${BLUE}▸${NC} $*"; }
ok()      { echo -e "${GREEN}✔${NC} $*"; }
warn()    { echo -e "${YELLOW}⚠${NC} $*"; }
err()     { echo -e "${RED}✖${NC} $*"; }

confirm() {
    local prompt="$1" default="${2:-y}"
    [[ "$default" == "y" ]] && prompt+=" [Y/n] " || prompt+=" [y/N] "
    read -rp "$(echo -e "${BOLD}${prompt}${NC}")" answer
    answer="${answer:-$default}"
    [[ "$answer" =~ ^[Yy] ]]
}

# ── 参数解析 ──
while [[ $# -gt 0 ]]; do
    case "$1" in
        --version)  VERSION="$2"; shift 2 ;;
        --uninstall) do_uninstall=1; shift ;;
        --help)
            echo "用法: bash install.sh [--version vX.Y.Z] [--uninstall]"
            exit 0 ;;
        *) shift ;;
    esac
done

# ── 卸载 ──
if [[ "${do_uninstall:-}" == "1" ]]; then
    echo -e "\n${BOLD}═══ chrome-pilot 卸载 ═══${NC}\n"
    if [[ -f "$INSTALL_BIN" ]] && confirm "移除 ${INSTALL_BIN}？"; then
        sudo rm -f "$INSTALL_BIN"
        ok "已移除 ${INSTALL_BIN}"
    fi
    if [[ -d "$DATA_DIR/extension" ]] && confirm "移除 ${DATA_DIR}/extension？"; then
        rm -rf "$DATA_DIR/extension"
        ok "已移除 extension"
    fi
    for skill_dir in "$HOME/.claude/skills/chrome-pilot"; do
        if [[ -d "$skill_dir" ]]; then
            rm -rf "$skill_dir"
            ok "已移除 $skill_dir"
        fi
    done
    echo -e "\n${GREEN}卸载完成${NC}"
    exit 0
fi

# ── 检测平台 ──
detect_platform() {
    local os arch
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *) err "不支持的架构: $arch"; exit 1 ;;
    esac
    case "$os" in
        darwin|linux) ;;
        *) err "不支持的系统: $os"; exit 1 ;;
    esac
    echo "${os}_${arch}"
}

# ── 获取最新版本 ──
get_latest_version() {
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/'
}

# ── 主流程 ──
echo -e "\n${BOLD}═══ Chrome Pilot 安装 ═══${NC}\n"

PLATFORM="$(detect_platform)"
info "检测到平台: ${PLATFORM}"

# 确定版本
if [[ -z "$VERSION" ]]; then
    info "获取最新版本..."
    VERSION="$(get_latest_version)"
fi
if [[ -z "$VERSION" ]]; then
    err "无法获取版本信息，请使用 --version 指定"
    exit 1
fi
ok "版本: ${VERSION}"

# Step 1: 下载
ARCHIVE="chrome-pilot_${PLATFORM}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo ""
info "下载 ${URL}..."
if ! curl -fSL -o "${TMP_DIR}/${ARCHIVE}" "$URL"; then
    err "下载失败: ${URL}"
    exit 1
fi
ok "下载完成"

# 解压
info "解压中..."
tar xzf "${TMP_DIR}/${ARCHIVE}" -C "$TMP_DIR"
ok "解压完成"

# Step 2: 安装二进制到 PATH
echo ""
if [[ -f "$INSTALL_BIN" ]]; then
    warn "${INSTALL_BIN} 已存在"
    if confirm "覆盖？"; then
        sudo cp "${TMP_DIR}/chrome-pilot" "$INSTALL_BIN"
        sudo chmod +x "$INSTALL_BIN"
        ok "已更新 ${INSTALL_BIN}"
    else
        info "跳过"
    fi
else
    if confirm "安装到 ${INSTALL_BIN}？"; then
        sudo cp "${TMP_DIR}/chrome-pilot" "$INSTALL_BIN"
        sudo chmod +x "$INSTALL_BIN"
        ok "已安装到 ${INSTALL_BIN}"
    else
        # 安装到 ~/.chrome-pilot/bin 作为备选
        mkdir -p "${DATA_DIR}/bin"
        cp "${TMP_DIR}/chrome-pilot" "${DATA_DIR}/bin/chrome-pilot"
        chmod +x "${DATA_DIR}/bin/chrome-pilot"
        ok "已安装到 ${DATA_DIR}/bin/chrome-pilot"
        warn "请将 ${DATA_DIR}/bin 加入 PATH"
    fi
fi

# Step 3: 安装 Extension
echo ""
info "安装 Chrome Extension..."
mkdir -p "${DATA_DIR}/extension"
if [[ -d "${TMP_DIR}/extension" ]]; then
    rm -rf "${DATA_DIR}/extension"
    cp -R "${TMP_DIR}/extension" "${DATA_DIR}/extension"
    ok "Extension 已安装到 ${DATA_DIR}/extension"
else
    # 从单独的 extension.zip 下载
    EXT_URL="https://github.com/${REPO}/releases/download/${VERSION}/extension.zip"
    if curl -fSL -o "${TMP_DIR}/extension.zip" "$EXT_URL" 2>/dev/null; then
        rm -rf "${DATA_DIR}/extension"
        mkdir -p "${DATA_DIR}/extension"
        unzip -q "${TMP_DIR}/extension.zip" -d "${DATA_DIR}/extension"
        ok "Extension 已安装到 ${DATA_DIR}/extension"
    else
        warn "未找到 Extension 文件，请从仓库手动获取"
    fi
fi

# Step 4: Skills 安装
echo ""
if [[ -d "${TMP_DIR}/skills" ]]; then
    echo -e "${BOLD}Skills 安装方式：${NC}"
    echo "  [1] 全局安装 — 复制到 ~/.claude/skills/"
    echo "  [2] 项目级安装 — 复制到指定项目"
    echo "  [3] 跳过"
    echo ""
    read -rp "$(echo -e "${BOLD}选择 [1/2/3]: ${NC}")" choice
    case "$choice" in
        1)
            mkdir -p "$HOME/.claude/skills"
            cp -R "${TMP_DIR}/skills/chrome-pilot" "$HOME/.claude/skills/chrome-pilot"
            ok "Skills 已安装到 ~/.claude/skills/chrome-pilot"
            ;;
        2)
            read -rp "$(echo -e "${BOLD}输入目标项目根目录路径: ${NC}")" project_root
            project_root="${project_root/#\~/$HOME}"
            if [[ ! -d "$project_root" ]]; then
                err "目录不存在: $project_root"; exit 1
            fi
            mkdir -p "${project_root}/.claude/skills"
            cp -R "${TMP_DIR}/skills/chrome-pilot" "${project_root}/.claude/skills/chrome-pilot"
            ok "Skills 已安装到 ${project_root}/.claude/skills/chrome-pilot"
            ;;
        3|*) info "跳过 Skills 安装" ;;
    esac
fi

# Step 5: Chrome Extension 加载引导
echo ""
echo -e "${BOLD}═══ Chrome Extension 加载 ═══${NC}\n"
echo -e "  ${YELLOW}1.${NC} 打开 Chrome，访问 ${BOLD}chrome://extensions${NC}"
echo -e "  ${YELLOW}2.${NC} 打开右上角 ${BOLD}开发者模式${NC}"
echo -e "  ${YELLOW}3.${NC} 点击 ${BOLD}加载已解压的扩展程序${NC}"
echo -e "  ${YELLOW}4.${NC} 选择目录: ${BOLD}${DATA_DIR}/extension${NC}"
echo ""

if confirm "是否现在打开 chrome://extensions？" "n"; then
    if command -v open &>/dev/null; then
        open "chrome://extensions"
    elif command -v xdg-open &>/dev/null; then
        xdg-open "chrome://extensions"
    fi
fi

# Step 6: 使用引导
echo ""
echo -e "${BOLD}═══ 使用方法 ═══${NC}\n"
echo -e "  ${BOLD}chrome-pilot start${NC}              # 启动守护进程"
echo -e "  ${BOLD}chrome-pilot status${NC}             # Extension 自动连接"
echo -e "  ${BOLD}chrome-pilot snapshot${NC}           # 获取页面快照"
echo -e "  ${BOLD}chrome-pilot dom click --ref e1${NC} # 点击元素"
echo ""
echo -e "${GREEN}安装完成！${NC}"
