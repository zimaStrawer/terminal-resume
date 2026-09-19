#!/usr/bin/env bash
#
# sizhou.ai 终端简历 —— 一行命令安装脚本
#
# 用法:
#   curl -fsSL https://raw.githubusercontent.com/zimaStrawer/terminal-resume/main/install.sh | bash
#
# 装完后在终端里敲:
#   sizhou-resume
#
# 说明:
#   - 自动检测当前系统与 CPU 架构，从 GitHub Releases 下载对应二进制
#   - 安装到 ~/.local/bin（无需 sudo），并自动加入 PATH
#   - 不依赖 npm / node / go，任何有 curl 和 bash 的环境都能装
#
set -euo pipefail

# ─── 可调参数 ──────────────────────────────────────────────────────────────
GITHUB_REPO="zimaStrawer/terminal-resume"
INSTALL_VERSION="${SIZHOU_VERSION:-latest}"   # 默认装最新版，可 SIZHOU_VERSION=v0.1.0 指定版本
BIN_NAME="sizhou-resume"
INSTALL_DIR="${SIZHOU_INSTALL_DIR:-$HOME/.local/bin}"

# ─── 配色（仅在终端时输出） ───────────────────────────────────────────────
if [[ -t 1 ]]; then
  C_RESET=$'\033[0m'; C_BOLD=$'\033[1m'; C_DIM=$'\033[2m'
  C_RED=$'\033[0;31m'; C_GREEN=$'\033[0;32m'; C_CYAN=$'\033[0;36m'
else
  C_RESET=''; C_BOLD=''; C_DIM=''; C_RED=''; C_GREEN=''; C_CYAN=''
fi

info()  { printf '%s\n' "${C_CYAN}==>${C_RESET} ${C_BOLD}$*${C_RESET}"; }
ok()    { printf '%s\n' "${C_GREEN}✓${C_RESET} $*"; }
warn()  { printf '%s\n' "${C_RED}⚠${C_RESET} $*"; }
die()   { printf '%s\n' "${C_RED}✗ $*${C_RESET}" >&2; exit 1; }

# ─── 检测平台 ──────────────────────────────────────────────────────────────
detect_target() {
  local os arch
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    *)
      # Windows (Git Bash / MSYS / Cygwin)
      case "$(uname -s)" in
        MINGW*|MSYS*|CYGWIN*) os="win32" ;;
        *) die "暂不支持的系统: $(uname -s)。目前支持 macOS / Linux / Windows。" ;;
      esac
      ;;
  esac

  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) die "暂不支持的 CPU 架构: $(uname -m)。目前支持 amd64 / arm64。" ;;
  esac

  TARGET="${os}-${arch}"
}

# ─── 解析下载地址 ──────────────────────────────────────────────────────────
# 最新版用 /releases/latest/download/<file>，指定版本用 /releases/download/<tag>/<file>
resolve_url() {
  local file="$1"
  if [[ "$INSTALL_VERSION" == "latest" ]]; then
    echo "https://github.com/${GITHUB_REPO}/releases/latest/download/${file}"
  else
    echo "https://github.com/${GITHUB_REPO}/releases/download/${INSTALL_VERSION}/${file}"
  fi
}

# ─── 主流程 ────────────────────────────────────────────────────────────────
main() {
  info "sizhou.ai 终端简历 · 安装器"
  echo

  detect_target
  local suffix=""
  [[ "$TARGET" == win32-* ]] && suffix=".exe"

  # Windows 下走别的方式提示（Git Bash 也能跑，但二进制是 .exe，直接提示手动下载）
  if [[ "$TARGET" == win32-* ]]; then
    die "检测到 Windows。请在浏览器打开以下地址下载对应版本，双击运行：\n     ${C_BOLD}https://github.com/${GITHUB_REPO}/releases/latest${C_RESET}"
  fi

  local asset="terminal-resume-${TARGET}"
  local url
  url="$(resolve_url "$asset")"

  info "目标平台: ${C_BOLD}${TARGET}${C_RESET}"
  info "安装位置: ${C_BOLD}${INSTALL_DIR}${C_RESET}"
  echo

  # 下载
  info "正在下载 ${C_DIM}${url}${C_RESET}"
  local tmp
  tmp="$(mktemp -d)"
  if ! curl -fsSL --retry 3 --connect-timeout 15 "$url" -o "${tmp}/${BIN_NAME}"; then
    rm -rf "$tmp"
    die "下载失败。请确认网络可访问 GitHub，或手动到 Releases 页下载：\n     ${C_BOLD}https://github.com/${GITHUB_REPO}/releases${C_RESET}"
  fi
  chmod +x "${tmp}/${BIN_NAME}"

  # 安装
  mkdir -p "$INSTALL_DIR"
  if ! mv -f "${tmp}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"; then
    rm -rf "$tmp"
    die "写入 ${INSTALL_DIR} 失败。可设置 SIZHOU_INSTALL_DIR 指定其他目录后重试。"
  fi
  rm -rf "$tmp"

  ok "已安装 ${C_BOLD}${INSTALL_DIR}/${BIN_NAME}${C_RESET} (${TARGET})"
  echo

  # PATH 处理
  local profile_file=""
  if [[ -f "$HOME/.zshrc" ]]; then profile_file="$HOME/.zshrc";
  elif [[ -f "$HOME/.bashrc" ]]; then profile_file="$HOME/.bashrc";
  elif [[ -f "$HOME/.bash_profile" ]]; then profile_file="$HOME/.bash_profile"; fi

  if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    if [[ -n "$profile_file" ]]; then
      if ! grep -qF "$INSTALL_DIR" "$profile_file" 2>/dev/null; then
        printf '\n# sizhou.ai 终端简历\nexport PATH="$PATH:%s"\n' "$INSTALL_DIR" >> "$profile_file"
      fi
      ok "已将 ${INSTALL_DIR} 加入 PATH（写入 ${profile_file}）"
    else
      warn "未找到 shell 配置文件，请手动把 ${INSTALL_DIR} 加入 PATH："
      warn "    export PATH=\"\$PATH:${INSTALL_DIR}\""
    fi
  else
    ok "${INSTALL_DIR} 已在 PATH 中"
  fi

  echo
  info "安装完成！现在可以这样启动："
  echo
  printf '  %s\n' "${C_BOLD}  sizhou-resume${C_RESET}"
  echo
  info "如果命令未生效，先执行："
  printf '  %s\n' "${C_DIM}  source ${profile_file:-~/.zshrc}${C_RESET}"
  echo
}

main "$@"
