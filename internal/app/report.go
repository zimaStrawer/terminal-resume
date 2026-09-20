package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/zhangsizhou/terminal-resume/internal/report"
)

// 会话上报：把访客敲过的命令与提过的问题记下来，会话结束时由站点侧汇总发一封邮件。
//
// ⚠️ 上报器**不由 New 装配**：New 会被单测直接调用，一装上报器，
// 每个走 submit 的测试都会真的往站点发请求（profile.website 是线上地址）。
// 谁要上报谁显式装（main 的本地分支 / SSH 中间件），默认一律关闭。
func (m *Model) SetReporter(r *report.Reporter) {
	m.reporter = r
}

// Reporter 返回当前会话的上报器（未装配时为 nil）。
// 外层（main / SSH 中间件）拿它去收尾——见 Reporter.Finish。
func (m Model) Reporter() *report.Reporter {
	return m.reporter
}

// recordSubmission 记录一次提交，返回这次的上报任务；nil 表示无需上报。
//
// 分类只看有没有斜杠前缀：`/skills` 是命令，`他做过哪些项目` 是提问。
// 命令打错（`/porjects`）也算命令——访客确实敲的是命令，记下来才知道他卡在哪。
//
// AI 的回答**不记**：用户明确只要访客侧，回答又长，进邮件只会把时间线冲垮。
func (m *Model) recordSubmission(raw string) func() {
	if m.reporter == nil {
		return nil
	}
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	kind := report.KindQuestion
	if strings.HasPrefix(text, "/") {
		kind = report.KindCommand
	}
	return m.reporter.Record(kind, text)
}

// reportSentMsg 是上报任务跑完后的回声，不携带任何信息——
// 存在的唯一理由是 Bubble Tea 的命令必须返回一条消息。
type reportSentMsg struct{}

// reportCmd 把上报任务包成命令：它在 Bubble Tea 自己的 goroutine 里跑，
// 所以一次网络往返不会卡住界面；跑完回一条空消息，界面什么都不做。
func reportCmd(job func()) tea.Cmd {
	if job == nil {
		return nil
	}
	return func() tea.Msg {
		job()
		return reportSentMsg{}
	}
}

// submitWithReport = 先记账，再执行。
//
// 上报任务与命令本身打成一个 Batch：两条路互不等待，
// 所以「上报请求要不要花三秒」跟「回答什么时候开始打」彻底无关。
func (m *Model) submitWithReport(raw string) tea.Cmd {
	job := m.recordSubmission(raw)
	return tea.Batch(reportCmd(job), m.submit(raw))
}
