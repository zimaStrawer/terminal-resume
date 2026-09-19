package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestLongAIAnswerWrapsInsteadOfTruncating 是这次「真实 AI 接入」暴露出来的回归。
//
// 真实 AI 回答通常是一整条**不含任何换行**的长句（见 functions/api/chat.ts 的 SSE 增量），
// 而旧版 renderChatMessage 对助手回答只做单行 ansi.Truncate —— 实测一条 168 字的回答
// 只显示出前 54 字，剩下的正文被静默丢掉，看起来就像"AI 话说一半"。
// 斜杠命令的本地回答自带换行，所以这个 bug 在纯本地阶段一直没暴露。
func TestLongAIAnswerWrapsInsteadOfTruncating(t *testing.T) {
	answerText := strings.Join([]string{
		"张思洲的精选项目包括：跑腿全链路优化、构序云体验适配、艺术画境产品设计以及 NeckDisco 创新应用。",
		"这些案例涵盖从 C 端消费体验到 B 端企业级平台，再到 AI 原生产品与空间音频交互的探索。",
		"每个项目都聚焦于解决复杂问题，通过设计策略与工程实现提升用户体验。",
	}, "")

	// 宽终端与窄终端都要验：访客的小终端就是窄的那种，
	// 而"只截断不折行"在窄屏下丢得更多。
	for _, terminalWidth := range []int{100, 64} {
		m := testModel(t)
		m.width, m.height = terminalWidth, 30
		m.recalculateLayout()

		// 真实链路是一片片送来的：这里也按 rune 切三片，
		// 顺带覆盖「完整文本边收边长」时折行是否会跟得上。
		runes := []rune(answerText)
		m.aiClient = &fakeAI{delta: []string{
			string(runes[:40]),
			string(runes[40:90]),
			string(runes[90:]),
		}}

		askAndSettle(t, &m, "他做过哪些项目？")

		width := m.viewport.Width()
		lines := m.renderChatMessage(m.chat[len(m.chat)-1], width)

		if len(lines) < 2 {
			t.Fatalf("%d 列下长回答只占了 %d 行（原文 %d 字、内容宽 %d 列），说明仍然被截断",
				terminalWidth, len(lines), RuneCount(answerText), width)
		}

		joined := ansi.Strip(strings.Join(lines, ""))
		// 一个字都不能少：折行只允许吃掉断行处的那个空格，正文必须原样保留。
		if got, want := strings.ReplaceAll(joined, " ", ""), strings.ReplaceAll(answerText, " ", ""); got != want {
			t.Fatalf("%d 列下折行后正文对不上\n got = %q\nwant = %q", terminalWidth, got, want)
		}
		// 并且空格也只能丢在断行处：n 行最多丢 n-1 个。
		if dropped := len(lines) - 1; strings.Count(answerText, " ")-strings.Count(joined, " ") > dropped {
			t.Fatalf("%d 列下丢了超过 %d 个空格，折行把正文里的空格也吃掉了", terminalWidth, dropped)
		}

		// 每一行都要能塞进内容宽度，否则会把输入框顶出屏幕。
		for index, line := range lines {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("%d 列下第 %d 行宽 %d 超过内容宽度 %d：%q",
					terminalWidth, index, got, width, ansi.Strip(line))
			}
		}

		// 行结构必须由完整文本决定：揭示进度退到一半时行数不变，
		// 否则输入框上方的整块内容会随着打字机推进一路抖动。
		partial := m.chat[len(m.chat)-1]
		partial.shown = RuneCount(answerText) / 2
		if mid := m.renderChatMessage(partial, width); len(mid) != len(lines) {
			t.Fatalf("%d 列下揭示到一半时行数从 %d 变成 %d，流式过程中会看到整块内容位移",
				terminalWidth, len(lines), len(mid))
		}
	}
}

// TestWrapChatTextLeavesLocalAnswersUntouched 锁住这次改动的"无副作用"：
// 斜杠命令的本地回答在各自的渲染函数里就折好了，wrapChatText 对它们必须逐字无操作，
// 否则 /skills、/log 这些既有排版会跟着一起漂移。
func TestWrapChatTextLeavesLocalAnswersUntouched(t *testing.T) {
	commands := []string{
		"skills", "resume", "contact", "log",
		"random", "help", "experience", "projects", "status",
	}
	for _, terminalWidth := range []int{100, 64} {
		m := testModel(t)
		m.width, m.height = terminalWidth, 30
		m.recalculateLayout()
		width := m.viewport.Width()

		for _, command := range commands {
			text, ok := m.commandAnswer(command, nil)
			if !ok {
				t.Fatalf("/%s 没有回答", command)
			}
			before := strings.Split(text, "\n")
			after := wrapChatText(text, width)
			if len(before) != len(after) {
				t.Fatalf("%d 列下 /%s 的行数从 %d 变成了 %d", terminalWidth, command, len(before), len(after))
			}
			for index := range before {
				if before[index] != after[index] {
					t.Fatalf("%d 列下 /%s 第 %d 行被改动了\n前 = %q\n后 = %q",
						terminalWidth, command, index, before[index], after[index])
				}
			}
		}
	}
}
