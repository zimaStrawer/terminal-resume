#!/usr/bin/env python3
"""驱动真实 TUI 二进制、抓最终稳定帧，并可选地把屏幕还原成文本。

为什么需要它：排查「某个命令到底渲染成什么样」「这条错误提示有多宽」时，
`scripts/capture-tui-frames.py` 那套分场景攒帧太重（要起一次服务、跑一堆会话），
而这个工具只做一件事——给一条输入，把最终稳定画面吐出来。

用法：

    # 输入一条命令，还原成屏幕文本
    python3 scripts/drive-tui.py --cmd "/resume" --text

    # 自由提问打本地后端
    python3 scripts/drive-tui.py --ask "他做过哪些项目？" --api http://127.0.0.1:8788 --text

    # 换尺寸 / 只存帧文件
    python3 scripts/drive-tui.py --cmd "/skills" --size 64x22 -o /tmp/skills.json

还原屏幕需要 Node + `@xterm/headless`，用 PREVIEW_NODE / PREVIEW_NODE_PATH 指定，
和 `make preview` 用的是同一套环境变量。
"""
from __future__ import annotations

import argparse
import base64
import fcntl
import json
import os
import pty
import select
import shutil
import struct
import subprocess
import sys
import tempfile
import termios
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BINARY = os.path.join(ROOT, "terminal-resume")

# 把 xterm.js 还原逻辑内嵌，省得再维护一个 js 文件。
# 与 scripts/render-tui-preview.js 的口径一致：按格取字符，宽字符的第二格跳过。
DUMP_JS = r"""
const fs = require('fs');
const { Terminal } = require('@xterm/headless');
const frame = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const term = new Terminal({
  cols: frame.cols, rows: frame.rows, scrollback: 0, allowProposedApi: true,
});
term.write(Buffer.from(frame.stream, 'base64'), () => {
  const buf = term.buffer.active;
  const out = [];
  for (let y = 0; y < frame.rows; y++) {
    const line = buf.getLine(y);
    let text = '';
    if (line) {
      for (let x = 0; x < frame.cols; x++) {
        const cell = line.getCell(x);
        if (!cell || cell.getWidth() === 0) continue;
        text += cell.getChars() || ' ';
      }
    }
    out.push(text.replace(/\s+$/, ''));
  }
  console.log(out.join('\n'));
  term.dispose();
});
"""


def capture(binary: str, line: str, args: list[str], cols: int, rows: int,
            settle: float, warmup: float) -> bytes:
    pid, fd = pty.fork()
    if pid == 0:
        env = dict(os.environ)
        env.update(TERM="xterm-256color", COLORTERM="truecolor",
                   COLUMNS=str(cols), LINES=str(rows))
        env.pop("NO_COLOR", None)
        os.execve(binary, [binary, *args], env)

    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    buffer = bytearray()

    def drain(seconds: float) -> None:
        end = time.time() + seconds
        while time.time() < end:
            ready, _, _ = select.select([fd], [], [], 0.05)
            if not ready:
                continue
            try:
                data = os.read(fd, 1 << 20)
            except OSError:
                return
            if not data:
                return
            buffer.extend(data)

    drain(warmup)
    for ch in line:
        os.write(fd, ch.encode())
        drain(0.04)
    # 留一点间隔再回车：贴太近偶尔会被当成同一个按键批次丢掉。
    drain(0.3)
    if line:
        os.write(fd, b"\r")
    drain(settle)

    try:
        os.write(fd, b"\x03")
        time.sleep(0.3)
        os.close(fd)
    except OSError:
        pass
    try:
        os.waitpid(pid, 0)
    except ChildProcessError:
        pass
    return bytes(buffer)


def render_text(frame_path: str) -> None:
    node = os.environ.get("PREVIEW_NODE") or shutil.which("node")
    if not node:
        print("找不到 node；用 PREVIEW_NODE 指定，或加 --no-text", file=sys.stderr)
        return
    with tempfile.NamedTemporaryFile("w", suffix=".cjs", delete=False) as handle:
        handle.write(DUMP_JS)
        script = handle.name
    env = dict(os.environ)
    if os.environ.get("PREVIEW_NODE_PATH"):
        env["NODE_PATH"] = os.environ["PREVIEW_NODE_PATH"]
    try:
        result = subprocess.run([node, script, frame_path], env=env,
                                capture_output=True, text=True, check=False)
        if result.returncode != 0:
            print(result.stderr.strip(), file=sys.stderr)
            return
        print(result.stdout, end="")
    finally:
        os.unlink(script)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--cmd", help="要输入的一条命令（如 /resume）")
    source.add_argument("--ask", help="要输入的一句自由提问（会打 AI 后端）")
    parser.add_argument("--size", default="100x30", help="终端尺寸，如 100x30")
    parser.add_argument("--settle", type=float, default=2.5, help="回车后等待秒数")
    parser.add_argument("--warmup", type=float, default=2.0, help="首屏等待秒数")
    parser.add_argument("-o", "--out", default="/tmp/tui-frame.json", help="帧输出路径")
    parser.add_argument("--text", action="store_true", help="额外把屏幕还原成文本打印")
    # ⚠️ 二进制自己的参数（--local / --api / --mono …）跟本脚本的参数同名冲突，
    # 所以用 parse_known_args 把它们接住透传，而不是声明成 positionals。
    options, binary_args = parser.parse_known_args()

    if not os.path.exists(BINARY):
        print(f"没找到二进制 {BINARY}，先 make build", file=sys.stderr)
        return 1

    try:
        cols, rows = (int(part) for part in options.size.lower().split("x"))
    except ValueError:
        print("--size 要写成 100x30 这样", file=sys.stderr)
        return 2

    line = options.cmd or options.ask or ""
    args = binary_args or ["--local"]
    if options.ask and "--api" not in args:
        print("--ask 需要自己带 --api（或让它走 profile.website 的默认后端）",
              file=sys.stderr)

    stream = capture(BINARY, line, args, cols, rows, options.settle, options.warmup)
    payload = {
        "label": line or "startup",
        "cols": cols,
        "rows": rows,
        "args": args,
        "stream": base64.b64encode(stream).decode(),
    }
    with open(options.out, "w") as handle:
        json.dump(payload, handle)
    print(f"wrote {options.out} ({len(stream)} bytes)")

    if options.text:
        render_text(options.out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
