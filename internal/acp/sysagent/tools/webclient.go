package tools

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// WebClientOptions 控制 WebFetch / WebSearch 出站 client 的防护行为。
type WebClientOptions struct {
	// AllowLoopback 仅供测试使用（httptest 监听 127.0.0.1）；生产恒为 false。
	AllowLoopback bool
}

// NewWebHTTPClient 返回带 SSRF 防护的 HTTP client：在 DNS 解析之后、建立连接之前
// 校验实际拨号地址，拒绝环回、link-local（含云元数据 169.254.169.254）、
// unspecified 与 multicast；内网网段与公网放行。
func NewWebHTTPClient(opts WebClientOptions) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	dialer.Control = func(network, address string, _ syscall.RawConn) error {
		return blockedDialAddr(address, opts.AllowLoopback)
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			MaxIdleConns:          8,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
}

// blockedDialAddr 校验拨号地址（形如 "1.2.3.4:443" 或 "[::1]:443"）。
func blockedDialAddr(address string, allowLoopback bool) error {
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
	return nil
}
