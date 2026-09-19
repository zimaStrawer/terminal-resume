#!/usr/bin/env python3
"""预览用的确定性对话后端。

真正的 AI 后端在作品集站的 `functions/api/chat.ts`（Cloudflare Pages Function），
预览不能依赖它——网络一通、模型一换，`docs/tui-preview.html` 就不可复现了。
所以这里按同一套 SSE 协议回一段**固定**长文，让「自由提问 -> 流式回答」
这条链路也能进预览，且每次跑出来都一样。

协议对齐 functions/api/chat.ts：
    POST /api/chat  {messages:[{role,content}]}
    -> text/event-stream
    -> data: {"choices":[{"delta":{"content":"..."}}]}
    -> data: [DONE]

用法：python3 scripts/chat-stub.py [port]     默认 8799
"""

import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# 预览里那句话。刻意用一条**不含换行**的长句——真实 AI 回答就是这样下发的，
# 它正是「只截断不折行」那个 bug 的触发条件，预览里要能看出来。
ANSWER = (
    "张思洲的精选项目包括：跑腿全链路优化、构序云体验适配、艺术画境产品设计以及 NeckDisco 创新应用。"
    "这些案例涵盖从 C 端消费体验到 B 端企业级平台，再到 AI 原生产品与空间音频交互的探索。"
    "每个项目都聚焦于解决复杂问题，通过设计策略与工程实现提升用户体验。"
    "如需了解具体细节，可以继续问我某个项目的取舍，或直接访问作品集的项目页面。"
)

# (起点, 终点, 发送前等待秒数)。按下标切 rune，分片长度不均匀，模拟模型一片片吐字。
#
# 第二片前那次长停顿是**故意**的：预览要抓一张「回答只打出一半」的快照，
# 靠的就是它——否则桩一发完，屏幕上只剩完成态，看不出流式过程。
CHUNKS = [
    (0, 39, 0.0),
    (39, 54, 1.2),  # 长停顿：此刻只收到了前 39 字
    (54, 78, 0.12),
    (78, 130, 0.12),
    (130, 9999, 0.12),
]


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        self.rfile.read(length)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream; charset=utf-8")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "close")
        self.end_headers()

        runes = list(ANSWER)
        for start, end, pause in CHUNKS:
            piece = "".join(runes[start:end])
            if not piece:
                continue
            time.sleep(pause)
            event = {"choices": [{"index": 0, "delta": {"content": piece}, "finish_reason": None}]}
            self.wfile.write(("data: " + json.dumps(event, ensure_ascii=False) + "\n\n").encode())
            self.wfile.flush()
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()

    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", "2")
        self.end_headers()
        self.wfile.write(b"ok")

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8799
    print(f"chat stub on http://127.0.0.1:{port}", flush=True)
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
