package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/zhangsizhou/terminal-resume/internal/report"
)

// newTestReporter 造一个指向本地假站点的上报器（单测绝不打真实网络）。
func newTestReporter(t *testing.T, url string) *report.Reporter {
	t.Helper()
	reporter, err := report.New(url, report.Meta{Version: Version, Source: report.SourceLocal})
	if err != nil {
		t.Fatal(err)
	}
	return reporter
}

// TestModelHasNoReporterByDefault 锁住「New 不装上上报器」这条底线。
//
// 一旦 New 自己装了，profile.website 是线上地址，
// 每个走 submit 的测试都会在跑测试时真的往作品集站发请求——
// 测试机网络一通，就会往线上写一堆假会话。
func TestModelHasNoReporterByDefault(t *testing.T) {
	m := testModel(t)
	if m.Reporter() != nil {
		t.Fatal("New 不应自带上报器（会让单测打到真实站点）")
	}
	// 没装上上报器时，提交必须照常工作，且不能 panic。
	m.submitWithReport("/skills")
	if len(m.chat) == 0 {
		t.Fatal("提交仍然应该产生回答")
	}
}

// TestSubmitRecordsCommandAndQuestion 记录的是访客侧两条：命令与提问。
func TestSubmitRecordsCommandAndQuestion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	m := testModel(t)
	reporter := newTestReporter(t, server.URL)
	m.SetReporter(reporter)

	m.submitWithReport("/skills")
	m.submitWithReport("他做过哪些项目？")

	entries := reporter.Entries()
	if len(entries) != 2 {
		t.Fatalf("记录了 %d 条，期望 2 条", len(entries))
	}
	if entries[0].Kind != report.KindCommand || entries[0].Text != "/skills" {
		t.Fatalf("第一条应记为命令：%+v", entries[0])
	}
	if entries[1].Kind != report.KindQuestion || entries[1].Text != "他做过哪些项目？" {
		t.Fatalf("第二条应记为提问：%+v", entries[1])
	}
}

// TestTypoedCommandIsStillRecorded 打错字的命令也要记：
// 邮件里能看到访客卡在哪一步，这正是记录的价值所在。
func TestTypoedCommandIsStillRecorded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	m := testModel(t)
	reporter := newTestReporter(t, server.URL)
	m.SetReporter(reporter)
	m.submitWithReport("/porjects")

	entries := reporter.Entries()
	if len(entries) != 1 || entries[0].Kind != report.KindCommand {
		t.Fatalf("打错的命令也应记为 command：%+v", entries)
	}
}

// TestBlankSubmitIsNotRecorded 空提交（Enter 敲在空输入框上）不该留下记录。
func TestBlankSubmitIsNotRecorded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	m := testModel(t)
	reporter := newTestReporter(t, server.URL)
	m.SetReporter(reporter)
	m.submitWithReport("   ")
	if len(reporter.Entries()) != 0 {
		t.Fatalf("空提交不该记录：%+v", reporter.Entries())
	}
}

// TestEnterKeyRoutesThroughReporting 走真实按键路径（而不是直接调 submit）：
// 这条链路是访客真正的入口，绕过它就等于没测。
func TestEnterKeyRoutesThroughReporting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	m := testModel(t)
	reporter := newTestReporter(t, server.URL)
	m.SetReporter(reporter)

	m.input.SetValue("/log")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got, ok := updated.(Model)
	if !ok {
		t.Fatal("Update 应返回 Model")
	}
	entries := got.Reporter().Entries()
	if len(entries) != 1 || entries[0].Text != "/log" {
		t.Fatalf("按 Enter 提交应记录一条：%+v", entries)
	}
}

// TestReportCmdIsSkipable 上报任务为空时不产生命令（nil 任务不该变成一条空转命令）。
func TestReportCmdIsSkipable(t *testing.T) {
	if cmd := reportCmd(nil); cmd != nil {
		t.Fatal("nil 任务应返回 nil 命令")
	}
	called := false
	cmd := reportCmd(func() { called = true })
	if cmd == nil {
		t.Fatal("有任务就该返回命令")
	}
	if msg := cmd(); msg != (reportSentMsg{}) {
		t.Fatalf("上报命令应回一条空消息，实际 %T", msg)
	}
	if !called {
		t.Fatal("命令执行时任务没跑")
	}
}

// TestReportSentMsgIsInert 上报回声不该改动界面（尤其是不能把输入框挪位）。
func TestReportSentMsgIsInert(t *testing.T) {
	m := testModel(t)
	before := m.View().Content
	updated, cmd := m.Update(reportSentMsg{})
	if cmd != nil {
		t.Fatal("上报回声不应产生新命令")
	}
	if got := updated.(Model).View().Content; got != before {
		t.Fatal("上报回声改变了画面")
	}
}
