#!/usr/bin/env bash
#
# sizhou.ai 终端简历 —— 一键下载并启动（不安装）
#
# 用法:
#   curl -fsSL https://zhangsizhou.pages.dev/ai.sh | bash
#
# 域名 sizhou.ai 就绪后可缩短为:
#   curl -fsSL sizhou.ai | bash
#
set -euo pipefail

# 二进制托管在 Cloudflare Pages（zhangsizhou.pages.dev），不依赖 GitHub
BASE_URL="${SIZHOU_BASE_URL:-https://zhangsizhou.pages.dev}"
BIN_NAME="sizhou-resume"

# 检测平台
detect_target() {
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    MINGW*|MSYS*|CYGWIN*)
      echo "Windows 请到 https://zhangsizhou.pages.dev 下载 .exe 运行" >&2
      exit 1 ;;
    *) echo "暂不支持的系统: $(uname -s)" >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "暂不支持的架构: $(uname -m)" >&2; exit 1 ;;
  esac
  echo "${os}-${arch}"
}

TARGET="$(detect_target)"

URL="${BASE_URL}/terminal-resume-${TARGET}"

# 下载到临时目录，跑完自动清理
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# 提示 + 进度条：二进制约 10MB，网络慢时下载要几秒，静默会显得「卡住」
printf '正在下载终端简历（约 10MB）…\n'
# -f 失败静默退出；--progress-bar 把进度条打到 stderr（不污染 stdout，TUI 不受影响）
if ! curl -fL --retry 3 --connect-timeout 15 --progress-bar "$URL" -o "${TMP}/${BIN_NAME}"; then
  echo "下载失败，请检查网络后重试。" >&2
  exit 1
fi
chmod +x "${TMP}/${BIN_NAME}"
printf '下载完成，正在启动…\n\n'

# 直接启动（本地 TUI，不启 SSH）
exec "${TMP}/${BIN_NAME}" --local
