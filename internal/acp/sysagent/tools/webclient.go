package tools

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// WebClientOptions 控制 WebFetch / WebSearch / Skill install 出站 client 的防护行为。
type WebClientOptions struct {
	// AllowLoopback 仅供测试使用（httptest 监听 127.0.0.1）；生产恒为 false。
	AllowLoopback bool
	// AllowPrivate 允许访问 RFC1918/ULA 私网地址。默认拦截：skill install 可从
	// URL 拉取内容并注入对话，私网不应是模型可读的数据源。确需抓取内网的
	// 自建部署可显式打开。
	AllowPrivate bool
}

// NewWebHTTPClient 返回带 SSRF 防护的 HTTP client：在 DNS 解析之后、建立连接之前
// 校验实际拨号地址，拒绝环回、link-local（含云元数据 169.254.169.254）、
// unspecified、multicast 与私网网段（RFC1918/ULA，AllowPrivate 可放行）。
func NewWebHTTPClient(opts WebClientOptions) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	dialer.Control = func(network, address string, _ syscall.RawConn) error {
		return blockedDialAddr(address, opts.AllowLoopback, opts.AllowPrivate)
	}
	return &http.Client{
		Transport: &http.Transport{
			ForceAttemptHTTP2:     true,
			DialContext:           dialer.DialContext,
			MaxIdleConns:          8,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
}

// blockedDialAddr 校验拨号地址（形如 "1.2.3.4:443" 或 "[::1]:443"）。
func blockedDialAddr(address string, allowLoopback, allowPrivate bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = strings.Trim(address, "[]")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("address %q is not allowed", address)
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("address %s is not allowed", ip)
	}
	if ip.IsLoopback() && !allowLoopback {
		return fmt.Errorf("address %s is not allowed", ip)
	}
	if ip.IsPrivate() && !allowPrivate {
		return fmt.Errorf("address %s is not allowed", ip)
	}
	return nil
}
