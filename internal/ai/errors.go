package ai

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
)

// apiHint 统一附在网络类错误末尾。
//
// 刻意只留「换后端」这一条出路：提示行只有一行、还和输入框（74 列）同宽竞争，
// 字一多就会被挤到折行。「--api off 关闭自由问答」写在 README 的启动参数表里
// ——那是多行场合，不跟这里抢宽度。
const apiHint = "（--api 可换后端）"

// describeTransportError 把 http.Client 的传输层错误翻译成一句能直接照着做的话。
//
// 起因是一次真实踩坑：默认端点写成了一个不存在的域名（NXDOMAIN），
// 终端上只打出一行
//
//	连接 AI 服务失败：Post "https://…/api/chat": EOF
//
// 这个 EOF 既没说清是 DNS 挂了、TLS 挂了还是服务没起，也没说该改哪里，
// 排查全靠猜。这里按错误类型分流，并在结尾统一给出路。
//
// 顺序有讲究：
//  1. 超时最先判——*net.DNSError 自己也算 net.Error，DNS 超时应当说「超时」而不是
//     「域名解析失败」；而查不到的域名（IsNotFound）Timeout() 为 false，不会被这条吞掉。
//  2. DNS 要排在 dial 之前——DNS 失败会被整个包进 *net.OpError{Op:"dial"} 里，
//     先判 dial 的话会把它说成「服务未启动」，指错方向。
func describeTransportError(err error, endpoint string) error {
	host := endpoint
	if parsed, parseErr := url.Parse(endpoint); parseErr == nil && parsed.Host != "" {
		host = parsed.Host
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("连接 AI 服务超时：%s%s", host, apiHint)
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return fmt.Errorf("AI 服务地址不存在：%s%s", host, apiHint)
		}
		return fmt.Errorf("AI 服务地址解析失败：%s%s", host, apiHint)
	}

	var recordErr tls.RecordHeaderError
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &recordErr) || errors.As(err, &certErr) {
		return fmt.Errorf("与 AI 服务的安全连接失败：%s%s", host, apiHint)
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return fmt.Errorf("连不上 AI 服务：%s%s", host, apiHint)
	}

	// 拿到了连接、却没等到任何响应就被挂断——最常见的是中间层（代理 / 网关）
	// 掐了连接，或者端口上蹲着的根本不是这个服务。
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("AI 服务没返回内容就断开：%s%s", host, apiHint)
	}

	return fmt.Errorf("连接 AI 服务失败：%s（%v）%s", host, err, apiHint)
}
