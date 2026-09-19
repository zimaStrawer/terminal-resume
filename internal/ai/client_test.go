package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestStreamCollectsDeltas 覆盖正常流：多条 data 分片拼成完整回答，[DONE] 收尾。
func TestStreamCollectsDeltas(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("content-type = %q", ct)
		}
		// 服务端做了同源校验，Go 客户端不能带 Origin。
		if origin := r.Header.Get("Origin"); origin != "" {
			t.Errorf("Origin should not be sent, got %q", origin)
		}
		gotBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "text/event-stream")
		for _, delta := range []string{"他", "常驻", "杭州。"} {
			chunk, _ := json.Marshal(map[string]any{
				"choices": []any{map[string]any{"delta": map[string]string{"content": delta}}},
			})
			w.Write([]byte("data: " + string(chunk) + "\n\n"))
		}
		w.Write([]byte("data: [DONE]\n\n"))
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"不该出现\"}}]}\n\n"))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-agent")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if endpoint := client.Endpoint(); endpoint != server.URL+"/api/chat" {
		t.Fatalf("endpoint = %q", endpoint)
	}

	var collected strings.Builder
	err = client.Stream(context.Background(), []Message{
		{Role: RoleUser, Content: "他在哪？"},
	}, func(delta string) { collected.WriteString(delta) })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got := collected.String(); got != "他常驻杭州。" {
		t.Fatalf("collected = %q, want 他常驻杭州。", got)
	}

	var sent struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("decode request body: %v\n%s", err, gotBody)
	}
	if len(sent.Messages) != 1 || sent.Messages[0].Content != "他在哪？" {
		t.Fatalf("request messages = %+v", sent.Messages)
	}
}

// TestStreamSurfacesServiceError 覆盖服务端可展示错误（例如越界拦截 / 限流）。
func TestStreamSurfacesServiceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"code":"RATE_LIMITED","message":"AI 服务当前请求较多，请稍后重试。"}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.Stream(context.Background(), []Message{{Role: RoleUser, Content: "你好"}}, nil)
	var service *ServiceError
	if !errors.As(err, &service) {
		t.Fatalf("err = %v, want *ServiceError", err)
	}
	if service.Status != http.StatusTooManyRequests || service.Code != "RATE_LIMITED" {
		t.Fatalf("service error = %+v", service)
	}
	if service.Error() != "AI 服务当前请求较多，请稍后重试。" {
		t.Fatalf("message = %q", service.Error())
	}
}

// TestStreamFallsBackWhenErrorBodyIsNotJSON 覆盖没有错误体的情况，至少给出状态码。
func TestStreamFallsBackWhenErrorBodyIsNotJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("upstream exploded"))
	}))
	defer server.Close()

	client, _ := NewClient(server.URL, "")
	err := client.Stream(context.Background(), []Message{{Role: RoleUser, Content: "你好"}}, nil)
	var service *ServiceError
	if !errors.As(err, &service) {
		t.Fatalf("err = %v, want *ServiceError", err)
	}
	if service.Code != "" || !strings.Contains(service.Error(), "502") {
		t.Fatalf("message = %q", service.Error())
	}
}

// TestStreamReturnsContextErrorOnCancel 覆盖用户按 Esc 中止：必须是 ctx 错误，
// 上层据此静默处理，不能弹成「服务异常」。
func TestStreamReturnsContextErrorOnCancel(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"半\"}}]}\n\n"))
		w.(http.Flusher).Flush()
		<-release
	}))
	defer server.Close()
	defer close(release)

	client, _ := NewClient(server.URL, "")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err := client.Stream(ctx, []Message{{Role: RoleUser, Content: "你好"}}, func(string) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestNewClientRejectsEmptyOrBareBase 覆盖地址校验。
func TestNewClientRejectsEmptyOrBareBase(t *testing.T) {
	if _, err := NewClient("   ", ""); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("empty base err = %v, want ErrNotConfigured", err)
	}
	if _, err := NewClient("zhang.design", ""); err == nil {
		t.Fatal("missing scheme should be rejected")
	}
}

// TestPrepareKeepsLatestWithinLimits 锁死「发送前先按服务端限额裁剪」的行为。
func TestPrepareKeepsLatestWithinLimits(t *testing.T) {
	messages := make([]Message, 0, 20)
	for index := 0; index < 20; index++ {
		messages = append(messages, Message{Role: RoleUser, Content: string(rune('a' + index))})
	}
	got := prepare(messages)
	if len(got) != MaxMessages {
		t.Fatalf("kept %d messages, want %d", len(got), MaxMessages)
	}
	// 保留的必须是最近的 12 条，且顺序是对话顺序（不是倒序）。
	if got[0].Content != "i" || got[len(got)-1].Content != "t" {
		t.Fatalf("window = %q … %q, want i … t", got[0].Content, got[len(got)-1].Content)
	}
}

// TestPrepareTruncatesOverlongMessage 确保超长单条被按字符截断（不是字节切一半）。
func TestPrepareTruncatesOverlongMessage(t *testing.T) {
	got := prepare([]Message{
		{Role: RoleUser, Content: strings.Repeat("张", MaxUserRunes+200)},
	})
	if len(got) != 1 {
		t.Fatalf("kept %d messages, want 1", len(got))
	}
	if runes := len([]rune(got[0].Content)); runes != MaxUserRunes {
		t.Fatalf("length = %d runes, want %d", runes, MaxUserRunes)
	}
}

// TestPrepareDropsBlankMessages 空内容不能进请求体（服务端会判 400）。
func TestPrepareDropsBlankMessages(t *testing.T) {
	got := prepare([]Message{
		{Role: RoleUser, Content: "   "},
		{Role: RoleAssistant, Content: "\n"},
		{Role: RoleUser, Content: "他在哪？"},
	})
	if len(got) != 1 || got[0].Content != "他在哪？" {
		t.Fatalf("prepare = %+v", got)
	}
}
