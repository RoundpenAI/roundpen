package imconnect

import (
	"fmt"
	"strings"

	"github.com/chenhg5/cc-connect/core"

	"github.com/RoundpenAI/roundpen/internal/assistant"

	_ "github.com/chenhg5/cc-connect/platform/dingtalk"
	_ "github.com/chenhg5/cc-connect/platform/discord"
	_ "github.com/chenhg5/cc-connect/platform/feishu"
	_ "github.com/chenhg5/cc-connect/platform/slack"
	_ "github.com/chenhg5/cc-connect/platform/telegram"
	_ "github.com/chenhg5/cc-connect/platform/wecom"
	_ "github.com/chenhg5/cc-connect/platform/weixin"
)

// PlatformsFromChannels builds cc-connect platforms for enabled, ready channels.
func PlatformsFromChannels(channels assistant.ImChannels) ([]core.Platform, error) {
	if channels == nil {
		return nil, nil
	}
	var out []core.Platform
	for _, typ := range assistant.SupportedChannelTypes() {
		ch, ok := channels[typ]
		if !ok || !ch.Enabled || !assistant.ChannelReady(typ, ch) {
			continue
		}
		opts, err := channelOpts(typ, ch)
		if err != nil {
			return nil, err
		}
		p, err := core.CreatePlatform(platformTypeName(typ), opts)
		if err != nil {
			return nil, fmt.Errorf("imconnect: create %s: %w", typ, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func platformTypeName(typ string) string {
	switch strings.ToLower(typ) {
	case "lark":
		return "lark"
	default:
		return strings.ToLower(typ)
	}
}

func channelOpts(typ string, ch assistant.ImChannel) (map[string]any, error) {
	opts := map[string]any{}
	if af := strings.TrimSpace(ch.AllowFrom); af != "" {
		opts["allow_from"] = af
	}
	switch strings.ToLower(typ) {
	case "telegram":
		opts["token"] = strings.TrimSpace(ch.Token)
		if ch.Proxy != "" {
			opts["proxy"] = ch.Proxy
		}
		opts["group_reply_all"] = ch.GroupReplyAll
	case "discord":
		opts["token"] = strings.TrimSpace(ch.Token)
	case "feishu", "lark":
		opts["app_id"] = strings.TrimSpace(ch.AppID)
		opts["app_secret"] = strings.TrimSpace(ch.AppSecret)
	case "dingtalk":
		opts["client_id"] = strings.TrimSpace(ch.ClientID)
		opts["client_secret"] = strings.TrimSpace(ch.ClientSecret)
	case "slack":
		opts["bot_token"] = strings.TrimSpace(ch.BotToken)
		opts["app_token"] = strings.TrimSpace(ch.AppToken)
	case "wecom":
		opts["mode"] = "websocket"
		opts["bot_id"] = strings.TrimSpace(ch.BotID)
		opts["bot_secret"] = strings.TrimSpace(ch.BotSecret)
	case "weixin":
		opts["token"] = strings.TrimSpace(ch.Token)
		if ch.BaseURL != "" {
			opts["base_url"] = strings.TrimSpace(ch.BaseURL)
		}
		if ch.AccountID != "" {
			opts["account_id"] = strings.TrimSpace(ch.AccountID)
		}
	default:
		return nil, fmt.Errorf("unsupported channel type %q", typ)
	}
	return opts, nil
}
