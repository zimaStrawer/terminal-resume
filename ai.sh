#!/usr/bin/env bash
#
# sizhou.ai 终端简历 —— 一键下载并启动（不安装）
#
# 用法:
#   curl -fsSL https://raw.githubusercontent.com/zimaStrawer/terminal-resume/main/ai.sh | bash
#
# 域名 sizhou.ai 就绪后可缩短为:
#   curl -fsSL sizhou.ai | bash
#
set -euo pipefail

GITHUB_REPO="zimaStrawer/terminal-resume"
VERSION="${SIZHOU_VERSION:-latest}"
BIN_NAME="sizhou-resume"

# 检测平台
detect_target() {
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    MINGW*|MSYS*|CYGWIN*)
      echo "Windows 请到 https://github.com/${GITHUB_REPO}/releases 下载 .exe 运行" >&2
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

if [[ "$VERSION" == "latest" ]]; then
  URL="https://github.com/${GITHUB_REPO}/releases/latest/download/terminal-resume-${TARGET}"
else
  URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/terminal-resume-${TARGET}"
fi

# 下载到临时目录，跑完自动清理
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

curl -fsSL --retry 3 --connect-timeout 15 "$URL" -o "${TMP}/${BIN_NAME}"
chmod +x "${TMP}/${BIN_NAME}"

# 直接启动（本地 TUI，不启 SSH）
exec "${TMP}/${BIN_NAME}" --local
