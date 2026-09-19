package ai

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

// urlError 复刻 net/http 抛出的真实外壳：
// 所有传输层错误都会被包成 *url.Error，所以测试必须穿过这一层。
func urlError(inner error) error {
	return &url.Error{Op: "Post", URL: "https://zhang.design/api/chat", Err: inner}
}

// dialError 是「压根没连上」的形态：*net.OpError{Op:"dial"}。
func dialError(inner error) error {
	return urlError(&net.OpError{Op: "dial", Net: "tcp", Err: inner})
}

// TestDescribeTransportErrorClassifies 锁死分流结果。
//
// 这些分支的存在理由是一次真实故障：默认端点是个没注册的域名，
// 终端上只显示 `Post "https://…": EOF`——看不出是域名问题还是服务没起，
// 排查成本极高。每条分支都必须说清「哪里坏了」并给出出路。
func TestDescribeTransportErrorClassifies(t *testing.T) {
	const endpoint = "https://zhang.design/api/chat"

	cases := []struct {
		name string
		err  error
		// want 是必须出现的子串；notWant 是不该出现的（用来抓分流串台）。
		want    []string
		notWant []string
	}{
		{
			name: "域名查不到",
			err:  dialError(&net.DNSError{Err: "no such host", Name: "zhang.design", IsNotFound: true}),
			want: []string{"地址不存在", "zhang.design", "--api"},
			// DNS 失败包在 dial 里，分流写反就会说成「服务未启动」
			notWant: []string{"服务未启动"},
		},
		{
			name: "域名解析失败（非 NXDOMAIN）",
			err:  dialError(&net.DNSError{Err: "server misbehaving", Name: "zhang.design"}),
			want: []string{"解析失败", "zhang.design"},
		},
		{
			name: "DNS 超时该说超时，不说解析失败",
			err:  dialError(&net.DNSError{Err: "i/o timeout", Name: "zhang.design", IsTimeout: true}),
			want: []string{"超时"},
			// 超时判断必须排在 DNS 判断之前，否则会被说成解析失败
			notWant: []string{"解析失败"},
		},
		{
			name: "端口连不上",
			err:  dialError(errors.New("connect: connection refused")),
			want: []string{"连不上", "zhang.design"},
			// 端口拒连不能跟 DNS 失败混为一谈
			notWant: []string{"地址不存在"},
		},
		{
			name: "连上了但没响应就断（线上踩的正是这条）",
			// ⚠️ 真实形态是裸 io.EOF，不带 dial 壳——写成 dialError 就会走错分支，
			// 这条用例等于在替分流顺序把关。
			err:     urlError(io.EOF),
			want:    []string{"没返回内容", "zhang.design", "--api"},
			notWant: []string{"连不上", "地址不存在"},
		},
		{
			name: "中间层掐断（半路 EOF）",
			err:  urlError(io.ErrUnexpectedEOF),
			want: []string{"没返回内容"},
		},
		{
			name: "没见过的错误也别吞",
			err:  urlError(errors.New("something exotic")),
			want: []string{"连接 AI 服务失败", "something exotic", "zhang.design"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := describeTransportError(testCase.err, endpoint).Error()
			for _, want := range testCase.want {
				if !strings.Contains(got, want) {
					t.Errorf("错误文案 = %q, 应当包含 %q", got, want)
				}
			}
			for _, notWant := range testCase.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("错误文案 = %q, 不该包含 %q（分流串台）", got, notWant)
				}
			}
		})
	}
}

// TestDescribeTransportErrorKeepsHostOnly 只报主机名，不要把整条 URL 糊到屏幕上。
// 提示行只有一行、还要折行，URL 一长就把真正要紧的出错原因挤没了。
func TestDescribeTransportErrorKeepsHostOnly(t *testing.T) {
	got := describeTransportError(urlError(io.EOF), "https://zhang.design/api/chat").Error()
	if strings.Contains(got, "/api/chat") {
		t.Fatalf("错误文案 = %q, 不该带路径", got)
	}
	if !strings.Contains(got, "zhang.design") {
		t.Fatalf("错误文案 = %q, 应当带主机名", got)
	}
}

// TestStreamTransportErrorIsHumanReadable 走真实 http.Client：
// 打一个必然被拒的本地端口，确认错误穿过 Stream 之后仍是人话。
// 单测合成错误测不到 http 栈的真实包装，这条补上。
func TestStreamTransportErrorIsHumanReadable(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:1", "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	streamErr := client.Stream(ctx, []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if streamErr == nil {
		t.Fatal("指望连不上，结果成功了")
	}
	got := streamErr.Error()
	for _, want := range []string{"连不上", "127.0.0.1:1", "--api"} {
		if !strings.Contains(got, want) {
			t.Errorf("Stream 错误 = %q, 应当包含 %q", got, want)
		}
	}
}
