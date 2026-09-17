package assistant

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

func TestMergeImChannelsSecrets(t *testing.T) {
	prev := ImChannels{
		"telegram": {Enabled: true, Token: "tok-secret"},
	}
	next := ImChannels{
		"telegram": {Enabled: true, Token: settings.SecretMask, AllowFrom: "1"},
	}
	got := MergeImChannelsSecrets(next, prev)
	if got["telegram"].Token != "tok-secret" {
		t.Fatalf("token = %q, want kept", got["telegram"].Token)
	}
	if got["telegram"].AllowFrom != "1" {
		t.Fatalf("allowFrom = %q", got["telegram"].AllowFrom)
	}
}

func TestChannelReady(t *testing.T) {
	if !ChannelReady("telegram", ImChannel{Token: "t"}) {
		t.Fatal("telegram ready")
	}
	if ChannelReady("feishu", ImChannel{AppID: "a"}) {
		t.Fatal("feishu incomplete")
	}
	if !ChannelReady("wecom", ImChannel{BotID: "b", BotSecret: "s"}) {
		t.Fatal("wecom ready")
	}
}

func TestSanitizeImChannels(t *testing.T) {
	in := ImChannels{"slack": {BotToken: "xoxb-1", AppToken: "xapp-1", Enabled: true}}
	out := SanitizeImChannels(in)
	if out["slack"].BotToken != settings.SecretMask {
		t.Fatalf("botToken not masked: %q", out["slack"].BotToken)
	}
	if in["slack"].BotToken != "xoxb-1" {
		t.Fatal("sanitize mutated input")
	}
}
