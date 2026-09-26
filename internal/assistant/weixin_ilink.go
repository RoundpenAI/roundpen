package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultWeixinAPIURL = "https://ilinkai.weixin.qq.com"
	weixinBotType       = "3"
)

// WeixinQRBegin holds a fresh ilink QR challenge for the UI.
type WeixinQRBegin struct {
	QRKey string `json:"qrKey"`
	QRURL string `json:"qrUrl"`
}

// WeixinQRStatus is a poll result. Confirmed credentials stay server-side.
type WeixinQRStatus struct {
	Status      string
	BotToken    string
	IlinkBotID  string
	BaseURL     string
	IlinkUserID string
}

// WeixinBeginQR fetches a login QR from ilink.
func WeixinBeginQR(ctx context.Context, apiBase string) (*WeixinQRBegin, error) {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if base == "" {
		base = defaultWeixinAPIURL
	}
	u, err := url.Parse(base + "/")
	if err != nil {
		return nil, fmt.Errorf("weixin: invalid api url: %w", err)
	}
	u = u.JoinPath("ilink", "bot", "get_bot_qrcode")
	q := u.Query()
	q.Set("bot_type", weixinBotType)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("weixin get_bot_qrcode: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("weixin get_bot_qrcode: http %d", resp.StatusCode)
	}
	var raw struct {
		QRCode           string `json:"qrcode"`
		QRCodeImgContent string `json:"qrcode_img_content"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("weixin decode: %w", err)
	}
	qrURL := strings.TrimSpace(raw.QRCodeImgContent)
	if qrURL == "" || strings.TrimSpace(raw.QRCode) == "" {
		return nil, fmt.Errorf("weixin: empty qrcode response")
	}
	return &WeixinQRBegin{QRKey: strings.TrimSpace(raw.QRCode), QRURL: qrURL}, nil
}

// WeixinPollQR checks scan/login status for qrKey.
func WeixinPollQR(ctx context.Context, apiBase, qrKey string) (*WeixinQRStatus, error) {
	qrKey = strings.TrimSpace(qrKey)
	if qrKey == "" {
		return nil, fmt.Errorf("weixin: qr_key required")
	}
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if base == "" {
		base = defaultWeixinAPIURL
	}
	u, err := url.Parse(base + "/")
	if err != nil {
		return nil, fmt.Errorf("weixin: invalid api url: %w", err)
	}
	u = u.JoinPath("ilink", "bot", "get_qrcode_status")
	q := u.Query()
	q.Set("qrcode", qrKey)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("iLink-App-ClientVersion", "1")
	client := &http.Client{Timeout: 40 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return &WeixinQRStatus{Status: "wait"}, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("weixin poll: http %d", resp.StatusCode)
	}
	var raw struct {
		Status      string `json:"status"`
		BotToken    string `json:"bot_token"`
		IlinkBotID  string `json:"ilink_bot_id"`
		BaseURL     string `json:"baseurl"`
		IlinkUserID string `json:"ilink_user_id"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("weixin decode: %w", err)
	}
	st := strings.TrimSpace(raw.Status)
	if st == "" {
		st = "wait"
	}
	out := &WeixinQRStatus{Status: st}
	if st == "confirmed" {
		out.BotToken = strings.TrimSpace(raw.BotToken)
		out.IlinkBotID = strings.TrimSpace(raw.IlinkBotID)
		out.BaseURL = strings.TrimSpace(raw.BaseURL)
		out.IlinkUserID = strings.TrimSpace(raw.IlinkUserID)
	}
	return out, nil
}
