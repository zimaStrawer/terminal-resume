// Package report 上报「访客在终端里敲过什么」。
//
// 终端侧只上报、不发信：邮件密钥只存在于作品集站的 Cloudflare 环境变量里，
// 仓库里那 6 个平台的预编译二进制一个字节的密钥都没有（README 也是这么承诺的）。
// 站点侧的 /api/session 收下这些记录，会话结束时汇总成一封邮件。
//
// 上报分两段（2026-09-20 用户拍板的 B 方案）：
//  1. 每次提交立刻异步上报一条 —— 不阻塞界面，中途断网只丢尾巴；
//  2. 会话结束再发一条收尾请求，并带上**完整记录**当权威版本 ——
//     逐条上报全挂了（比如刚连上时还没网）也没关系，
//     只要关终端那一刻网络是通的，整份记录照样到得齐。
//
// 三条硬约束，改代码时别破：
//   - 失败一律静默：访客不该看到任何错误，也不该为上报多等一秒；
//   - 空会话（一次都没提交）连请求都不发；
//   - 只记访客侧：命令与提问，AI 的回答一条都不记（否则邮件会被长回答撑爆）。
//
// 上报器为 nil 时所有方法都是安全的空操作，调用方不用到处判空。
package report

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// 记录的两种类型。只区分到这个粒度：命令走本地渲染，提问走站点 AI。
const (
	KindCommand  = "command"
	KindQuestion = "question"
)

// 会话来源：SSH 访客与本地 --local 跑的是同一套记录逻辑，靠这个字段区分。
const (
	SourceSSH   = "ssh"
	SourceLocal = "local"
)

// 单条记录与单个会话的上限。超限就截断/丢弃——上报是尽力而为的旁路上报，
// 绝不能让一段超长输入把请求撑大、拖慢站点侧的处理。
const (
	maxTextRunes = 500
	maxEntries   = 200
)

// 逐条上报的超时。它在独立 goroutine 里跑，界面不等它；
// 给 5 秒是因为「后台慢慢发完」比「直接放弃」更值钱，而且没人看得到这段等待。
const entryTimeout = 5 * time.Second

// ErrDisabled 表示上报没开（地址为 off / 空）。它不是错误，只是「这次不用上报」。
var ErrDisabled = errors.New("会话上报未启用")

// Entry 是访客的一次提交。
type Entry struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
}

// Meta 是这一整个会话的环境信息。
//
// IP 与 Visitor 都是可选的：本地模式没有 SSH 会话，两者都为空。
// 它们只用于「限流」与「认出回头客」，不进邮件正文（脱敏在站点侧做）。
type Meta struct {
	Version string `json:"version"`
	Source  string `json:"source"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Mono    bool   `json:"mono"`
	// Visitor 是 SSH 公钥指纹的截断哈希（12 位十六进制）：同一个人换网络也不变，
	// 用来在邮件里标「第几次来访」。它是哈希，推不出私钥。
	Visitor string `json:"visitor,omitempty"`
	// IP 只发给站点侧做限流，不进邮件正文。
	IP string `json:"ip,omitempty"`
}

// payload 是 /api/session 的请求体。
//
// Entry 与 Entries 二选一：逐条上报只带 Entry，收尾请求带 Entries（权威全量）。
type payload struct {
	SessionID string     `json:"sessionId"`
	StartedAt time.Time  `json:"startedAt"`
	ClosedAt  *time.Time `json:"closedAt,omitempty"`
	Closed    bool       `json:"closed"`
	Entry     *Entry     `json:"entry,omitempty"`
	Entries   []Entry    `json:"entries,omitempty"`
	Meta      Meta       `json:"meta"`
}

// Reporter 负责一个会话的记录与上报。
type Reporter struct {
	endpoint  string
	userAgent string
	http      *http.Client
	id        string
	startedAt time.Time
	meta      Meta

	mu       sync.Mutex
	entries  []Entry
	finished bool
}

// New 用站点根地址构造上报器（例如 https://example.com，会自动补 /api/session）。
// 传空串返回 ErrDisabled——调用方拿到 nil 上报器即可，所有方法都是空操作。
func New(baseURL string, meta Meta) (*Reporter, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return nil, ErrDisabled
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("上报地址无效：%w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("上报地址无效：%q", base)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/session"
	parsed.RawQuery, parsed.Fragment = "", ""

	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	// 传输层用默认的（http.ProxyFromEnvironment 自带回环排除，本地联调不会被代理拐走）。
	return &Reporter{
		endpoint:  parsed.String(),
		userAgent: userAgentFor(meta.Version),
		id:        id,
		startedAt: time.Now(),
		meta:      meta,
		http:      &http.Client{},
	}, nil
}

// Enabled 上报器是否真的会发请求。
func (r *Reporter) Enabled() bool {
	return r != nil && r.endpoint != ""
}

// SessionID 返回本次会话的随机 id（站点侧按它把逐条上报攒成一封邮件）。
func (r *Reporter) SessionID() string {
	if r == nil {
		return ""
	}
	return r.id
}

// Entries 返回已记录的条目（副本）。主要给单测与排查用。
func (r *Reporter) Entries() []Entry {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Entry(nil), r.entries...)
}

// Record 记一条，并返回这条的上报任务。
//
// 返回值是 func() 而不是直接发请求：上报可能要等一个网络往返，
// 必须由调用方丢到独立 goroutine 里跑（app 侧见 reportCmd），绝不能卡在 Elm 循环里。
// 返回 nil 表示不用上报（上报已关闭，或文本是空的）。
func (r *Reporter) Record(kind, text string) func() {
	if !r.Enabled() {
		return nil
	}
	text = truncateRunes(strings.TrimSpace(text), maxTextRunes)
	if text == "" {
		return nil
	}
	entry := Entry{At: time.Now(), Kind: kind, Text: text}

	r.mu.Lock()
	if len(r.entries) < maxEntries {
		r.entries = append(r.entries, entry)
	}
	r.mu.Unlock()

	return func() {
		r.postWithTimeout(entryTimeout, payload{
			SessionID: r.id,
			StartedAt: r.startedAt,
			Entry:     &entry,
			Meta:      r.meta,
		})
	}
}

// Finish 发送收尾请求：带上完整记录（权威版本），站点侧收到就发那封汇总邮件。
//
// 它是**阻塞**的，且只会真正执行一次——第二次调用直接返回。
// 调用方必须传带超时的 ctx：会话都结束了，不该让访客（或 SSH 会话）为它多等。
//
// 空会话（一次提交都没有）不发任何请求：用户明确要求「空会话不发」，
// 与其让站点侧去判空，不如终端这边一个包都别发。
func (r *Reporter) Finish(ctx context.Context) {
	if !r.Enabled() {
		return
	}
	r.mu.Lock()
	if r.finished {
		r.mu.Unlock()
		return
	}
	r.finished = true
	if len(r.entries) == 0 {
		r.mu.Unlock()
		return
	}
	entries := append([]Entry(nil), r.entries...)
	r.mu.Unlock()

	now := time.Now()
	r.post(ctx, payload{
		SessionID: r.id,
		StartedAt: r.startedAt,
		ClosedAt:  &now,
		Closed:    true,
		Entries:   entries,
		Meta:      r.meta,
	})
}

// postWithTimeout 给一次性上报套上超时。
func (r *Reporter) postWithTimeout(timeout time.Duration, body payload) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	r.post(ctx, body)
}

// post 发一个请求并彻底忽略结果：成功也好、连不上也好，都一样安静。
//
// 这里刻意不做重试。重试会让「关终端」这件小事被拖成好几秒，
// 而丢一份会话记录的成本远低于让访客在退出时干等。
func (r *Reporter) post(ctx context.Context, body payload) {
	if !r.Enabled() {
		return
	}
	data, err := json.Marshal(body)
	if err != nil {
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(data))
	if err != nil {
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if r.userAgent != "" {
		request.Header.Set("User-Agent", r.userAgent)
	}

	response, err := r.http.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	// 把响应体读掉（有上限）再关，连接才能被复用。
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
}

func userAgentFor(version string) string {
	if version == "" {
		return "zhang-terminal-resume"
	}
	return "zhang-terminal-resume/" + version
}

// newSessionID 生成一个随机会话 id。
//
// 它只是把「同一会话的若干次上报」串起来的键，不要求不可预测到什么程度，
// 但仍用 crypto/rand：math/rand 在同一秒内启动的两个会话上可能撞号（进程启动时种子相近）。
func newSessionID() (string, error) {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成会话 id 失败：%w", err)
	}
	return hex.EncodeToString(buf), nil
}

// truncateRunes 按字符（不是字节）截断，避免把中文切一半。
func truncateRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit])
}
