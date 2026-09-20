package report

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder 是一个记账用的假站点：把收到的每个请求体留下来，供断言检查。
type recorder struct {
	mu      sync.Mutex
	bodies  []payload
	paths   []string
	server  *httptest.Server
	failAll bool
}

func newRecorder(t *testing.T) *recorder {
	t.Helper()
	r := &recorder{}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.paths = append(r.paths, req.URL.Path)
		r.mu.Unlock()
		if r.failAll {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var body payload
		if err := json.NewDecoder(req.Body).Decode(&body); err == nil {
			r.mu.Lock()
			r.bodies = append(r.bodies, body)
			r.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *recorder) received() ([]payload, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]payload(nil), r.bodies...), append([]string(nil), r.paths...)
}

func TestNewDisabled(t *testing.T) {
	reporter, err := New("", Meta{})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("空地址应返回 ErrDisabled，实际 %v", err)
	}
	if reporter != nil {
		t.Fatalf("空地址不应返回上报器")
	}
	// 关掉的上报器（以及 nil）所有方法都必须是安全的空操作：
	// 调用方到处判空太容易漏，这里就是那道保险。
	if reporter.Enabled() || reporter.SessionID() != "" || reporter.Entries() != nil {
		t.Fatal("关闭的上报器应完全空转")
	}
	reporter.Finish(context.Background())
	if job := reporter.Record(KindCommand, "/skills"); job != nil {
		t.Fatal("关闭的上报器不应产出上报任务")
	}
}

func TestNewRejectsBogusBase(t *testing.T) {
	for _, base := range []string{"not a url", "ftp://"} {
		if _, err := New(base, Meta{}); err == nil {
			t.Fatalf("%q 应当被拒", base)
		}
	}
}

func TestEndpointIsSessionAPI(t *testing.T) {
	rec := newRecorder(t)
	reporter, err := New(rec.server.URL+"/", Meta{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	reporter.Record(KindCommand, "/skills")()
	if _, paths := rec.received(); len(paths) != 1 || paths[0] != "/api/session" {
		t.Fatalf("上报应打到 /api/session，实际 %v", paths)
	}
}

func TestRecordPostsOneEntry(t *testing.T) {
	rec := newRecorder(t)
	reporter, err := New(rec.server.URL, Meta{Version: "test", Source: SourceLocal})
	if err != nil {
		t.Fatal(err)
	}
	job := reporter.Record(KindQuestion, "他做过哪些项目？")
	if job == nil {
		t.Fatal("开启的上报器应产出上报任务")
	}
	// 记录必须是**同步**完成的：任务还没跑，界面这边已经记上了。
	if entries := reporter.Entries(); len(entries) != 1 || entries[0].Text != "他做过哪些项目？" {
		t.Fatalf("记录未即时生效：%v", entries)
	}
	if bodies, _ := rec.received(); len(bodies) != 0 {
		t.Fatalf("任务执行前不应发出请求：%v", bodies)
	}
	job()
	bodies, _ := rec.received()
	if len(bodies) != 1 {
		t.Fatalf("应发出 1 个请求，实际 %d", len(bodies))
	}
	body := bodies[0]
	if body.Closed || body.Entry == nil || body.Entry.Kind != KindQuestion {
		t.Fatalf("逐条上报应为未闭合的单条：%+v", body)
	}
	if body.Meta.Source != SourceLocal || body.Meta.Version != "test" {
		t.Fatalf("元信息没带上：%+v", body.Meta)
	}
}

func TestRecordIgnoresBlankAndTruncates(t *testing.T) {
	rec := newRecorder(t)
	reporter, _ := New(rec.server.URL, Meta{})
	if job := reporter.Record(KindCommand, "   "); job != nil {
		t.Fatal("空白输入不应上报")
	}
	reporter.Record(KindQuestion, strings.Repeat("字", maxTextRunes+80))()
	bodies, _ := rec.received()
	if len(bodies) != 1 {
		t.Fatalf("应只发出 1 个请求，实际 %d", len(bodies))
	}
	if got := len([]rune(bodies[0].Entry.Text)); got != maxTextRunes {
		t.Fatalf("长文本应截断到 %d 字，实际 %d", maxTextRunes, got)
	}
}

func TestFinishSkipsEmptySession(t *testing.T) {
	rec := newRecorder(t)
	reporter, _ := New(rec.server.URL, Meta{})
	reporter.Finish(context.Background())
	if bodies, paths := rec.received(); len(bodies) != 0 || len(paths) != 0 {
		t.Fatalf("空会话必须一个包都不发（用户要求），实际 %d 个请求", len(paths))
	}
}

func TestFinishCarriesFullTranscript(t *testing.T) {
	rec := newRecorder(t)
	reporter, _ := New(rec.server.URL, Meta{Source: SourceSSH, Visitor: "a1b2c3", IP: "1.2.3.4"})
	reporter.Record(KindCommand, "/skills")
	reporter.Record(KindQuestion, "他现在在做什么？")
	reporter.Finish(context.Background())

	bodies, _ := rec.received()
	if len(bodies) != 1 {
		t.Fatalf("收尾应发 1 个请求，实际 %d", len(bodies))
	}
	body := bodies[0]
	if !body.Closed || body.ClosedAt == nil {
		t.Fatalf("收尾请求必须标记 closed 并带上结束时间：%+v", body)
	}
	if len(body.Entries) != 2 || body.Entries[0].Kind != KindCommand || body.Entries[1].Kind != KindQuestion {
		t.Fatalf("收尾必须带完整记录：%+v", body.Entries)
	}
	if body.SessionID == "" {
		t.Fatal("缺少会话 id")
	}
}

func TestFinishOnlyOnce(t *testing.T) {
	rec := newRecorder(t)
	reporter, _ := New(rec.server.URL, Meta{})
	reporter.Record(KindCommand, "/help")
	reporter.Finish(context.Background())
	reporter.Finish(context.Background())
	if _, paths := rec.received(); len(paths) != 1 {
		t.Fatalf("收尾只能发一次，实际 %d 次", len(paths))
	}
}

func TestPostFailureIsSilent(t *testing.T) {
	rec := newRecorder(t)
	rec.failAll = true
	reporter, _ := New(rec.server.URL, Meta{})
	// 站点返回 500、连接被拒、请求超时：三种情况都不该 panic，也不该有返回值。
	reporter.Record(KindCommand, "/skills")()
	reporter.Finish(context.Background())

	// 连不上（服务已关）也必须安静。
	rec.server.Close()
	reporter.finished = false
	reporter.Finish(context.Background())
}

func TestFinishRespectsTimeout(t *testing.T) {
	// 站点故意挂住：收尾必须在超时后自己放弃，不能把会话拖死。
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		time.Sleep(time.Second)
	}))
	defer slow.Close()

	reporter, _ := New(slow.URL, Meta{})
	reporter.Record(KindCommand, "/skills")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	reporter.Finish(ctx)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("收尾没遵守超时：等了 %v", elapsed)
	}
}

func TestSessionIDsDiffer(t *testing.T) {
	first, err := New("https://example.test", Meta{})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := New("https://example.test", Meta{})
	if first.SessionID() == "" || first.SessionID() == second.SessionID() {
		t.Fatalf("两次会话应拿到不同的 id：%q / %q", first.SessionID(), second.SessionID())
	}
}

func TestEntriesCap(t *testing.T) {
	reporter, _ := New("https://example.test", Meta{})
	for index := 0; index < maxEntries+20; index++ {
		reporter.Record(KindCommand, "/skills")
	}
	if got := len(reporter.Entries()); got != maxEntries {
		t.Fatalf("条目数应封顶在 %d，实际 %d", maxEntries, got)
	}
}
