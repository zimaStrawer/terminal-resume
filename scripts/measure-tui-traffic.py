#!/usr/bin/env python3
"""量 TUI 在终端上的真实开销：每秒往 pty 写多少字节。

用法：
    python3 scripts/measure-tui-traffic.py ./terminal-resume          # 空闲 6 秒
    python3 scripts/measure-tui-traffic.py ./terminal-resume 30       # 连打 30 个字符

为什么量这个：TUI 的「重绘开销」对用户（尤其 SSH 访问的访客）就等于
「每秒往终端写多少字节」。输入框那个提示字符动画的唯一代价就是多刷了几帧，
所以这个数字直接回答了「动画值不值得开」。

怎么读：
- 空闲（动画关）应当是 0 —— 没有动画时程序完全静默，不会占带宽。
- 空闲（动画开）≈1.4 KB/s：乱序阶段 64ms 一帧，且每帧只改一个单元格。
- 打字场景两版的差值应当≈「空闲（动画开）」那一行，说明**每次按键没有额外成本**
  （打字自己的回显开销两版相同）。

注意：先 warmup 2.5 秒再开始计数，避开首屏整屏重绘那一大坨字节。
"""
import fcntl
import os
import pty
import select
import struct
import sys
import termios
import time

COLS, ROWS = 100, 30
WARMUP = 2.5
WINDOW = 6.0
TYPE_GAP = 0.12


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(2)
    binary = sys.argv[1]
    type_chars = int(sys.argv[2]) if len(sys.argv) > 2 else 0

    pid, fd = pty.fork()
    if pid == 0:
        os.environ["TERM"] = "xterm-256color"
        os.environ["COLORTERM"] = "truecolor"
        os.environ["COLUMNS"] = str(COLS)
        os.environ["LINES"] = str(ROWS)
        os.environ.pop("NO_COLOR", None)
        os.execv(binary, [binary, "--local"])

    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    total = 0

    def drain(seconds):
        nonlocal total
        end = time.time() + seconds
        while True:
            remaining = end - time.time()
            if remaining <= 0:
                return
            ready, _, _ = select.select([fd], [], [], min(remaining, 0.05))
            if not ready:
                continue
            try:
                data = os.read(fd, 1 << 20)
            except OSError:
                return
            if not data:
                return
            total += len(data)

    drain(WARMUP)
    before = total
    start = time.time()

    if type_chars:
        for index in range(type_chars):
            os.write(fd, b"abcdefghij"[index % 10 : index % 10 + 1])
            drain(TYPE_GAP)
        drain(0.6)
    else:
        drain(WINDOW)

    elapsed = time.time() - start
    written = total - before
    scene = f"打字 {type_chars} 字符" if type_chars else "空闲        "
    print(
        f"{os.path.basename(binary):24s} {scene} "
        f"写入 {written:7d} 字节 / {elapsed:4.1f}s = {written / elapsed:8.1f} 字节/秒 "
        f"({written / elapsed / 1024:.2f} KB/s)"
    )

    os.kill(pid, 9)
    os.close(fd)


if __name__ == "__main__":
    main()
