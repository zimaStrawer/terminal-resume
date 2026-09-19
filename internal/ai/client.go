// Package ai 是作品集站后端（Cloudflare Pages Function `functions/api/chat.ts`）的客户端。
//
// 终端侧**只发请求、不持有任何密钥**：Key 永远留在 Cloudflare 的环境变量里，
// 所以预编译的二进制（仓库里有 6 个平台的产物）即使被下载也拿不到东西。
//
// 协议与服务端一一对应（见 Zima_room2.0/functions/api/chat.ts）：
//
//	POST {site}/api/chat
//	Content-Type: application/json
//	{ "messages": [ { "role": "user" | "assistant", "content": "…" } ] }
//
//	200 -> SSE：data: {"choices":[{"delta":{"content":"…"}}]} … data: [DONE]
//	非 200 -> JSON：{ "error": { "code": "…", "message": "…" } }
//
// 注意：服务端会做同源校验（带 Origin 且不同源则 403）。Go 客户端默认不发 Origin，
// 所以能正常通过；这里也不要画蛇添足地补一个 Origin 头。
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// 与服务端 functions/api/chat.ts 的限额保持一致。
// 超限会被服务端判成 INVALID_MESSAGES 直接 400，所以在客户端就先裁好，
// 少一次往返、也少一次让访客看到报错的机会。
const (
	MaxMessages       = 12
	MaxUserRunes      = 1_500
	MaxAssistantRunes = 4_000
	MaxTotalRunes     = 10_000
)

// requestTimeout 比服务端 45s 的上游超时略长，让服务端有机会把
// 「回答等待时间过长，请重试」这句可展示的错误返回来，而不是我们这边先断。
const requestTimeout = 55 * time.Second

// Message 是一条对话消息，字段名与 OpenAI 兼容接口一致。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// ServiceError 是服务端返回的、可以直接展示给访客的错误（message 已经是中文）。
type ServiceError struct {
	Status  int
	Code    string
	Message string
}

func (e *ServiceError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("AI 服务返回 %d。", e.Status)
}

// ErrNotConfigured 表示没有可用的后端地址，自由问答整体不可用。
var ErrNotConfigured = errors.New("未配置 AI 服务地址")

// Client 是 /api/chat 的极简客户端。
type Client struct {
	endpoint  string
	userAgent string
	http      *http.Client
}

// NewClient 用站点根地址构造客户端（例如 https://example.com，会自动补 /api/chat）。
func NewClient(baseURL, userAgent string) (*Client, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return nil, ErrNotConfigured
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("AI 服务地址无效：%w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("AI 服务地址无效：%q", base)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/chat"
	parsed.RawQuery, parsed.Fragment = "", ""
	// 传输层用默认的就行，别自造绕过代理的 Transport。
	// 曾怀疑「shell 里的 HTTP_PROXY 会把本地联调（--api http://127.0.0.1:8788）
	// 也拐去代理」，实测不成立：Go 的 http.ProxyFromEnvironment 自带回环排除，
	// 设着 HTTP_PROXY 时 127.0.0.1 / localhost / [::1] 一律返回 nil，只有公网才走代理。
	return &Client{
		endpoint:  parsed.String(),
		userAgent: userAgent,
		http:      &http.Client{Timeout: requestTimeout},
	}, nil
}

// Endpoint 返回实际请求的地址，便于日志与排查。
func (c *Client) Endpoint() string {
	if c == nil {
		return ""
	}
	return c.endpoint
}

// Stream 发送一轮对话，每收到一个增量分片就调用一次 onDelta；
// 返回 nil 说明流正常结束（含收到 [DONE]）。
// 错误只可能是 *ServiceError（服务端给了可展示文案）或网络/上下文错误。
func (c *Client) Stream(ctx context.Context, messages []Message, onDelta func(string)) error {
	if c == nil {
		return ErrNotConfigured
	}

	payload, err := json.Marshal(map[string][]Message{"messages": prepare(messages)})
	if err != nil {
		return fmt.Errorf("编码请求失败：%w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("构造请求失败：%w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	if c.userAgent != "" {
		request.Header.Set("User-Agent", c.userAgent)
	}

	response, err := c.http.Do(request)
	if err != nil {
		// 用户主动中止（Esc）会走到这里，交给上层静默处理。
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return ctx.Err()
		}
		// 传输层错误不能直接透给访客看（`…: EOF` 这种没人看得懂），
		// 交给 describeTransportError 翻译成人话 + 出路。
		return describeTransportError(err, c.endpoint)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return decodeServiceError(response)
	}
	return readSSE(response.Body, onDelta)
}

// decodeServiceError 把服务端的错误体翻译成可展示的错误。
func decodeServiceError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	var envelope struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil && envelope.Error.Message != "" {
		return &ServiceError{
			Status:  response.StatusCode,
			Code:    envelope.Error.Code,
			Message: envelope.Error.Message,
		}
	}
	return &ServiceError{Status: response.StatusCode}
}

// readSSE 逐行消费 SSE 流。按 SSE 规范，一个事件可能跨多行，
// 这里只关心 `data:` 行——服务端的每个分片都是单行 JSON。
func readSSE(body io.Reader, onDelta func(string)) error {
	reader := bufio.NewReaderSize(body, 64<<10)
	for {
		line, err := reader.ReadString('\n')
		if payload, ok := dataLine(line); ok {
			if payload == "[DONE]" {
				return nil
			}
			if text := deltaText(payload); text != "" && onDelta != nil {
				onDelta(text)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				// 上游没发 [DONE] 就断了：已经收到的内容仍然有效，不算失败。
				return nil
			}
			return err
		}
	}
}

// dataLine 从一行 SSE 里取出 `data:` 后面的内容。
func dataLine(line string) (string, bool) {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimSpace(line[len("data:"):]), true
}

// deltaText 从一段 SSE data 里取出增量文本。
// 兼容 delta.content（流式）与 message.content（上游偶尔退化成的非流式整包）。
func deltaText(payload string) string {
	var chunk struct {
		Choices []struct {
			Delta   struct{ Content string } `json:"delta"`
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal([]byte(payload), &chunk) != nil || len(chunk.Choices) == 0 {
		return ""
	}
	if text := chunk.Choices[0].Delta.Content; text != "" {
		return text
	}
	return chunk.Choices[0].Message.Content
}

// prepare 按服务端限额裁剪要发送的历史：从最新的往回取，最多 12 条、总长 10,000 字。
// 本地页面（斜杠命令产生的回答）在调用前就已经排除，不在这里处理。
func prepare(messages []Message) []Message {
	kept := make([]Message, 0, min(len(messages), MaxMessages))
	total := 0
	for index := len(messages) - 1; index >= 0 && len(kept) < MaxMessages; index-- {
		content := strings.TrimSpace(messages[index].Content)
		if content == "" {
			continue
		}
		limit := MaxAssistantRunes
		role := messages[index].Role
		if role == RoleUser {
			limit = MaxUserRunes
		}
		content = truncateRunes(content, limit)
		length := utf8.RuneCountInString(content)
		if total+length > MaxTotalRunes {
			break
		}
		total += length
		kept = append(kept, Message{Role: role, Content: content})
	}
	// kept 是倒序取的，翻回来才是对话顺序。
	for left, right := 0, len(kept)-1; left < right; left, right = left+1, right-1 {
		kept[left], kept[right] = kept[right], kept[left]
	}
	return kept
}

// truncateRunes 按字符（不是字节）截断，避免把中文切一半。
func truncateRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit])
}
