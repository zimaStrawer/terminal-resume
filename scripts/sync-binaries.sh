#!/usr/bin/env bash
#
# 重编 6 平台二进制，并同步到「线上托管源」。
#
# 二进制有两个落点，漏同步任一处，线上就会一直跑旧版本：
#   1. npm/bin/native/      源头。npm publish 时 prepack 会自己重建，所以不必提交进 git
#                           （本仓库 .gitignore 已忽略它）
#   2. <作品集站>/public/    线上托管源。ai.sh / ai.ps1 从 zhangsizhou.pages.dev 下载的就是
#                           这里的文件，它被 git 追踪，**必须 commit + push 才会上线**
#
# 用法:
#   bash scripts/sync-binaries.sh
#   PORTFOLIO_PUBLIC=/path/to/public bash scripts/sync-binaries.sh
#
# ⚠️ 本脚本只完成「本地同步」。线上真正换版本还差最后一步：
#    在作品集站仓库 commit 这几个二进制并 push 到 main（会触发 Cloudflare Pages 重建）。
#    脚本结束时会打印待执行的命令。
#
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE_DIR="$PROJECT_ROOT/npm/bin/native"
PORTFOLIO_PUBLIC="${PORTFOLIO_PUBLIC:-$HOME/Documents/Zima_room2.0/public}"

TARGETS=(
  terminal-resume-darwin-amd64
  terminal-resume-darwin-arm64
  terminal-resume-linux-amd64
  terminal-resume-linux-arm64
  terminal-resume-win32-amd64.exe
  terminal-resume-win32-arm64.exe
)

if [[ ! -d "$PORTFOLIO_PUBLIC" ]]; then
  echo "目标目录不存在: $PORTFOLIO_PUBLIC" >&2
  echo "用 PORTFOLIO_PUBLIC=/path/to/public 指定，或先创建它。" >&2
  exit 1
fi

echo "==> 1/3 交叉编译到源头 $SOURCE_DIR"
node "$PROJECT_ROOT/npm/scripts/build-native.mjs"

echo
echo "==> 2/3 同步到线上托管源 $PORTFOLIO_PUBLIC"
OLD_HASHES=$(cd "$PORTFOLIO_PUBLIC" && shasum -a 256 "${TARGETS[@]}" 2>/dev/null || true)
for f in "${TARGETS[@]}"; do
  cp "$SOURCE_DIR/$f" "$PORTFOLIO_PUBLIC/$f"
  chmod 755 "$PORTFOLIO_PUBLIC/$f"
done

echo
echo "==> 3/3 校验两处 sha256 一致"
FAILED=0
for f in "${TARGETS[@]}"; do
  a=$(shasum -a 256 "$SOURCE_DIR/$f" | awk '{print $1}')
  b=$(shasum -a 256 "$PORTFOLIO_PUBLIC/$f" | awk '{print $1}')
  if [[ "$a" == "$b" ]]; then
    printf '  [OK]   %-38s %s\n' "$f" "${a:0:12}"
  else
    printf '  [FAIL] %-38s 源头 %s != 线上源 %s\n' "$f" "${a:0:12}" "${b:0:12}"
    FAILED=1
  fi
done

# 顺手确认与上一版确实不同：hash 没变说明源码没重编生效，多半是白跑一趟
if [[ -n "$OLD_HASHES" ]]; then
  CHANGED=0
  for f in "${TARGETS[@]}"; do
    new=$(shasum -a 256 "$PORTFOLIO_PUBLIC/$f" | awk '{print $1}')
    if ! grep -q "^$new " <<<"$OLD_HASHES"; then
      CHANGED=1
    fi
  done
  if [[ "$CHANGED" -eq 0 ]]; then
    echo
    echo "  ⚠️  所有 hash 与同步前完全相同 —— 源码可能没有实际改动，线上不会有任何变化。"
  fi
fi

# ai.sh / ai.ps1 也是两处共存的下载入口，只提示差异、不自动覆盖（避免误改线上的）
for s in ai.sh ai.ps1; do
  if [[ -f "$PROJECT_ROOT/$s" && -f "$PORTFOLIO_PUBLIC/$s" ]]; then
    if ! cmp -s "$PROJECT_ROOT/$s" "$PORTFOLIO_PUBLIC/$s"; then
      echo
      echo "  ⚠️  $s 两处不一致（本仓库 vs 线上源），需人工确认以哪边为准："
      echo "       diff $PROJECT_ROOT/$s $PORTFOLIO_PUBLIC/$s"
    fi
  fi
done

echo
if [[ "$FAILED" -ne 0 ]]; then
  echo "同步校验失败。" >&2
  exit 1
fi

echo "本地同步完成。线上要生效，还差最后一步（在作品集站仓库执行）："
echo
echo "    cd $(dirname "$PORTFOLIO_PUBLIC")"
echo "    git add public/terminal-resume-darwin-* public/terminal-resume-linux-* public/terminal-resume-win32-*"
echo "    git commit -m '终端简历二进制同步更新'"
echo "    git push origin main"
echo
echo "push 到 main 会触发 Cloudflare Pages 重建，之后可抽查线上是否已换新："
echo "    curl -s https://zhangsizhou.pages.dev/terminal-resume-darwin-arm64 | shasum -a 256"
