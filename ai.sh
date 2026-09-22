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

# 主题色 #16B8F3 真彩色 ANSI 前缀
C="\033[38;2;22;184;243m"
R="\033[0m"

printf 'Starting SIZHOU Terminal...\n'

# 预取总长：Cloudflare 对 HEAD(-I) 不返回 content-length，改用 Range 0-0 请求拿全量长度
TOTAL="$(curl -fsSL -r 0-0 -o /dev/null -D - "$URL" 2>/dev/null | tr -d '\r' | awk 'tolower($1)=="content-length:"{print $2}' | tail -1)"
[[ -z "${TOTAL}" ]] && TOTAL="$(curl -fsSL -o /dev/null -D - "$URL" 2>/dev/null | tr -d '\r' | awk 'tolower($1)=="content-length:"{print $2}' | tail -1)"

# 后台下载（静默写文件），主循环轮询已下载字节数画主题色块进度条
curl -fsSL --retry 3 --connect-timeout 15 "$URL" -o "${TMP}/${BIN_NAME}" &
CURL_PID=$!

# 主题色块进度条：用 █ 填充已完成部分，右侧显示真实百分比
W=30  # 色块总宽（字符数）
printf '\033[?25l'  # 隐藏光标，进度条更干净
last_pct=-1
while kill -0 "$CURL_PID" 2>/dev/null; do
  if [[ -f "${TMP}/${BIN_NAME}" ]]; then
    done_bytes=$(wc -c "${TMP}/${BIN_NAME}" 2>/dev/null | awk '{print $1}')
  else
    done_bytes=0
  fi
  done_bytes=${done_bytes:-0}
  if [[ -n "${TOTAL}" && "${TOTAL}" -gt 0 ]]; then
    pct=$(( done_bytes * 100 / TOTAL ))
    [[ $pct -gt 100 ]] && pct=100
  else
    # 无总长：按「已下载字节」转成旋转帧，不做假进度
    pct=-1
  fi
  if [[ $pct -ge 0 && $pct -ne $last_pct ]]; then
    fill=$(( pct * W / 100 ))
    empty=$(( W - fill ))
    bar=""
    [[ $fill -gt 0 ]] && bar+="$(printf '%*s' "$fill" '' | tr ' ' '█')"
    [[ $empty -gt 0 ]] && bar+="$(printf '%*s' "$empty" '' | tr ' ' '░')"
    printf '\r  %b%s%b %3d%%' "$C" "$bar" "$R" "$pct"
    last_pct=$pct
  elif [[ $pct -lt 0 ]]; then
    # 旋转指示（真实下载中，只是未知总量）
    sp="⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
    i=$(( (done_bytes / 1024) % 10 ))
    printf '\r  %b%s%b' "$C" "${sp:i:1}" "$R"
  fi
  sleep 0.05
done
# 收尾：补到 100%
printf '\r  %b%s%b %3d%%\n' "$C" "$(printf '%*s' "$W" '' | tr ' ' '█')" "$R" 100
printf '\033[?25h'  # 恢复光标

wait "$CURL_PID"
CURL_EXIT=$?
if [[ $CURL_EXIT -ne 0 ]]; then
  echo "下载失败，请检查网络后重试。" >&2
  exit 1
fi

chmod +x "${TMP}/${BIN_NAME}"

# 直接启动（本地 TUI，不启 SSH）
#
# ⚠️ 这里**不能**用 exec：exec 会用新程序替换掉当前 bash 进程，而上面的
# `trap 'rm -rf "$TMP"' EXIT` 是 shell 级机制 —— 进程被顶掉之后它永远不会触发，
# 结果是每跑一次就在临时目录留下一份 ~9.5 MB 的二进制（实测 16 次 = 153 MB）。
# 普通调用即可：bash 会等程序退出，然后 EXIT trap 正常清理。
"${TMP}/${BIN_NAME}" --local
