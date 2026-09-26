// Package imconnect bridges IM platform connectors (cc-connect: Telegram,
// Feishu, Weixin, …) to Roundpen agent sessions at assistant granularity.
//
// A Supervisor owns one Engine per assistant that has enabled IM channels.
// Each Engine maps chat turns onto agentsession rows stamped with that
// assistant_id; ACP permission prompts surface as chat buttons.
package imconnect
