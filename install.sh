#!/usr/bin/env bash
set -euo pipefail

# ── 颜色 ──
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY_NAME="chrome-pilot"
INSTALL_BIN="/usr/local/bin/${BINARY_NAME}"

SKILLS=(chrome-pilot)

# ── 辅助函数 ──

info()  { echo -e "${BLUE}▸${NC} $*"; }
ok()    { echo -e "${GREEN}✔${NC} $*"; }
warn()  { echo -e "${YELLOW}⚠${NC} $*"; }
err()   { echo -e "${RED}✖${NC} $*"; }

confirm() {
    local prompt="$1"
    local default="${2:-y}"
    if [[ "$default" == "y" ]]; then
        prompt+=" [Y/n] "
    else
        prompt+=" [y/N] "
    fi
    read -rp "$(echo -e "${BOLD}${prompt}${NC}")" answer
    answer="${answer:-$default}"
    [[ "$answer" =~ ^[Yy] ]]
}

copy_skill() {
    local src="$1"
    local dst="$2"
    if [[ -L "$dst" ]]; then
        rm "$dst"
        info "移除旧 symlink: $dst"
    fi
    if [[ -d "$dst" ]]; then
        rm -rf "$dst"
    fi
    cp -R "$src" "$dst"
    ok "复制: $(basename "$dst")"
}

remove_skill() {
    local dst="$1"
    if [[ -e "$dst" || -L "$dst" ]]; then
        rm -rf "$dst"
        ok "已移除: $dst"
    fi
}

# ── 卸载 ──

do_uninstall() {
    echo -e "\n${BOLD}═══ chrome-pilot 卸载 ═══${NC}\n"

    # 移除全局 skills
    local global_dir="$HOME/.claude/skills"
    for skill in "${SKILLS[@]}"; do
        remove_skill "${global_dir}/${skill}"
    done

    # 移除二进制
    if [[ -f "$INSTALL_BIN" ]]; then
        if confirm "移除 ${INSTALL_BIN}？"; then
            sudo rm -f "$INSTALL_BIN"
            ok "已移除 ${INSTALL_BIN}"
        fi
    fi

    echo ""
    info "如果曾安装到项目级目录，请手动移除对应 .claude/skills/ 下的 skill 文件夹"
    echo -e "\n${GREEN}卸载完成${NC}"
    exit 0
}

# ── 主流程 ──

if [[ "${1:-}" == "--uninstall" ]]; then
    do_uninstall
fi

echo -e "\n${BOLD}═══ chrome-pilot 安装 ═══${NC}\n"

# Step 1: 构建
info "构建 ${BINARY_NAME}..."
cd "$SCRIPT_DIR"
go build -o "$BINARY_NAME" .
ok "构建成功: ${SCRIPT_DIR}/${BINARY_NAME}"

# Step 2: 安装到 PATH
echo ""
if [[ -f "$INSTALL_BIN" ]]; then
    warn "${INSTALL_BIN} 已存在"
    if confirm "覆盖？"; then
        sudo cp "${SCRIPT_DIR}/${BINARY_NAME}" "$INSTALL_BIN"
        ok "已更新 ${INSTALL_BIN}"
    else
        info "跳过安装到 PATH"
    fi
else
    if confirm "安装到 ${INSTALL_BIN}？"; then
        sudo cp "${SCRIPT_DIR}/${BINARY_NAME}" "$INSTALL_BIN"
        ok "已安装到 ${INSTALL_BIN}"
    else
        info "跳过安装到 PATH（可手动运行 ${SCRIPT_DIR}/${BINARY_NAME}）"
    fi
fi

# Step 3: 选择 skills 安装方式
echo ""
echo -e "${BOLD}Skills 安装方式：${NC}"
echo "  [1] 全局安装 — 复制到 ~/.claude/skills/，所有项目可用"
echo "  [2] 项目级安装 — 复制到指定项目的 .claude/skills/"
echo "  [3] 跳过 — 不安装 skills"
echo ""
read -rp "$(echo -e "${BOLD}选择 [1/2/3]: ${NC}")" choice

case "$choice" in
    1)
        TARGET_DIR="$HOME/.claude/skills"
        mkdir -p "$TARGET_DIR"
        info "安装到 ${TARGET_DIR}"
        echo ""
        for skill in "${SKILLS[@]}"; do
            copy_skill "${SCRIPT_DIR}/skills/${skill}" "${TARGET_DIR}/${skill}"
        done
        ;;
    2)
        read -rp "$(echo -e "${BOLD}输入目标项目根目录路径: ${NC}")" project_root
        project_root="${project_root/#\~/$HOME}"
        if [[ ! -d "$project_root" ]]; then
            err "目录不存在: $project_root"
            exit 1
        fi
        TARGET_DIR="${project_root}/.claude/skills"
        mkdir -p "$TARGET_DIR"
        info "安装到 ${TARGET_DIR}"
        echo ""
        for skill in "${SKILLS[@]}"; do
            copy_skill "${SCRIPT_DIR}/skills/${skill}" "${TARGET_DIR}/${skill}"
        done
        ;;
    3)
        info "跳过 skills 安装"
        ;;
    *)
        warn "无效选择，跳过 skills 安装"
        ;;
esac

# Step 4: Chrome Extension 安装引导
echo ""
echo -e "${BOLD}═══ Chrome Extension 安装 ═══${NC}\n"
info "Chrome Extension 需要手动加载到浏览器："
echo ""
echo -e "  ${YELLOW}1.${NC} 打开 Chrome，访问 ${BOLD}chrome://extensions${NC}"
echo -e "  ${YELLOW}2.${NC} 打开右上角 ${BOLD}开发者模式${NC}"
echo -e "  ${YELLOW}3.${NC} 点击 ${BOLD}加载已解压的扩展程序${NC}"
echo -e "  ${YELLOW}4.${NC} 选择目录: ${BOLD}${SCRIPT_DIR}/extension${NC}"
echo ""

if confirm "是否现在打开 chrome://extensions？"; then
    if command -v open &>/dev/null; then
        open "chrome://extensions"
    elif command -v xdg-open &>/dev/null; then
        xdg-open "chrome://extensions"
    else
        info "请手动在 Chrome 中打开 chrome://extensions"
    fi
fi

# Step 5: 使用引导
echo ""
echo -e "${BOLD}═══ 使用方法 ═══${NC}\n"
echo -e "  ${YELLOW}1.${NC} 启动守护进程："
echo -e "     ${BOLD}chrome-pilot start${NC}"
echo ""
echo -e "  ${YELLOW}2.${NC} Extension 会自动连接（无需手动配置 token）"
echo ""
echo -e "  ${YELLOW}3.${NC} 检查连接状态："
echo -e "     ${BOLD}chrome-pilot status${NC}"
echo ""
echo -e "  ${YELLOW}4.${NC} 开始使用："
echo -e "     ${BOLD}chrome-pilot snapshot${NC}      # 获取页面快照"
echo -e "     ${BOLD}chrome-pilot tab list${NC}      # 列出所有 tab"
echo -e "     ${BOLD}chrome-pilot dom click --ref e1${NC}  # 点击元素"
echo ""

echo -e "${GREEN}安装完成！${NC}"
