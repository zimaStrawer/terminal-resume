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

# cleanup 挂在 EXIT trap 上：程序无论怎么退出（正常 / Ctrl+C / 关窗口）都会删掉
# 临时目录，并在「程序真的启动过」时回报一句，让访客知道本机不留副本。
#
# LAUNCHED 这个标志是必需的：下载失败、脚本提前退出时 EXIT trap 同样会跑，
# 那种情况下不该报「已清理」——否则它会紧跟在「下载失败」后面，像在说成功了。
LAUNCHED=0
cleanup() {
  rm -rf "$TMP"
  if [[ "$LAUNCHED" == "1" ]]; then
    printf '\n  %b✓%b 本次下载的临时文件已清理，本机不留副本\n' "$C" "$R"
  fi
}
trap cleanup EXIT

# 主题色 #16B8F3 真彩色 ANSI 前缀
C="\033[38;2;22;184;243m"
R="\033[0m"

printf 'Starting SIZHOU Terminal...\n'

# 后台下载（静默写文件）；响应头随手落盘 —— 200 响应头自带全量 content-length，
# 主循环从这份头文件里轮询总长。**不要**再单独发探测请求：Cloudflare Pages
# 忽略 Range（对 Range 0-0 也回 200 全量）、对 HEAD 不给 content-length，
# 任何"只拿头"的探测都会退化成把整个 ~9.5MB 文件完整下载一遍再扔掉
# （实测 3.6s+，期间界面零反馈），等于访客每次都要下载两份。
curl -fsSL --retry 3 --connect-timeout 15 "$URL" -D "${TMP}/headers" -o "${TMP}/${BIN_NAME}" &
CURL_PID=$!

# 主题色块进度条：用 █ 填充已完成部分，右侧显示真实百分比
W=30  # 色块总宽（字符数）
# fmt_mb 把字节数格式化成「N.NMB」（一位小数），供进度条旁显示 (已下载/总量)。
fmt_mb() {
  local b=${1:-0}
  printf '%d.%d' $(( b / 1048576 )) $(( (b % 1048576) * 10 / 1048576 ))
}
printf '\033[?25l'  # 隐藏光标，进度条更干净
last_pct=-1
TOTAL=""
while kill -0 "$CURL_PID" 2>/dev/null; do
  # 响应头一到就解析总长（只解析一次）；没拿到之前先走旋转指示，
  # 这样下载一启动进度指示就出现，不用等任何探测往返。
  if [[ -z "${TOTAL}" && -s "${TMP}/headers" ]]; then
    TOTAL="$(tr -d '\r' < "${TMP}/headers" | awk 'tolower($1)=="content-length:"{print $2}' | tail -1)"
  fi
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
    printf '\r  %b%s%b %3d%% (%sMB/%sMB)' "$C" "$bar" "$R" "$pct" "$(fmt_mb "$done_bytes")" "$(fmt_mb "$TOTAL")"
    last_pct=$pct
  elif [[ $pct -lt 0 ]]; then
    # 旋转指示（真实下载中，总量未知）：带上已下载量，等待阶段也有真实反馈
    sp="⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
    i=$(( (done_bytes / 1024) % 10 ))
    printf '\r  %b%s%b %sMB' "$C" "${sp:i:1}" "$R" "$(fmt_mb "$done_bytes")"
  fi
  sleep 0.05
done
# 收尾：补到 100%（总量未知时用最终文件大小补齐分母）
final_bytes=0
[[ -f "${TMP}/${BIN_NAME}" ]] && final_bytes=$(wc -c < "${TMP}/${BIN_NAME}")
printf '\r  %b%s%b %3d%% (%sMB/%sMB)\n' "$C" "$(printf '%*s' "$W" '' | tr ' ' '█')" "$R" 100 "$(fmt_mb "$final_bytes")" "$(fmt_mb "${TOTAL:-$final_bytes}")"
printf '\033[?25h'  # 恢复光标

# ⚠️ 不能用裸 `wait`：下载失败时 wait 会返回非零，set -e 直接让脚本退出，
# 下面那句「下载失败」永远打不出来（2026-09-22 实测 404：只剩 curl 自己的
# 英文报错，访客看到的是一根跑满 100% 的进度条 + 报错，然后什么都没有）。
# 放进 `||` 列表里，失败就不会触发 set -e。
CURL_EXIT=0
wait "$CURL_PID" || CURL_EXIT=$?
if [[ $CURL_EXIT -ne 0 ]]; then
  echo "下载失败，请检查网络后重试。" >&2
  exit 1
fi

chmod +x "${TMP}/${BIN_NAME}"

# 直接启动（本地 TUI，不启 SSH）
#
# ⚠️ 这里**不能**用 exec：exec 会用新程序替换掉当前 bash 进程，而上面的
# `trap cleanup EXIT` 是 shell 级机制 —— 进程被顶掉之后它永远不会触发，
# 结果是每跑一次就在临时目录留下一份 ~9.5 MB 的二进制（实测 16 次 = 153 MB）。
# 普通调用即可：bash 会等程序退出，然后 EXIT trap 正常清理并回报（见 cleanup）。
#
# LAUNCHED=1 必须在启动**之前**置位：cleanup 靠它区分「程序真的跑起来了」
# 和「下载/校验阶段就失败了」，后者不该报「已清理」。
LAUNCHED=1
"${TMP}/${BIN_NAME}" --local
