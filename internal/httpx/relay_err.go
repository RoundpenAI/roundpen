package httpx

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatRelayError turns an OAuth relay HTTP failure into a user-facing message.
// Known relay bodies get a concrete explanation; unknown ones still include the
// status and any error text so maintainers can diagnose.
func FormatRelayError(path string, status int, raw []byte) string {
	detail := relayErrDetail(raw)
	if mapped := mapRelayDetail(detail); mapped != "" {
		return mapped
	}
	if detail != "" {
		return fmt.Sprintf("OAuth 中转 %s 失败（HTTP %d）：%s", path, status, detail)
	}
	return fmt.Sprintf("OAuth 中转 %s 失败（HTTP %d）", path, status)
}

// RelayErrSuffix is kept for callers that only need the trailing detail.
func RelayErrSuffix(raw []byte) string {
	if detail := relayErrDetail(raw); detail != "" {
		return ": " + detail
	}
	return ""
}

func relayErrDetail(raw []byte) string {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return ""
	}
	var envelope struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) == nil {
		if msg := strings.TrimSpace(envelope.Error); msg != "" {
			return msg
		}
	}
	msg := string(raw)
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

func mapRelayDetail(detail string) string {
	d := strings.ToLower(strings.TrimSpace(detail))
	switch {
	case d == "":
		return ""
	case strings.Contains(d, "http return is only allowed"):
		return "连接失败：回跳地址不能用公网 http，请用 https，或局域网 / localhost 地址。"
	case strings.Contains(d, "return must be an absolute url"):
		return "连接失败：缺少有效的回跳地址（需要绝对 URL）。"
	case strings.Contains(d, "return url must not include credentials"):
		return "连接失败：回跳地址不能包含用户名或密码。"
	case strings.Contains(d, "worker secrets") && strings.Contains(d, "not set"):
		return "连接失败：官方 OAuth 中转未配置该供应商的密钥，请联系维护者。"
	case strings.Contains(d, "authorization bearer") && strings.Contains(d, "x-roundpen-install-pub"):
		return "连接失败：本机安装身份凭证缺失，请检查 install-identity 是否完整。"
	case strings.Contains(d, "state is required"):
		return "连接失败：OAuth state 无效。"
	case strings.Contains(d, "could not register install identity"):
		return "连接失败：无法在官方 OAuth 中转登记本机安装身份，请稍后重试。"
	default:
		return ""
	}
}
