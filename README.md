# sizhou.ai — 终端简历

一份在访客电脑的终端中运行的中文交互式简历。视觉语言参考现代 AI TUI：内容居中、底部命令输入、斜杠命令与输入框下方的常驻快捷键提示行。**整个界面只有一块屏**：上方是对话（内容越攒越多就把 logo 顶出屏幕），底部是钉住的输入框。

## 一句话启动（推荐）

无需 npm / node / go，任何有 `curl` 和 `bash` 的环境都能跑：

```bash
curl -fsSL https://zhangsizhou.pages.dev/ai.sh | bash
```

Windows（PowerShell）：

```powershell
irm https://zhangsizhou.pages.dev/ai.ps1 | iex
```

这一句会自动下载对应平台的程序并**直接启动**，退出后不留任何痕迹。支持 macOS / Linux / Windows 的 amd64 与 arm64。

> 域名 `sizhou.ai` 就绪后，命令会分别缩短为 `curl -fsSL sizhou.ai/ai.sh | bash` 与 `irm https://sizhou.ai/ai.ps1 | iex`。

## 安装成常驻命令（可选）

想装成一个随时可敲的命令（不用每次下载）：

```bash
curl -fsSL https://raw.githubusercontent.com/zimaStrawer/terminal-resume/main/install.sh | bash
```

装完直接敲 `sizhou-resume`。

## 已实现

- ASCII `ZHANG` 首页，窄终端自动切换为紧凑标题
- **对话式界面**：命令的回答以对话流形式流式打印，旧回答排在输入框上方，越攒越多就把 logo 顶出屏幕；输入框始终钉在底部
- **按 `/` 弹出快捷命令菜单**，四条固定文案（`/skills`、`/resume`、`/log`、`/personal`），永远落在输入框正上方，不遮挡 logo；方向键选择、Enter 执行
- **输入框左右各一条主题色实心竖条**（`#16B8F3`），框内只有一行输入区（上下各一行内边距，让它垂直居中），**框区铺一层 `#201E1E` 灰面板**（对齐 OpenCode 的输入框观感），靠这两条竖条和 `›` 提示符辨认。竖条**由格子背景色整格铺满**画出来，所以必然贴边、等高、纵向连续无缝——靠字形墨迹画不出这个效果：方块字符（`▏`/`▕`）的墨迹高度由字体决定、会被切成几段，而 box-drawing 的 `│`（U+2502）为了让相邻行的线衔接，墨迹比格还高，会从框底漏出一截、在页面底色上显形（2026-09-20 用户报的「下面还有旧的竖线露出来」）。格内仍留一个不可见的占位字形（`ǀ`，U+01C0，前景色 = 背景色）供光标定位与测试探针认框行，为什么是这个字符见 `internal/app/box.go`
- **页面底色是 `#0D0D0D` 而不是纯黑**：macOS 输入法组词时会把输入行从光标处向行尾整行擦除（BCE），擦除填充色 = 终端遗留的画笔背景色，应用无法控制。只要页面色与面板色差得远，擦除就会把面板灰涂到框右侧的边距格上（打字时框外闪一条灰块）。抓 opencode v2.0.9 的原始字节流后确认它的做法是「页面 `#080808` / 面板 `#1c1c1c`，只差 Δ20——擦照样在擦，只是怎么擦都看不出来」。所以这里把页面提到 `#0D0D0D`，与面板 `#201E1E` 只差 Δ19：稳定状态的框边界依然清晰（主要靠 `#16B8F3` 竖线辨认），组词期间的瞬态灰块则低于察觉阈值
- **中文输入法不跑偏**：macOS 输入法的预编辑串（未上屏的拼音）由终端按**硬件光标**位置绘制，渲染完一帧后硬件光标会停在帧尾任意角落，拼音和候选词会被画到输入框外面。修复方式是每帧显式把硬件光标钉到输入光标所在的格子（`View().Cursor`），光标列按**显示格数**算（中文 1 rune 占 2 格），本地 TUI 与 SSH 访客同样生效
- **自由问答是真实 AI**：输入不以 `/` 开头的文本会打到作品集站的 `/api/chat`（Cloudflare Pages Function），回答按 SSE 逐片回来。**终端二进制里没有任何密钥**，它只负责转发，密钥留在站点侧的环境变量里
- **长回答按内容宽度折行**：真实 AI 回答通常是一整条不含任何换行的长句，必须折行显示。旧版对助手回答只做单行截断，实测一条 168 字的回答只显示出前 54 字、后面整段被静默丢掉（斜杠命令的本地回答自带换行，所以这个 bug 一直没暴露）
- **提示字符是动的**：闪烁 10 步 ×300ms → 停 560ms → 乱序 16 帧 ×64ms（字符集 `x > / & - ~ ^ =`）→ 停 720ms 循环，节奏与字符集照搬本站网页模块标题的光标，基字符是 `›`。每帧只占 1 列、暗态用空格补齐，所以输入框整行宽度恒定，字不会抖（实测见下）
- **空态占位文案** `随便聊聊，或按 "/" 唤出快捷命令`；终端窄于 62 列自动换成短句 `按 "/" 唤出快捷命令`。框内不再重复快捷键提示，输入框下面是**三行**：间距空行 + 常驻操作提示（只有一句 `连按两次 esc 退出`，左缘与框左竖线同列，武装时换成 `再按一次 esc 退出`）+ 反馈行（未知命令、主题切换等才出字）。空行是刻意的：提示紧贴框下沿太挤
- **收尾文案跟在 logo 下方**：一句致谢、一句邀请，加一行主题色标语 `Design Without Boundaries`；空首页时它正好落在 logo 与输入框之间，有了回答就和 logo 一起被对话往上顶、最终一起离屏
- `/about`（别名 `/personal`）、`/experience`、`/projects`、`/skills`、`/status`、`/log`、`/random`、`/contact`、`/resume` 等命令
- 所有页面内容（`/projects`、`/project 01` …）都作为**对话回答**打印，不切独立页面——早期那套切页界面（数字键 1—5 / `?` / Esc 逐级返回）已随死代码一起删除
- Tab 命令补全、Ctrl+P 打开帮助页
- 退出：`Ctrl+C`（随时）、`/exit`（`/退出` `/quit` `/q`）、首页**连按两次 Esc**。Esc 的层级是「清空输入 → 回看历史时回到最新 → 连按两次退出」；等回答时 Esc 是中止生成，已经收到的部分会保留
- 中文命令别名，例如 `/项目`、`/经历`、`/联系`
- 深色主题（页面底 `#0D0D0D`、输入框面板 `#201E1E`、竖条与下拉选中 `#16B8F3`、强调色 `#00BBF9`）与高对比黑白模式（`--mono`，黑白下不铺面板底色）
- 除输入框提示字符外无持续动画
- CLI 在访客本机运行；可选 SSH 模式不提供系统 Shell

## 那个动画的开销

唯一一处持续动画是输入框的提示字符，代价实测如下（Apple M2，100×30）：

| 场景 | 终端写入 | CPU（单帧渲染） |
|---|---|---|
| 空闲（动画关） | 0 字节/秒 | — |
| 空闲（动画开） | ≈1.4 KB/s | 371µs/帧，平均 ≈0.2% 单核 |
| 打字 30 字符（动画关 → 开） | ≈9.5KB → ≈16KB | 增量为 0，即每次按键没有额外成本 |

对照：流式回答本来就是 16ms 一帧（≈62fps），比动画最密的乱序阶段（64ms 一帧）还快 4 倍——也就是说这个动画比它之前就在做的事便宜得多。按键最坏等待一帧 ≈0.4ms（长对话 ≈0.7ms），远低于可感知阈值，不会造成输入卡顿。

自己复现（`go test` 下的基准 + pty 实测）：
```bash
go test ./internal/app/ -run XXX -bench Benchmark -benchtime 300x
python3 scripts/measure-tui-traffic.py ./terminal-resume        # 空闲
python3 scripts/measure-tui-traffic.py ./terminal-resume 30     # 连打 30 个字符
```

## 本地预览

需要 Go 1.25 或更高版本。

项目目录中已经生成了当前 Mac 可直接运行的文件，本地预览不需要再经过 `go run` 的编译启动：

```bash
./terminal-resume --local
```

修改代码或换到另一台电脑后，重新构建一次即可：

```bash
make build
```

## 自由问答（真实 AI）

首页输入框里输入**不以 `/` 开头**的文本，就会走真实 AI，而不是本地命令。回答同样是流式打印进对话流里的。

默认后端地址取简历 YAML 里的 `profile.website`，也就是**AI 助手和简历挂在同一个站点上**：

```
profile.website  →  https://zhangsizhou.pages.dev
实际请求          →  https://zhangsizhou.pages.dev/api/chat
```

⚠️ `profile.website` 必须填**真实已部署**的域名。它曾经是个占位域名（`zhang.design`），根本没注册，访客一提问只看到一行 `…: EOF`。地址写错是这条链路上最难自查的故障，所以客户端会把传输层错误翻译成人话再显示：

| 情况 | 屏幕上的提示 |
|---|---|
| 域名查不到 | `AI 服务地址不存在：zhang.design（--api 可换后端）` |
| 端口拒连 | `连不上 AI 服务：127.0.0.1:8799（--api 可换后端）` |
| 连上了但没响应就断 | `AI 服务没返回内容就断开：host（--api 可换后端）` |

那个 `/api/chat` 是作品集站上的 Cloudflare Pages Function（`functions/api/chat.ts`），负责持密钥、校验同源、限制轮数（最近 12 条 / 1 万字）和上下文范围，再转发给上游模型。

**部署前记得给 Pages 项目配密钥**，否则函数会返回 503 `AI 服务尚未配置。`，网页访客和终端都问不出东西（密钥只在 Cloudflare 侧，不要写进仓库）：

```bash
wrangler pages secret put TOKENHUB_API_KEY --project-name=zhangsizhou
wrangler pages secret put TOKENHUB_MODEL   --project-name=zhangsizhou
```

**终端侧永远不持密钥。** 这个二进制只发一个 `POST {messages:[...]}` 并读回 SSE，任何密钥都留在站点侧的环境变量里——所以它可以安全地分发给访客。

本地联调可以直接指向本地起的站点：

```bash
./terminal-resume --local --api http://127.0.0.1:8788
```

其它行为：

- `--api` 覆盖默认地址，也可以用环境变量 `TERMINAL_RESUME_API`
- `--api off` 关掉自由问答，只保留斜杠命令（不配后端时输入自由文本会给出提示，不会静默失败）
- 回答生成中按 `Esc` 中断，**已经收到的部分会保留**
- 只有自由问答这条线的消息会进请求历史；斜杠命令的本地页面又长又没走过对话协议，塞进去只会把真正的上下文挤掉

## 会话记录与邮件

访客不需要留邮箱、不需要任何动作：他关掉终端时，这次会话里**他敲过的命令和提过的问题**会被整理成一封邮件发到作者邮箱。

上报分两段（不是退出时才一次性发，那样断网就整份丢）：

1. 每次提交立刻异步上报一条（在独立 goroutine 里，界面不等它）；
2. 会话结束时再发一条收尾请求，**带上完整记录**当权威版本——逐条上报全挂了也没关系，只要关终端那一刻网络是通的，整份记录照样到齐。

站点侧的 `/api/session`（`functions/api/session.ts`）收下这些记录、用 KV 归档，收尾时通过 Resend 发信。终端侧依旧只发请求、**不发信也不持密钥**。

规则：

- **只记访客侧**：命令与提问，AI 的回答一条都不记（回答又长，进邮件只会把时间线冲垮）
- **空会话不发**：一次都没提交就退出，连一个包都不发
- **失败一律静默**：访客不该看到任何错误，也不该为上报多等一秒
- 收尾放在 SSH 中间件里（而不是模型里），这样 idle / max-session 超时、网络中断、直接关窗口也都覆盖得到

调试与关闭：

```bash
./terminal-resume --local --report http://127.0.0.1:8788   # 指向本地站点
./terminal-resume --report off                             # 彻底关闭（预览脚本默认就这么跑）
```

`--report` 留空时沿用 `profile.website`，也可以用环境变量 `TERMINAL_RESUME_REPORT`。

## 界面预览

`docs/tui-preview.html` 是界面预览页，由**真实运行的二进制 + pty 原始字节流**还原（xterm.js），不是手绘稿。改了界面后重新生成：

```bash
make preview
```

它会先跑一遍二进制抓取各种终端尺寸/主题下的字节流（标准 100×30、窄屏 64×22、窄屏短句 50×22、通知行、黑白模式、以及一组自由问答），再逐格还原成 HTML。

预览必须可复现，所以它**从不碰真实网络**：不涉及自由问答的会话都带 `--api off`，而「对话」那组打的是 [`scripts/chat-stub.py`](scripts/chat-stub.py)——一个按同一套 SSE 协议回固定长文的本地桩。那段固定长文刻意是不含换行的长句，正好是「只截断不折行」那个 bug 的触发条件，桩里还插了一次长停顿，好让快照能抓到"只打出一半"的瞬间。

渲染依赖 `@xterm/headless`；运行时可用 `PREVIEW_PYTHON` / `PREVIEW_NODE` / `PREVIEW_NODE_PATH` 指定解释器和依赖目录：

```bash
make preview PREVIEW_NODE_PATH="$(npm root -g)"
```

## 将它变成可分享的一行命令

`npm/` 是独立的发布包。它只包含一个启动脚本和 macOS、Linux、Windows 的预编译程序，访客无需安装 Go。

```bash
cd npm
npm run build:native
npm pack --dry-run
```

确认包内容后，使用自己的 npm 账号登录，并在确认包名仍可用、简历中的示例内容已替换后公开发布：

```bash
npm login
npm publish
```

公开发布是对外操作，不会由构建命令自动执行。发布后可在另一台装有 Node.js 的电脑上运行 `npx -y zhang-terminal-resume` 验收。

## 可选：SSH 访问

如果未来也想让访客输入 `ssh -p 23234 zhang@your-domain.com`，本项目仍保留 SSH 服务模式。它需要一台持续在线的公网服务器；仅用本地电脑或 Cloudflare Pages 不能提供原生 SSH 入口。

本机测试 SSH 服务：

```bash
./terminal-resume
```

另开一个终端连接：

```bash
ssh -p 23234 zhang@localhost
```

首次启动服务时会自动生成 `.data/ssh_host_ed25519`。它是 SSH 服务器的身份凭证，不是访客登录密钥。线上部署后应持久保存；如果删除并重新生成，访客会看到服务器指纹发生变化的安全警告。

## 修改简历内容

编辑 [`profile/resume.yaml`](profile/resume.yaml) 即可。默认内容会被编译进程序，也可以在启动时加载外部文件：

```bash
./terminal-resume --content ./profile/resume.yaml
```

当前项目案例和联系方式是示例内容，上线前需要替换。其中两个字段有额外用途，别当普通文案填：

- **`profile.website`** —— 同时是自由问答的后端地址（见上文），必须是**真实已部署**的域名，填错访客一提问只会看到一行连接错误
- **`profile.resume_url`** —— **留空**时 `/resume` 显示「等待简历下载中」的进度占位，而不是一条打不开的死链；填入真实 PDF 地址后自动变回可点击的下载链接，不需要改代码

其余的 `email` / `github` 会原样显示给访客，占位域名点开就是 404。

## 常用启动参数

```text
--local                 不启动 SSH，直接预览 TUI
--listen ADDRESS        SSH 监听地址，默认 0.0.0.0:23234
--host-key PATH         SSH 主机密钥路径
--content PATH          外部简历 YAML 文件
--api BASE             自由问答的后端站点，默认取 profile.website；off 关闭
--report BASE          会话上报的后端站点，默认取 profile.website；off 关闭
--mono                  使用黑白高对比主题
--idle-timeout 10m      空闲连接超时
--max-session 30m       单次连接最长时间
```

设置标准环境变量 `NO_COLOR=1` 也会启用黑白模式。

## Docker 部署

```bash
docker build -t terminal-resume .
docker volume create terminal-resume-data
docker run -d \
  --name terminal-resume \
  --restart unless-stopped \
  -p 2222:23234 \
  -v terminal-resume-data:/app/.data \
  terminal-resume
```

然后运行：

```bash
ssh -p 2222 zhang@your-server.com
```

正式使用 `ssh zhang@resume.example.com` 这种无端口命令时，把域名解析到服务器，并将公网 22 端口转发到容器 23234 端口。这个项目可以独立部署，不需要与个人网站共用代码仓库或运行进程。

## 技术选择

- Go：生成单个可执行文件，部署简单、启动快、内存占用低
- Bubble Tea + Lip Gloss：负责 TUI 状态与视觉样式
- Wish：把每次 SSH 连接转换为独立的 TUI 会话
- YAML：非开发方式维护简历内容

这里不需要在终端渲染 React。React/Ink 也能制作 TUI，但这份简历更适合使用 Go：依赖更少，SSH 服务和界面可以放在同一个小程序里。

## 验证

```bash
go test ./...
go vet ./...
go build ./cmd/terminal-resume
```

界面相关的改动一律靠**真实运行**取证，不靠肉眼（`scripts/` 下的脚本都用 pty 抓原始字节流，再用 xterm.js 逐格还原）：

- `python3 scripts/drive-tui.py --cmd "/resume" --text` —— 输入一条命令、直接把还原后的屏幕打印出来，排查渲染最快的方式
- `make preview` —— 重生成 `docs/tui-preview.html`
- `python3 scripts/measure-tui-traffic.py ./terminal-resume 30` —— 量终端流量，估动画/重绘开销
- `python3 scripts/chat-stub.py 8799` —— 确定性 SSE 后端，用来逐字比对「流式回答有没有丢字」
