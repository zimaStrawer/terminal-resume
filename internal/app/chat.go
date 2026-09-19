package app

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// chatRole 区分对话流里的发言方。
type chatRole int

const (
	chatUser chatRole = iota
	chatAssistant
)

// 流式打印的节奏：每 streamInterval 追加 streamChunk 个可见字符。
// 斜杠命令的本地回答一次性备好，靠它打出打字机效果；
// 真实 AI 回答则是网络一片片送来、这里再逐字追上去（total 随网络增长，shown 负责追），
// 两者共用同一条 tick 链。
const (
	streamChunk    = 6
	streamInterval = 16 * time.Millisecond
)

// streamTickMsg 驱动流式打印：每收到一次就多显示一小段。
type streamTickMsg struct{}

// chatMessage 是对话流里的一条消息。
// shown 是当前已经打印出来的"可见字符数"（不含转义序列），用于流式效果；
// shown >= total 表示这条已经打印完毕。
type chatMessage struct {
	role  chatRole
	text  string
	total int
	shown int
	// ai 表示这条属于「自由问答」那条线。斜杠命令的回答是本地页面
	// （又长、又没走过对话协议），不参与发给 /api/chat 的历史。
	ai bool
}

func newChatMessage(role chatRole, text string) chatMessage {
	total := visibleRuneCount(text)
	return chatMessage{role: role, text: text, total: total, shown: total}
}

// newAIChatMessage 新建一条属于自由问答的消息（会进 AI 的历史）。
func newAIChatMessage(role chatRole, text string) chatMessage {
	message := newChatMessage(role, text)
	message.ai = true
	return message
}

// newStreamingMessage 新建一条"还没开始打印"的本地回答，交给 Update 逐帧补全。
func newStreamingMessage(text string) chatMessage {
	message := newChatMessage(chatAssistant, text)
	message.shown = 0
	return message
}

// visibleRuneCount 统计文本里的可见字符数（不计 ANSI 转义序列与换行）。
func visibleRuneCount(text string) int {
	total := 0
	for _, line := range strings.Split(text, "\n") {
		total += utf8.RuneCountInString(ansi.Strip(line))
	}
	return total
}

func (c chatMessage) done() bool {
	return c.shown >= c.total
}

// visiblePrefix 返回 s 中「前 n 个可见字符」对应的前缀。
// 它会跳过转义序列（CSI / OSC / 两字节 ESC），因此不会把 ANSI 序列从中间切断；
// 因为正文里的每个字符各自带样式，截断后的前缀依然是合法且样式正确的。
func visiblePrefix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	index := 0
	for index < len(s) {
		if s[index] == 0x1b {
			index = skipEscape(s, index)
			continue
		}
		if count == n {
			break
		}
		_, size := utf8.DecodeRuneInString(s[index:])
		if size <= 0 {
			break
		}
		index += size
		count++
	}
	return s[:index]
}

// skipEscape 返回从 index（指向 ESC）开始的整个转义序列之后的下标。
// 支持 CSI（ESC [ … 终止符）、OSC（ESC ] … BEL / ESC \）和两字节 ESC 序列。
func skipEscape(s string, index int) int {
	next := index + 1
	if next >= len(s) {
		return next
	}
	switch s[next] {
	case '[': // CSI
		next++
		for next < len(s) && (s[next] < 0x40 || s[next] > 0x7e) {
			next++
		}
		if next < len(s) {
			next++
		}
		return next
	case ']': // OSC
		next++
		for next < len(s) {
			if s[next] == 0x07 { // BEL 结束
				return next + 1
			}
			if s[next] == 0x1b && next+1 < len(s) && s[next+1] == '\\' { // ST 结束
				return next + 2
			}
			next++
		}
		return next
	default:
		return next + 1
	}
}

// finishStreaming 把仍在打印的消息立刻补全，避免旧消息停在半截。
func (m *Model) finishStreaming() {
	m.streaming = false
	for index := range m.chat {
		m.chat[index].shown = m.chat[index].total
	}
}

// startStreaming 启动打字机的 tick 链；已经在跑就什么都不做。
// 两条流共用一个开关：本地回答一次备好、AI 回答一片片到达，
// 都在「有新字要显示」时调用它。
func (m *Model) startStreaming() tea.Cmd {
	if m.streaming {
		return nil
	}
	m.streaming = true
	return streamCmd()
}

// advanceStream 推进一次流式打印，返回是否还有字要继续显示。
// 追平当前已收到的文本就停链（AI 还在途中的话，下一片到达会重新启动），
// 避免网络慢时空转 tick。
func (m *Model) advanceStream() bool {
	if !m.streaming {
		return false
	}
	index := len(m.chat) - 1
	if index < 0 {
		m.streaming = false
		return false
	}
	message := &m.chat[index]
	if message.shown >= message.total {
		m.streaming = false
		return false
	}
	message.shown = min(message.shown+streamChunk, message.total)
	if message.shown >= message.total {
		m.streaming = false
		return false
	}
	return true
}

// streamCmd 安排下一次流式推进。
func streamCmd() tea.Cmd {
	return tea.Tick(streamInterval, func(time.Time) tea.Msg { return streamTickMsg{} })
}

// conversationLines 把 logo、收尾文案与对话流拼成一段多行文本（尚未做底部对齐）。
// logo 与收尾文案居中，消息左对齐。
// 收尾文案紧跟在 **logo 之后**：空对话时它就在 logo 与输入框之间，
// 一旦有了回答，它会和 logo 一起被对话往上顶、最终一起离屏——
// 不要把它挂到末尾常驻输入框上方（用户 2026-09-19 明确要求）。
func (m Model) conversationLines(width int) []string {
	logo := m.logoCompact
	if width >= logoWideMinWidth {
		logo = m.logoWide
	}
	center := lipgloss.NewStyle().Width(max(width, 1)).Align(lipgloss.Center)
	lines := strings.Split(center.Render(logo), "\n")
	lines = append(lines, "")
	lines = append(lines, m.greetingLines(width)...)
	lines = append(lines, "")
	for _, message := range m.chat {
		lines = append(lines, m.renderChatMessage(message, width)...)
		lines = append(lines, "")
	}
	return lines
}

// renderChatMessage 把一条消息渲染成若干行。
//
// 关键点一：行结构始终由**完整文本**决定，流式只在行内逐字揭示。
// 这样打印过程中总行数不变，上方对话区（以及 logo）不会每个 tick 都位移，
// 终端只需要重画少数几行；否则整块内容会随着答案变长而持续抖动。
//
// 关键点二：助手回答必须先按内容宽度**折行**，不能只截断。
// 斜杠命令的本地回答渲染时就已经折好、每行自带换行，所以以前没暴露问题；
// 而真实 AI 回答通常是一整条不含任何换行的长句——单行截断会把后面整段正文
// 悄悄丢掉（实测一条 168 字的回答只显示出前 54 字，剩下的全没了）。
// 折行对本来就折好的本地回答是无操作，因此本地命令的排版逐字不变。
//
// 代价：真实 AI 回答是边收边长，所以「完整文本」也在变——收窄窗口时
// 行数仍会随新文本增加，这是网络流本身的性质，无法靠预先定行结构消除。
func (m Model) renderChatMessage(message chatMessage, width int) []string {
	if message.role == chatUser {
		lines := strings.Split(ansi.Wrap(ansi.Strip(message.text), max(width-2, 1), ""), "\n")
		out := make([]string, 0, len(lines))
		for index, line := range lines {
			if index == 0 {
				out = append(out, "› "+line)
				continue
			}
			out = append(out, "  "+line)
		}
		return out
	}

	// 还在等首字节：先给一个占位，别让提问之后看起来像卡住了。
	if ansi.Strip(message.text) == "" {
		return []string{m.styles.dim.Render("…")}
	}

	lines := wrapChatText(message.text, max(width, 1))
	out := make([]string, 0, len(lines))
	remaining := message.shown
	for _, line := range lines {
		if remaining <= 0 {
			out = append(out, "")
			continue
		}
		length := utf8.RuneCountInString(ansi.Strip(line))
		if remaining < length {
			out = append(out, visiblePrefix(line, remaining)+"\x1b[0m")
			remaining = 0
			continue
		}
		remaining -= length
		out = append(out, line)
	}
	return out
}

// wrapChatText 把一条消息折成「显示行」。
//
// 先按已有的换行切开：斜杠命令的本地回答在各自的渲染函数里就折好了，
// 每行都不超宽，对这些行 ansi.Wrap 是无操作，所以本地排版不会因为这次改动漂移。
// 只有超宽的行（真实 AI 回答就是这种）才真正折行。
//
// 末尾再逐行硬截一次兜底：ansi.Wrap 遇到「整行都是不可断的长词」（例如超长 URL）
// 时仍可能给出一行超宽，宁可截掉也不能让它把输入框挤出屏幕。
//
// 注意 ansi.Wrap 在断行处会吃掉那个空格（实测 out runes ≤ in runes，绝不会新增字符），
// 所以「已显示字数」只会多算、不会漏算，揭示到末尾时剩余额度全部落在最后一行上，
// 不会出现答案被打到一半就停住的情况。
func wrapChatText(text string, width int) []string {
	source := strings.Split(text, "\n")
	out := make([]string, 0, len(source))
	for _, line := range source {
		if ansi.StringWidth(line) <= width {
			out = append(out, line)
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(line, width, ""), "\n")...)
	}
	for index := range out {
		out[index] = ansi.Truncate(out[index], width, "")
	}
	return out
}
