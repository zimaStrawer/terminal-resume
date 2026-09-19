package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/zhangsizhou/terminal-resume/internal/ai"
	"github.com/zhangsizhou/terminal-resume/profile"
)

// fakeAI 是 aiStreamer 的测试替身：不发任何网络请求，按剧本回吐增量。
// seen 记录每一次收到的请求历史，用来断言「发出去的到底是什么」。
type fakeAI struct {
	mu    sync.Mutex
	delta []string
	err   error
	seen  [][]ai.Message
}

func (f *fakeAI) Stream(ctx context.Context, messages []ai.Message, onDelta func(string)) error {
	f.mu.Lock()
	f.seen = append(f.seen, append([]ai.Message(nil), messages...))
	delta, err := f.delta, f.err
	f.mu.Unlock()

	for _, text := range delta {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		onDelta(text)
	}
	return err
}

func (f *fakeAI) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen)
}

func (f *fakeAI) lastRequest(t *testing.T) []ai.Message {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) == 0 {
		t.Fatal("AI 后端根本没有被调用")
	}
	return f.seen[len(f.seen)-1]
}

// drainCommands 手工驱动一遍 tea.Cmd 链：把命令产出的消息喂回 Update，直到没有新消息。
// 真实运行时由 bubbletea 的事件循环做这件事；测试里自己跑是为了不用真开一个 Program。
func drainCommands(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for rounds := 0; len(queue) > 0; rounds++ {
		if rounds > 2000 {
			t.Fatal("命令链没有收敛（大概率是 tick 自续了）")
		}
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		switch msg := next().(type) {
		case nil:
			// 流已经关掉、或者命令本身没有后续，跳过。
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			updated, follow := m.Update(msg)
			model, ok := updated.(Model)
			if !ok {
				t.Fatalf("Update 返回 %T，want app.Model", updated)
			}
			*m = model
			queue = append(queue, follow)
		}
	}
}

// askAndSettle 提交一段输入，并把整条命令链跑干净（含流式打字机）。
func askAndSettle(t *testing.T, m *Model, text string) {
	t.Helper()
	drainCommands(t, m, m.submit(text))
}

// TestFreeformQuestionStreamsAnswerFromBackend 覆盖主链路：
// 自由输入 -> 打进 AI 后端 -> 增量回到对话流 -> 打字机追平。
func TestFreeformQuestionStreamsAnswerFromBackend(t *testing.T) {
	m := testModel(t)
	backend := &fakeAI{delta: []string{"他", "常驻", "杭州。"}}
	m.aiClient = backend

	askAndSettle(t, &m, "他在哪？")

	if got := answer(t, m); got != "他常驻杭州。" {
		t.Fatalf("answer = %q, want 他常驻杭州。", got)
	}
	if len(m.chat) != 2 || !m.chat[0].ai || !m.chat[1].ai {
		t.Fatalf("提问与回答都应标成 AI 线，chat = %+v", m.chat)
	}
	if !m.chat[1].done() || m.streaming || m.awaiting {
		t.Fatalf("流没有收干净：message=%+v streaming=%v awaiting=%v", m.chat[1], m.streaming, m.awaiting)
	}
	request := backend.lastRequest(t)
	if len(request) != 1 || request[0].Role != ai.RoleUser || request[0].Content != "他在哪？" {
		t.Fatalf("发给后端的历史 = %+v", request)
	}
}

// TestLocalCommandAnswersStayOutOfAIHistory 确保本地页面不挤掉真正的对话上下文。
func TestLocalCommandAnswersStayOutOfAIHistory(t *testing.T) {
	m := testModel(t)
	backend := &fakeAI{delta: []string{"好。"}}
	m.aiClient = backend

	askAndSettle(t, &m, "/skills")
	askAndSettle(t, &m, "他擅长什么？")

	request := backend.lastRequest(t)
	if len(request) != 1 || request[0].Content != "他擅长什么？" {
		t.Fatalf("发给 AI 的历史应当只有那一问，实际 = %+v", request)
	}
	// 本地那条回答仍留在对话流里（只是不进 AI 历史）。
	if len(m.chat) != 4 {
		t.Fatalf("对话流条数 = %d, want 4", len(m.chat))
	}
	if m.chat[1].ai {
		t.Fatal("斜杠命令的回答不该被标成 AI 线")
	}
}

// TestTypoedCommandSuggestsInsteadOfAskingAI 保证「打错字」和「自由提问」分得开。
func TestTypoedCommandSuggestsInsteadOfAskingAI(t *testing.T) {
	m := testModel(t)
	backend := &fakeAI{}
	m.aiClient = backend

	askAndSettle(t, &m, "porjects")

	if calls := backend.callCount(); calls != 0 {
		t.Fatalf("打错字不该发给 AI，实际调用了 %d 次", calls)
	}
	if !strings.Contains(m.notice, "/projects") {
		t.Fatalf("notice = %q, want 提示 /projects", m.notice)
	}
	if len(m.chat) != 0 {
		t.Fatalf("对话流应为空，实际 %d 条", len(m.chat))
	}
}

// TestSlashPrefixedUnknownCommandNeverHitsAI 保证 / 开头的输入不会被瞎猜成提问。
func TestSlashPrefixedUnknownCommandNeverHitsAI(t *testing.T) {
	m := testModel(t)
	backend := &fakeAI{}
	m.aiClient = backend

	askAndSettle(t, &m, "/nope")

	if calls := backend.callCount(); calls != 0 {
		t.Fatalf("/ 开头的输入不该发给 AI，实际调用了 %d 次", calls)
	}
	if m.noticeKind != noticeError || !strings.Contains(m.notice, "/nope") {
		t.Fatalf("notice = %q kind = %v", m.notice, m.noticeKind)
	}
}

// TestAIFailureDropsEmptyAnswer 覆盖「一个字都没收到就失败」：
// 空回答要从对话流里删掉，只留底部提示，不能挂一条永远的 "…"。
func TestAIFailureDropsEmptyAnswer(t *testing.T) {
	m := testModel(t)
	backend := &fakeAI{err: &ai.ServiceError{
		Status:  429,
		Code:    "RATE_LIMITED",
		Message: "AI 服务当前请求较多，请稍后重试。",
	}}
	m.aiClient = backend

	askAndSettle(t, &m, "他在哪？")

	if m.notice != "AI 服务当前请求较多，请稍后重试。" || m.noticeKind != noticeError {
		t.Fatalf("notice = %q kind = %v", m.notice, m.noticeKind)
	}
	if len(m.chat) != 1 || m.chat[0].role != chatUser {
		t.Fatalf("空回答应被删掉，chat = %+v", m.chat)
	}
}

// TestAIFailureKeepsPartialAnswer 覆盖「收到一半才断」：半截回答照样留着。
func TestAIFailureKeepsPartialAnswer(t *testing.T) {
	m := testModel(t)
	backend := &fakeAI{
		delta: []string{"他常驻"},
		err:   &ai.ServiceError{Status: 502, Code: "UPSTREAM_ERROR", Message: "AI 服务未能完成请求，请稍后重试。"},
	}
	m.aiClient = backend

	askAndSettle(t, &m, "他在哪？")

	if got := answer(t, m); got != "他常驻" {
		t.Fatalf("answer = %q, want 他常驻", got)
	}
	if len(m.chat) != 2 {
		t.Fatalf("半截回答不该被删，chat 条数 = %d", len(m.chat))
	}
	if !strings.Contains(m.notice, "未能完成请求") {
		t.Fatalf("notice = %q", m.notice)
	}
}

// TestEscStopsInFlightRequest 覆盖用户按 Esc 中止生成。
func TestEscStopsInFlightRequest(t *testing.T) {
	m := testModel(t)
	m.aiClient = &fakeAI{}
	// submit 会把状态推进到「在途」；返回的命令不执行，模拟首字节还没到。
	_ = m.submit("他在哪？")
	if !m.awaiting {
		t.Fatal("submit 之后应当处于等待后端的状态")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)

	if m.awaiting {
		t.Fatal("Esc 之后不该还在等后端")
	}
	if m.aiCancel != nil {
		t.Fatal("Esc 之后应当清掉取消函数")
	}
	if m.notice != "已停止生成。" || m.noticeKind != noticeInfo {
		t.Fatalf("notice = %q kind = %v", m.notice, m.noticeKind)
	}
	// 一个字都还没到，那条空回答应当被删掉。
	if len(m.chat) != 1 {
		t.Fatalf("chat 条数 = %d, want 1（只剩提问）", len(m.chat))
	}
}

// TestStaleStreamEventsAreIgnored 覆盖「旧流还在吐，但已经被作废」。
func TestStaleStreamEventsAreIgnored(t *testing.T) {
	m := testModel(t)
	m.aiClient = &fakeAI{}
	_ = m.submit("第一问")
	stale := m.aiSeq

	m.cancelAI() // 相当于按了 Esc / 又提了一问
	updated, _ := m.Update(aiDeltaMsg{seq: stale, text: "不该出现"})
	m = updated.(Model)

	if got := answer(t, m); got != "" {
		t.Fatalf("陈旧事件被写进了对话流：%q", got)
	}
	updated, _ = m.Update(aiDoneMsg{seq: stale, err: &ai.ServiceError{Message: "不该出现"}})
	m = updated.(Model)
	if m.notice == "不该出现" {
		t.Fatal("陈旧流的错误不该弹提示")
	}
}

// TestFreeformQuestionWithoutBackendExplainsHow 覆盖没配后端时的降级：
// 不静默失败，而是说清怎么开。
func TestFreeformQuestionWithoutBackendExplainsHow(t *testing.T) {
	m := testModel(t)
	m.SetAPIBase("")

	askAndSettle(t, &m, "他在哪？")

	if len(m.chat) != 0 {
		t.Fatalf("没有后端时不该往对话流里写东西，chat = %+v", m.chat)
	}
	if !strings.Contains(m.notice, "--api") {
		t.Fatalf("notice = %q, 应当提示 --api", m.notice)
	}
}

// TestNewTakesAPIBaseFromProfileWebsite 锁死默认值来源：
// AI 助手和简历挂同一个站点，不需要额外配置。
func TestNewTakesAPIBaseFromProfileWebsite(t *testing.T) {
	resume, err := profile.Load("")
	if err != nil {
		t.Fatal(err)
	}
	m := New(resume, true)
	want := strings.TrimRight(resume.Profile.Website, "/") + "/api/chat"
	if m.APIBase() != want {
		t.Fatalf("APIBase = %q, want %q", m.APIBase(), want)
	}
	if !strings.HasSuffix(m.APIBase(), "/api/chat") {
		t.Fatalf("APIBase = %q, want 以 /api/chat 结尾", m.APIBase())
	}
}

// TestLookLikeCommandTypo 固定「打错字 vs 自由提问」的判定边界。
// 关键是别把普通英文词拦成命令提示：hello 距 help 恰好 2 步，必须放行。
func TestLookLikeCommandTypo(t *testing.T) {
	typos := []string{
		"porjects", // 相邻字母写反（靠 Damerau 换位才算 1 步）
		"skils",
		"persnal",
		"exitt",
		"projec",  // 少了最后一个字母
		"statuss", // 多了一个字母
		"contct",
	}
	for _, text := range typos {
		if !looksLikeCommandTypo(text) {
			t.Errorf("%q 应当被判成打错的命令", text)
		}
	}

	freeform := []string{
		"hello", // 距 help 两步：绝不能被拦成命令提示
		"他在哪？",
		"你是谁",
		"讲讲你的项目",
		"what do you do",
		"abc", // 太短，不足以判定
	}
	for _, text := range freeform {
		if looksLikeCommandTypo(text) {
			t.Errorf("%q 是自由提问，不该判成打错的命令", text)
		}
	}
}
