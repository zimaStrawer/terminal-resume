#!/usr/bin/env node
/**
 * 用 xterm.js（headless）把 pty 原始字节流还原成 docs/tui-preview.html。
 *
 * 为什么不手写预览、也不用 pyte：pyte 在「菜单开 → 关」这种大面积重排时会留下
 * 假残影（把回答画到 logo 行上），真实终端并没有。xterm.js 是权威还原器。
 *
 * 用法：
 *   NODE_PATH=$(npm root -g) node scripts/render-tui-preview.js [frames.json] [out.html]
 * 默认读 /tmp/tui_frames.json，写 <repo>/docs/tui-preview.html。
 * 依赖 @xterm/headless。
 */
const fs = require('fs');
const path = require('path');
const { Terminal } = require('@xterm/headless');

const ROOT = path.dirname(__dirname);
const FRAMES_PATH = process.argv[2] || '/tmp/tui_frames.json';
const OUT = process.argv[3] || path.join(ROOT, 'docs', 'tui-preview.html');

const FRAMES = JSON.parse(fs.readFileSync(FRAMES_PATH, 'utf8'));

const PAGE_BG = '#0D0D0D';
const SURFACE = '#161616';

/* ---------- 调色板（256 色，供 0x01/0x02 模式用） ---------- */
const BASE16 = [
  '#000000', '#cd3131', '#0dbc79', '#e5e510', '#2472c8', '#bc3fbc', '#11a8cd', '#e5e5e5',
  '#666666', '#f14c4c', '#23d18b', '#f5f543', '#3b8eea', '#d670d6', '#29b8db', '#e5e5e5',
];

function rgbHex(r, g, b) {
  return '#' + [r, g, b].map((v) => v.toString(16).padStart(2, '0')).join('');
}

const PALETTE = (() => {
  const p = BASE16.slice();
  const lv = [0, 95, 135, 175, 215, 255];
  for (let r = 0; r < 6; r++) for (let g = 0; g < 6; g++) for (let b = 0; b < 6; b++) {
    p.push(rgbHex(lv[r], lv[g], lv[b]));
  }
  for (let i = 0; i < 24; i++) {
    const v = 8 + i * 10;
    p.push(rgbHex(v, v, v));
  }
  return p;
})();

/* ---------- 颜色读取 ----------
 * ⚠️ xterm.js 的 getFgColorMode() 返回的是「位掩码」而不是 0/1/2 枚举：
 *   0                     = 未设色（走终端默认）
 *   0x01000000/0x02000000 = 256 色板索引
 *   0x03000000 (50331648) = 真彩色，取值即 0xRRGGBB
 * 按 0/1/2 判断会把真彩色全判成默认色（曾因此丢掉整个 logo 的双色）。
 */
const MODE_MASK = 50331648;
const MODE_RGB = 50331648;
const MODE_PALETTE_A = 16777216;
const MODE_PALETTE_B = 33554432;

function colorOf(cell, isFg, fallback) {
  const mode = (isFg ? cell.getFgColorMode() : cell.getBgColorMode()) & MODE_MASK;
  if (mode === 0) return fallback;
  const value = isFg ? cell.getFgColor() : cell.getBgColor();
  if (mode === MODE_RGB) {
    return rgbHex((value >> 16) & 0xff, (value >> 8) & 0xff, value & 0xff);
  }
  if (mode === MODE_PALETTE_A || mode === MODE_PALETTE_B) {
    return PALETTE[value & 0xff] || fallback;
  }
  return fallback;
}

/* ---------- 渲染单帧 ---------- */
function renderFrame(frame) {
  return new Promise((resolve) => {
    const term = new Terminal({
      cols: frame.cols,
      rows: frame.rows,
      scrollback: 0,
      allowProposedApi: true,
    });
    term.write(Buffer.from(frame.stream, 'base64'), () => {
      const buf = term.buffer.active;
      const lines = [];
      for (let y = 0; y < frame.rows; y++) {
        const line = buf.getLine(y);
        if (!line) { lines.push([]); continue; }
        const runs = [];
        for (let x = 0; x < frame.cols; x++) {
          const cell = line.getCell(x);
          if (!cell) continue;
          if (cell.getWidth() === 0) continue; // 宽字符的第二格
          const chars = cell.getChars() || ' ';
          const fg = colorOf(cell, true, '#F8FAFC');
          const bg = colorOf(cell, false, PAGE_BG);
          const bold = cell.isBold() ? 1 : 0;
          const key = fg + '|' + bg + '|' + bold;
          const last = runs[runs.length - 1];
          if (last && last.key === key) last.text += chars;
          else runs.push({ key, fg, bg, bold, text: chars });
        }
        lines.push(runs);
      }
      term.dispose();
      resolve(lines);
    });
  });
}

function escapeHtml(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function linesToHtml(lines) {
  return lines
    .map((runs) => {
      if (!runs.length) return '';
      // 整行都是「默认底色 + 空白」时不必输出 span，保持右边界干净
      const plain = runs.every((r) => r.bg === PAGE_BG && r.text.trim() === '');
      if (plain) return ' ';
      return runs
        .map((run) => {
          const parts = [
            'color:' + run.fg,
            run.bg === PAGE_BG ? null : 'background:' + run.bg,
            run.bold ? 'font-weight:600' : null,
          ].filter(Boolean);
          return `<span style='${parts.join(';')}'>${escapeHtml(run.text)}</span>`;
        })
        .join('');
    })
    .join('\n');
}

function sizeTitle(group) {
  if (group.cols === 100) return '标准终端 ' + group.cols + ' × ' + group.rows;
  if (group.cols === 64) return '窄屏终端 ' + group.cols + ' × ' + group.rows;
  if (group.cols === 90) return '黑白模式（--mono） ' + group.cols + ' × ' + group.rows;
  return '终端 ' + group.cols + ' × ' + group.rows;
}

/* ---------- 主流程 ---------- */
(async () => {
  const groups = [];
  const seen = new Set();
  for (const frame of FRAMES) {
    const key = frame.cols + 'x' + frame.rows;
    let group = groups.find((g) => g.key === key);
    if (!group) { group = { key, cols: frame.cols, rows: frame.rows, cards: [] }; groups.push(group); }
    const dedupe = key + '|' + frame.label;
    if (seen.has(dedupe)) continue;
    seen.add(dedupe);
    process.stdout.write('render ' + frame.label + ' … ');
    group.cards.push({ label: frame.label, html: linesToHtml(await renderFrame(frame)) });
    console.log('ok');
  }

  const body = groups
    .map(
      (g) =>
        `<section class='group'><h1>${escapeHtml(sizeTitle(g))}</h1>` +
        g.cards
          .map((c) => `<section class='card'><h2>${escapeHtml(c.label)}</h2><pre>${c.html}</pre></section>`)
          .join('') +
        '</section>'
    )
    .join('\n');

  const intro = `<header class='intro'>
  <h1 class='intro-title'>ZHANG Terminal Resume · 界面预览</h1>
  <p class='intro-sub'>由真实运行的二进制 + pty 原始字节流还原（xterm.js），不是手绘稿。</p>
  <ul class='spec'>
    <li><b>终端底色</b> <code>#0D0D0D</code> —— 通过 OSC 11 写入终端，整块画布都吃这个颜色。</li>
    <li><b>输入框</b> <code>#201E1E</code> 实心底，<b>左右各一条主题色实心竖条</b> <code>#16B8F3</code>，始终钉在屏幕底部。竖条由<b>格子背景色整格铺满</b>画成，所以必然贴边、等高、连续无缝。靠字形墨迹画不出这个效果：方块字符 <code>▏</code>/<code>▕</code> 的墨迹高度由字体决定、会被切成几截；box-drawing 的 <code>│</code>（U+2502）为了让相邻行的线衔接，墨迹比格还高，会从框底漏出一截、在页面底色上显形。格内另留一个不可见的占位字形 <code>ǀ</code>（U+01C0，前景色 = 背景色）供光标定位与测试探针认框行。框内<b>只有一行输入区</b>，上下各一行内边距让它垂直居中；空态显示占位文案 <code>Ask me anything, or press / for shortcuts.</code>（窄于 62 列时换成短句 <code>Ask me anything, or press /</code>）。</li>
    <li><b>提示字符是动画的</b> 输入框里那个字符照搬网站模块标题的光标（<code>AsciiTitle.astro</code>）：闪烁 10 步 ×300ms → 停 560ms → 乱序 16 帧 ×64ms（字符集 <code>x &gt; / &amp; - ~ ^ =</code>）→ 停 720ms 循环，基字符是 <code>›</code>。每一帧都只占 1 列，且暗态用空格填充，所以输入框整行宽度恒定、字位置不抖。快照只抓到循环中的某一帧，故各卡片里这个字符会不一样。</li>
    <li><b>框内不再重复快捷键提示</b> 旧版那行 <code>/ shortcuts  ctrl+p commands  ctrl+c exit</code> 已删除，提示统一由占位文案承担；输入框下方那一行也只在有通知（notices）时才出字。</li>
    <li><b>下拉框</b> 不设背景色（直接落在终端底色上），未选中项灰色 <code>#94A3B8</code>，选中项主题色 <code>#16B8F3</code>。</li>
    <li><b>快捷菜单</b> <code>/</code> 唤出，四条固定文案（skills / resume / log / personal），永远落在输入框正上方，不遮 logo。</li>
    <li><b>收尾文案</b> 跟在 logo 下方：一句致谢 + 一句邀请 + 一行主题色标语 <code>Design Without Boundaries</code>。空首页时它在 logo 与输入框之间，有了回答就和 logo 一起被顶上去、最终离屏。行尾下划线是静态光标（不闪烁）。</li>
    <li><b>自由问答是真实 AI</b> 输入不以 <code>/</code> 开头的文本会打到作品集站的 <code>/api/chat</code>（Cloudflare Pages Function），回答按 SSE 逐片回来。<b>终端二进制里没有任何密钥</b>，它只负责转发；密钥留在站点侧的环境变量里。默认地址取简历里的 <code>profile.website</code>（AI 和简历同一个站点），<code>--api</code> 可覆盖、<code>--api off</code> 可关闭。上文「对话」那组就是这条链路——后端用 <code>scripts/chat-stub.py</code> 这个确定性桩，回答固定，所以预览可复现、不碰网络。</li>
    <li><b>回答流式打印</b> 逐字显现，未打印到的位置留空，整页行结构不跳动。网络快慢与打字速度是解耦的：网络只负责把整段变长，打字机负责追上去，所以慢网络下也不会先卡住再一次蹦一大段。</li>
    <li><b>长回答按内容宽度折行</b> 真实 AI 回答通常是一整条<b>不含任何换行</b>的长句，必须折行显示。旧版对助手回答只做单行截断，实测一条 168 字的回答只显示出前 54 字、后面整段被静默丢掉（斜杠命令的本地回答自带换行，所以这个 bug 一直没暴露）。</li>
    <li><b>对话流</b> 旧回答按顺序累积在下方，越攒越多就把收尾文案和 logo 一路顶出屏幕；输入框始终钉在底部。Esc 可中断正在生成的回答，已收到的部分会保留。</li>
    <li><b>简历未就绪时给进度占位</b> <code>/resume</code> 的 RESUME 一栏：<code>profile.resume_url</code> 为空时渲染「等待简历下载中」+ 一条 30 格进度条，而不是给一个打不开的链接（模板里原本是一条不存在的域名）。进度条只用 <code>█</code> 与空格，刻意避开 <code>░▒▓</code>、<code>▏▎▍</code> 这些 East Asian Width = Ambiguous 的字形——它们在偏好 CJK 字体的终端里会被排成两列，把整行挤歪。填上真实 PDF 地址后自动变回可点击的下载链接，不用改代码。</li>
    <li><b>LOGO</b> <code>SI</code> 银灰 <code>#A6AEBB</code> + <code>ZHOU</code> 近白 <code>#FFFFFF</code>。</li>
  </ul>
</header>`;

  const html = `<!doctype html>
<meta charset='utf-8'>
<title>ZHANG Terminal Resume · 预览</title>
<style>
 body {
   margin: 0; padding: 40px 32px 64px;
   background: ${PAGE_BG}; color: #F8FAFC;
   font-family: -apple-system, 'PingFang SC', sans-serif;
 }
 h1 { font-size: 15px; font-weight: 600; margin: 0 0 16px; color: #16B8F3; }
 h2 { font-size: 13px; font-weight: 500; margin: 0 0 8px; color: #94A3B8; }
 .group { margin-bottom: 44px; }
 .card {
   background: ${SURFACE}; border: 1px solid #262626; border-radius: 12px;
   padding: 18px 20px; margin-bottom: 18px; overflow-x: auto;
 }
 pre {
   margin: 0; font-family: 'SF Mono', Menlo, Consolas, monospace;
   font-size: 13px; line-height: 1.45; white-space: pre; tab-size: 4;
   background: ${PAGE_BG};
 }
 .intro {
   max-width: 760px; margin: 0 0 44px; padding: 22px 24px;
   border: 1px solid #262626; border-radius: 12px; background: ${SURFACE};
 }
 .intro-title { font-size: 20px; font-weight: 600; margin: 0 0 6px; color: #F8FAFC; }
 .intro-sub { margin: 0 0 16px; font-size: 13px; color: #94A3B8; }
 .spec { margin: 0; padding-left: 18px; font-size: 13px; line-height: 1.85; color: #CBD5E1; }
 .spec b { color: #F8FAFC; font-weight: 600; }
 .spec code {
   font-family: 'SF Mono', Menlo, Consolas, monospace; font-size: 12px;
   color: #16B8F3; background: rgba(22,184,243,.10);
   padding: 1px 5px; border-radius: 4px;
 }
</style>
${intro}
${body}
`;

  fs.writeFileSync(OUT, html);
  console.log('wrote ' + OUT + ' (' + html.length + ' bytes)');
})();
