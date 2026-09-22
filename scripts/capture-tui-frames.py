#!/usr/bin/env python3
"""驱动 terminal-resume 并把每一步「按键之后」的累计原始字节流存成 JSON。

屏幕还原交给 xterm.js（Node 侧，见 scripts/render-tui-preview.js）——
pyte 在「菜单开 → 关」这种大面积重排时会留下假残影（把回答画到 logo 行上），
真实终端并没有。不要用 pyte 下结论。

用法：
    python3 scripts/capture-tui-frames.py [输出路径]
默认输出 /tmp/tui_frames.json。
"""

import base64
import fcntl
import json
import os
import pty
import select
import socket
import struct
import subprocess
import sys
import termios
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = os.path.join(ROOT, "terminal-resume")
OUT = sys.argv[1] if len(sys.argv) > 1 else "/tmp/tui_frames.json"


def run_session(cols, rows, steps, extra_args=(), initial=1.8, gap=0.6, settle=3.2):
    """steps 元素为 (label, action[, settle])。

    action 传 bytes 列表 = 按下这些键后快照；传 float = 等待这么多秒后快照。
    列表里也可以混 float，表示「敲到这里停一下再继续」。
    """
    if not os.path.exists(BIN):
        sys.exit(f"没有找到二进制 {BIN}，先跑 make build")

    pid, fd = pty.fork()
    if pid == 0:
        os.environ["TERM"] = "xterm-256color"
        os.environ["COLORTERM"] = "truecolor"
        os.environ["COLUMNS"] = str(cols)
        os.environ["LINES"] = str(rows)
        os.environ.pop("NO_COLOR", None)
        os.execv(BIN, [BIN, "--local", *extra_args])
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))

    buffer = b""

    def drain(duration):
        nonlocal buffer
        end = time.time() + duration
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
            buffer += data

    def stop():
        try:
            os.write(fd, b"\x03")
            time.sleep(0.2)
            os.close(fd)
        except OSError:
            pass
        try:
            os.waitpid(pid, 0)
        except ChildProcessError:
            pass

    frames = []
    drain(initial)
    frames.append(("启动首页", buffer))
    for step in steps:
        label, action = step[0], step[1]
        step_settle = step[2] if len(step) > 2 and step[2] is not None else settle
        if isinstance(action, (int, float)):
            drain(action)
        else:
            for key in action:
                if isinstance(key, (int, float)):
                    drain(key)
                    continue
                os.write(fd, key)
                drain(gap)
            drain(step_settle)
        frames.append((label, buffer))
    stop()
    return frames


def session(label_prefix, cols, rows, steps, extra_args=()):
    return [
        {
            "label": label_prefix + label,
            "cols": cols,
            "rows": rows,
            "stream": base64.b64encode(buf).decode(),
        }
        for label, buf in run_session(cols, rows, steps, extra_args)
    ]


STUB_PORT = 8799
STUB_SCRIPT = os.path.join(ROOT, "scripts", "chat-stub.py")

# 不涉及自由问答的会话一律 --api off：预览里不许有任何一个请求跑到真实网络上，
# 否则网络一通、模型一换，预览就不可复现了。
#
# --report off 同理，而且更容易被漏掉：会话上报默认沿用 profile.website（线上站点），
# 于是「只是截几张图」会把一条条假会话写进线上、最后变成发给作者的邮件。
OFFLINE = ("--api", "off", "--report", "off")


class ChatStub:
    """预览用的确定性对话后端（见 scripts/chat-stub.py）。

    自由问答真正的后端在作品集站的 /api/chat，预览不能依赖它——
    回答是模型现场生成的，每次都不一样，根本没法复现。
    所以预览时起一个本地桩，按同一套 SSE 协议回一段固定长文。
    """

    def __init__(self, port=STUB_PORT):
        self.port = port
        self.url = f"http://127.0.0.1:{port}"
        self.process = None

    def __enter__(self):
        self.process = subprocess.Popen(
            [sys.executable, STUB_SCRIPT, str(self.port)],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        self._wait_ready()
        return self

    def __exit__(self, *exception):
        if self.process is None:
            return
        self.process.terminate()
        try:
            self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.process.kill()

    def _wait_ready(self, timeout=10.0):
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                with socket.create_connection(("127.0.0.1", self.port), timeout=0.3):
                    return
            except OSError:
                time.sleep(0.1)
        sys.exit("对话桩没起来，先手动跑 python3 scripts/chat-stub.py 看看报错")


def main():
    frames = []

    # 1) 对话流主链路：logo + 收尾文案在上，回答在下方累积，把两者一起往上顶
    frames += session("", 100, 30, [
        ("按 / 打开下拉框 · 四条快捷命令落在输入框上方", [b"/"]),
        ("执行 /skills · 收尾文案随 logo 一起被顶到回答上方", [b"\r"]),
        ("执行 /personal · 收尾文案与 logo 一起被顶出屏幕", [b"/", b"\x1b[B", b"\x1b[B", b"\x1b[B", b"\r"]),
    ], extra_args=OFFLINE)

    # 2) 流式打印的中间态 + 其余长命令
    frames += session("", 100, 30, [
        ("执行 /help · 回答正在流式打印（约三分之一）", [b"/help\r"], 0.0),
        ("/help 回答完成", []),
        ("执行 /resume", [b"/resume\r"]),
        ("执行 /log", [b"/log\r"]),
    ], extra_args=OFFLINE)

    # 3) 窄屏
    frames += session("窄屏 · ", 64, 22, [
        ("按 / 打开下拉框", [b"/"]),
        ("执行 /skills · 长回答自动折行", [b"\r"]),
    ], extra_args=OFFLINE)

    # 4) 黑白模式
    frames += session("黑白 · ", 90, 26, [
        ("按 / 打开下拉框", [b"/"]),
        ("执行 /skills", [b"\r"]),
    ], extra_args=("--mono", *OFFLINE))

    # 5) 通知行：输入框下方那一行不再常驻文案，只在有反馈（如未知命令）时才出字
    frames += session("通知 · ", 100, 30, [
        ("输入未知命令 /nope · 提示落在输入框下方，输入框位置不动", [b"/nope\r"]),
    ], extra_args=OFFLINE)

    # 6) 更窄的终端：占位文案自动换成短句（< 62 列）
    frames += session("窄屏短句 · ", 50, 22, [
        ("启动首页 · 占位文案换成短句 按 \"/\" 唤出快捷命令", []),
    ], extra_args=OFFLINE)

    # 7) 自由问答：真实 AI 流式回答进对话区。
    #    桩回了**一条不含换行的长句**——真实模型就是这样下发的，
    #    也正是「只截断不折行」那个 bug 的触发条件，预览里必须能看出它折了行。
    #    第二帧的 settle 刻意压到 0.2s（加上 run_session 每键后的 0.6s，共约 0.8s），
    #    落在桩那次 1.2s 停顿之内，于是它抓到的确实是"只打出一半"的瞬间。
    with ChatStub() as stub:
        question = "他做过哪些项目？".encode()
        frames += session("对话 · ", 100, 30, [
            ("输入一句自由提问", [question], 0.3),
            ("提交后 · 回答正在流式打印", [b"\r"], 0.2),
            ("回答打印完成 · 长句自动折行，一个字都没丢", [], 3.0),
        ], extra_args=("--api", stub.url, "--report", "off"))

    with open(OUT, "w") as handle:
        json.dump(frames, handle)
    print(f"wrote {OUT}: {len(frames)} frames")


if __name__ == "__main__":
    main()
