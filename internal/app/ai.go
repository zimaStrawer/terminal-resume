package app

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/zhangsizhou/terminal-resume/internal/ai"
)

// DefaultAPIBase 是自由问答默认打的后端地址：作品集站上的 Cloudflare Pages Function
// （`/api/chat`）。终端侧只发请求、不持密钥，换成别的域名或本地联调时用 --api 覆盖。
//
// 注意：它是保底值，真实默认取自 profile/resume.yaml 的 profile.website，
// 也就是「AI 助手和简历挂在同一个站点上」。
//
// ⚠️ 这里必须填**真实已部署**的地址。曾经填的占位域名（zhang.design）根本没注册，
// 访客一提问就只看到一行 `…: EOF`——地址写错是这条链路上最难自查的故障。
const DefaultAPIBase = "https://zhangsizhou.pages.dev"

// aiStreamer 是 app 侧对 AI 客户端的最小契约：发一轮、收增量。
// 抽成接口只有一个目的——单测能注入假实现，不让测试真的打网络。
type aiStreamer interface {
	Stream(ctx context.Context, messages []ai.Message, onDelta func(string)) error
}

// aiEvent 是流式过程中的一次事件。它在后台 goroutine 里产生，
// 经 channel 转成 tea 消息送回 Elm 循环。
type aiEvent struct {
	seq   int
	delta string
	err   error
	done  bool
}

// aiDeltaMsg / aiDoneMsg 是流式事件对应的 tea 消息。
// seq 用来作废旧流：用户按了 Esc、或又提了下一问，旧流还会吐几个事件出来，
// 靠 seq 对不上号把它们丢掉。
type aiDeltaMsg struct {
	seq  int
	text string
}

type aiDoneMsg struct {
	seq int
	err error
}

// SetAPIBase 指定作品集站地址（AI 后端挂在它的 /api/chat）。
// 传空串或非法地址表示关闭自由问答，只保留斜杠命令。
func (m *Model) SetAPIBase(baseURL string) {
	client, err := ai.NewClient(baseURL, "zhang-terminal-resume/"+Version)
	if err != nil {
		m.aiClient = nil
		m.aiBase = strings.TrimSpace(baseURL)
		return
	}
	m.aiClient = client
	m.aiBase = client.Endpoint()
}

// aiReady 判断自由问答是否可用。
func (m Model) aiReady() bool {
	return m.aiClient != nil
}

// APIBase 返回自由问答实际请求的地址（未配置时为空串），用于启动日志与测试。
func (m Model) APIBase() string {
	return m.aiBase
}

// listenAI 从 channel 取下一个事件转成 tea.Msg。
// 这是 bubbletea 里消费外部流的标准写法：一次命令只取一个事件，
// 处理完再挂上下一条，于是事件能逐条排队进 Elm 循环。
func listenAI(events <-chan aiEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return nil
		}
		if event.done {
			return aiDoneMsg{seq: event.seq, err: event.err}
		}
		return aiDeltaMsg{seq: event.seq, text: event.delta}
	}
}

// cancelAI 中止在途请求，并让后续到达的事件全部作废。
func (m *Model) cancelAI() {
	if m.aiCancel != nil {
		m.aiCancel()
		m.aiCancel = nil
	}
	m.awaiting = false
	// seq 前进一格：旧流余下的事件都对不上号，会被静默丢弃。
	m.aiSeq++
}

// askAI 把一条自由提问发给作品集的 /api/chat，并把回答流式打进对话流。
func (m *Model) askAI(prompt string) tea.Cmd {
	if !m.aiReady() {
		m.notice = "自由问答未配置：加 --api https://你的站点 后可用。"
		m.noticeKind = noticeInfo
		return nil
	}

	// 上一条还没结束就先掐断，避免两条流往同一条回答里插字。
	m.cancelAI()
	m.finishStreaming()
	m.chatOffset = 0

	m.chat = append(m.chat, newAIChatMessage(chatUser, prompt))
	m.chat = append(m.chat, newAIChatMessage(chatAssistant, ""))

	m.aiSeq++
	seq := m.aiSeq
	// 历史要在 append 之后取：最后一条正是这次的提问。
	history := m.aiHistory()

	ctx, cancel := context.WithCancel(context.Background())
	m.aiCancel = cancel
	m.awaiting = true

	// 带缓冲：UI 万一停一拍也不会把网络 goroutine 卡死。
	events := make(chan aiEvent, 64)
	m.aiEvents = events
	client := m.aiClient
	go func() {
		defer close(events)
		err := client.Stream(ctx, history, func(delta string) {
			events <- aiEvent{seq: seq, delta: delta}
		})
		events <- aiEvent{seq: seq, err: err, done: true}
	}()

	return tea.Batch(listenAI(events), m.startStreaming())
}

// handleAIDelta 把新到的增量追加到最后一条回答上。
// 注意只加 total、不动 shown：打字机自己会追上去，
// 于是「网络多快」和「字打多快」解耦，慢网络下也不会一次蹦一大段。
func (m *Model) handleAIDelta(msg aiDeltaMsg) tea.Cmd {
	if msg.seq != m.aiSeq || len(m.chat) == 0 {
		return nil
	}
	last := &m.chat[len(m.chat)-1]
	if last.role != chatAssistant {
		return nil
	}
	last.text += msg.text
	last.total = visibleRuneCount(last.text)
	return m.startStreaming()
}

// handleAIDone 收尾：把失败写进底部提示，已经收到的半截回答照样留着。
func (m *Model) handleAIDone(msg aiDoneMsg) tea.Cmd {
	if msg.seq != m.aiSeq {
		return nil
	}
	m.awaiting = false
	m.aiCancel = nil
	if msg.err == nil || errors.Is(msg.err, context.Canceled) {
		return nil
	}
	// 一个字都没收到就失败：把那条空回答删掉，只留底部提示，
	// 否则对话流里会挂着一条永远显示 "…" 的空消息。
	m.dropEmptyAnswer()
	m.notice = msg.err.Error()
	m.noticeKind = noticeError
	return nil
}

// dropEmptyAnswer 删掉最后那条还没吐字就结束的回答。
func (m *Model) dropEmptyAnswer() {
	if len(m.chat) == 0 {
		return
	}
	last := m.chat[len(m.chat)-1]
	if last.role == chatAssistant && strings.TrimSpace(ansi.Strip(last.text)) == "" {
		m.chat = m.chat[:len(m.chat)-1]
	}
}

// aiHistory 把「自由问答」这条线的消息整理成请求历史。
//
// 斜杠命令的回答（本地渲染的页面）刻意不进历史：它们又长、又是给人看的排版，
// 而服务端每轮只收 12 条 / 10,000 字，塞进去只会把真正的上下文挤掉。
// 需要项目细节时，AI 那边有它自己的系统提示词（functions/_shared/portfolioContext.ts）。
func (m Model) aiHistory() []ai.Message {
	history := make([]ai.Message, 0, len(m.chat))
	for _, message := range m.chat {
		if !message.ai {
			continue
		}
		text := strings.TrimSpace(ansi.Strip(message.text))
		if text == "" {
			continue
		}
		role := ai.RoleUser
		if message.role == chatAssistant {
			role = ai.RoleAssistant
		}
		history = append(history, ai.Message{Role: role, Content: text})
	}
	return history
}

// looksLikeCommandTypo 判断一段自由输入是不是"斜杠命令打错了"。
// 只有单个词、且与某条命令的编辑距离不超过 1 才算——
// 这样 `porjects` 会提示「你是否想输入 /projects」，而 `你是谁`、`hello` 会正常走 AI。
//
// 距离用带相邻换位的版本（Damerau-Levenshtein）：真实手误大多是「两个字母写反」，
// 换位必须只算 1 步，否则 `porjects` 会被算成 2 步。
//
// 阈值死卡在 1（而不是 2）是有意的：`hello` 距 `help` 恰好是 2 步，
// 一旦放到 2，最普通的英文问候就会被拦成「你是不是想输入 /help」——
// 拦错的代价（把自由问答堵住）远大于漏判（一次手误交给 AI，它照样答得出来）。
func looksLikeCommandTypo(text string) bool {
	fields := strings.Fields(text)
	if len(fields) != 1 {
		return false
	}
	word := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	if RuneCount(word) < 4 {
		return false
	}
	for _, command := range []string{
		"home", "whoami", "personal", "experience", "projects", "project",
		"skills", "status", "log", "random", "contact", "resume", "help",
		"theme", "clear", "exit",
	} {
		if levenshteinWithTransposition(word, command) <= 1 {
			return true
		}
	}
	return false
}

// levenshteinWithTransposition 是 Damerau-Levenshtein 的最优字符串对齐版：
// 相邻两个字符写反了只算 1 步（"porjects" → "projects"）。
func levenshteinWithTransposition(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}

	rows := make([][]int, len(ar)+1)
	for i := range rows {
		rows[i] = make([]int, len(br)+1)
		rows[i][0] = i
	}
	for j := range rows[0] {
		rows[0][j] = j
	}

	for i := 1; i <= len(ar); i++ {
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			best := min(rows[i-1][j]+1, rows[i][j-1]+1, rows[i-1][j-1]+cost)
			if i > 1 && j > 1 && ar[i-1] == br[j-2] && ar[i-2] == br[j-1] {
				best = min(best, rows[i-2][j-2]+1)
			}
			rows[i][j] = best
		}
	}
	return rows[len(ar)][len(br)]
}
