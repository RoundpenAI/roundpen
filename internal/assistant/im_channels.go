package assistant

import (
	"encoding/json"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

// ImChannels maps cc-connect platform type → channel config.
type ImChannels map[string]ImChannel

// ImChannel is one messaging platform binding for an assistant.
type ImChannel struct {
	Enabled bool `json:"enabled"`

	// Shared
	AllowFrom string `json:"allowFrom,omitempty"`

	// BotToken family (telegram, discord) / weixin
	Token string `json:"token,omitempty"`
	Proxy string `json:"proxy,omitempty"`

	// Telegram
	GroupReplyAll bool `json:"groupReplyAll,omitempty"`

	// AppPair (feishu)
	AppID     string `json:"appId,omitempty"`
	AppSecret string `json:"appSecret,omitempty"`

	// DingTalk
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`

	// Slack DualToken
	BotToken string `json:"botToken,omitempty"`
	AppToken string `json:"appToken,omitempty"`

	// WeCom WebSocket
	BotID     string `json:"botId,omitempty"`
	BotSecret string `json:"botSecret,omitempty"`

	// Weixin ilink
	BaseURL   string `json:"baseUrl,omitempty"`
	AccountID string `json:"accountId,omitempty"`
}

// secretFields lists JSON field names that must be masked on read.
var channelSecretFields = []string{
	"token", "appSecret", "clientSecret", "botToken", "appToken", "botSecret",
}

// SanitizeImChannels returns a copy with secrets masked for API responses.
func SanitizeImChannels(in ImChannels) ImChannels {
	if in == nil {
		return ImChannels{}
	}
	out := make(ImChannels, len(in))
	for k, ch := range in {
		c := ch
		c.Token = settings.MaskSecret(c.Token)
		c.AppSecret = settings.MaskSecret(c.AppSecret)
		c.ClientSecret = settings.MaskSecret(c.ClientSecret)
		c.BotToken = settings.MaskSecret(c.BotToken)
		c.AppToken = settings.MaskSecret(c.AppToken)
		c.BotSecret = settings.MaskSecret(c.BotSecret)
		out[k] = c
	}
	return out
}

// MergeImChannelsSecrets keeps previous secrets when the client sends mask/empty.
// A missing key in next means remove that channel. Nil next means no change.
func MergeImChannelsSecrets(next, prev ImChannels) ImChannels {
	if next == nil {
		if prev == nil {
			return ImChannels{}
		}
		return prev
	}
	out := make(ImChannels, len(next))
	for k, ch := range next {
		p := prev[k]
		ch.Token = settings.ResolveSecret(ch.Token, p.Token)
		ch.AppSecret = settings.ResolveSecret(ch.AppSecret, p.AppSecret)
		ch.ClientSecret = settings.ResolveSecret(ch.ClientSecret, p.ClientSecret)
		ch.BotToken = settings.ResolveSecret(ch.BotToken, p.BotToken)
		ch.AppToken = settings.ResolveSecret(ch.AppToken, p.AppToken)
		ch.BotSecret = settings.ResolveSecret(ch.BotSecret, p.BotSecret)
		out[k] = ch
	}
	return out
}

// ImChannelsJSON marshals channels for storage.
func ImChannelsJSON(c ImChannels) (json.RawMessage, error) {
	if c == nil {
		c = ImChannels{}
	}
	return json.Marshal(c)
}

// ParseImChannels unmarshals stored JSON.
func ParseImChannels(raw []byte) (ImChannels, error) {
	if len(raw) == 0 {
		return ImChannels{}, nil
	}
	var c ImChannels
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	if c == nil {
		c = ImChannels{}
	}
	return c, nil
}

// HasEnabledChannel reports whether any channel is enabled with credentials.
func (c ImChannels) HasEnabledChannel() bool {
	for typ, ch := range c {
		if ch.Enabled && ChannelReady(typ, ch) {
			return true
		}
	}
	return false
}

// ChannelReady reports whether typ has the minimum credentials to start.
func ChannelReady(typ string, ch ImChannel) bool {
	typ = strings.ToLower(strings.TrimSpace(typ))
	switch typ {
	case "telegram", "discord":
		return strings.TrimSpace(ch.Token) != ""
	case "feishu", "lark":
		return strings.TrimSpace(ch.AppID) != "" && strings.TrimSpace(ch.AppSecret) != ""
	case "dingtalk":
		return strings.TrimSpace(ch.ClientID) != "" && strings.TrimSpace(ch.ClientSecret) != ""
	case "slack":
		return strings.TrimSpace(ch.BotToken) != "" && strings.TrimSpace(ch.AppToken) != ""
	case "wecom":
		return strings.TrimSpace(ch.BotID) != "" && strings.TrimSpace(ch.BotSecret) != ""
	case "weixin":
		return strings.TrimSpace(ch.Token) != ""
	default:
		return false
	}
}

// SupportedChannelTypes are the platform types Roundpen ships UI for.
func SupportedChannelTypes() []string {
	return []string{"telegram", "discord", "slack", "feishu", "dingtalk", "wecom", "weixin"}
}

// Ensure channelSecretFields is referenced (docs / future generic mask).
var _ = channelSecretFields
