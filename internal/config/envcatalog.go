package config

import (
	"fmt"
	"strconv"
	"time"
)

// EnvVar is one process env knob shown on the admin Environment page.
type EnvVar struct {
	Name    string `json:"name"`
	Group   string `json:"group"` // core | sandbox | browser | llm | preview | push | advanced
	Default string `json:"default"`
	Value   string `json:"value"`
	Secret  bool   `json:"secret"`
	Hint    string `json:"hint"`   // English
	HintZh  string `json:"hintZh"` // 中文
}

const maskedSecret = "••••••••"

type envSpec struct {
	Name, Group, Default, Hint, HintZh string
	Secret                             bool
	value                              func(*Config) string
}

// EnvCatalog returns the documented env vars with defaults and the values
// currently loaded into cfg (secrets masked). Edit .env (or the real process
// environment) and restart the daemon to change them.
func EnvCatalog(cfg *Config) []EnvVar {
	if cfg == nil {
		cfg = &Config{}
	}
	specs := []envSpec{
		{
			Name: "DATABASE_URL", Group: "core", Secret: true,
			Hint:   "PostgreSQL DSN. Required.",
			HintZh: "PostgreSQL 连接串，必填。",
			value:  func(c *Config) string { return maskNonEmpty(c.DatabaseURL) },
		},
		{
			Name: "ROUNDPEN_HTTP_ADDR", Group: "core", Default: ":9527",
			Hint:   "Listen address for the control plane HTTP API.",
			HintZh: "控制面 HTTP 监听地址。",
			value:  func(c *Config) string { return c.HTTPAddr },
		},
		{
			Name: "ROUNDPEN_DATA_ROOT", Group: "core", Default: "./data",
			Hint:   "Data directory (secret.key, internal keys, workspace roots).",
			HintZh: "数据目录（secret.key、内部密钥、工作区等）。",
			value:  func(c *Config) string { return c.EffectiveDataRoot() },
		},
		{
			Name: "ROUNDPEN_LOG_LEVEL", Group: "core", Default: "info",
			Hint:   "slog level: debug, info, warn, error.",
			HintZh: "日志级别：debug / info / warn / error。",
			value:  func(c *Config) string { return c.LogLevel.String() },
		},
		{
			Name: "ROUNDPEN_BACKEND", Group: "core", Default: "docker",
			Hint:   "Sandbox backend. Use docker (qemu is accepted as a legacy alias).",
			HintZh: "沙箱后端，使用 docker（qemu 为历史别名）。",
			value:  func(c *Config) string { return c.Backend },
		},
		{
			Name: "DOCKER_HOST", Group: "core", Default: "unix:///var/run/docker.sock",
			Hint:   "Docker Engine endpoint for Agent (and managed Browser) containers.",
			HintZh: "Docker Engine 地址，用于 Agent / 托管 Browser 容器。",
			value:  func(c *Config) string { return c.DockerHost },
		},
		{
			Name: "ROUNDPEN_DOCKER_RUNTIME", Group: "core",
			Hint:   "Optional Docker runtime (e.g. runc, kata). Empty = daemon default.",
			HintZh: "可选 Docker runtime（如 runc、kata）；空 = 守护进程默认。",
			value:  func(c *Config) string { return c.DockerRuntime },
		},
		{
			Name: "ROUNDPEN_SECRET_KEY", Group: "core", Secret: true,
			Hint:   "32-byte master key (hex/base64) sealing DB secrets. Empty = auto file under data root.",
			HintZh: "密封数据库密钥的主密钥（32 字节 hex/base64）；空 = 数据目录下自动生成文件。",
			value:  func(c *Config) string { return maskNonEmpty(c.SecretKey) },
		},
		{
			Name: "ROUNDPEN_ALLOW_KEY_MISMATCH", Group: "core", Default: "false",
			Hint:   "Allow boot when the master key cannot open sealed secrets (recovery only).",
			HintZh: "主密钥无法解密已密封密钥时仍允许启动（仅用于恢复）。",
			value:  func(c *Config) string { return boolStr(c.AllowKeyMismatch) },
		},
		{
			Name: "ROUNDPEN_BOOTSTRAP_ADMIN", Group: "core", Default: "true",
			Hint:   "Create the bootstrap admin account when missing.",
			HintZh: "缺少管理员时自动创建引导账号。",
			value:  func(c *Config) string { return boolStr(c.BootstrapAdmin) },
		},
		{
			Name: "ROUNDPEN_BOOTSTRAP_ADMIN_PASSWORD", Group: "core", Secret: true,
			Hint:   "Initial admin password when the account has none.",
			HintZh: "管理员尚无密码时的初始密码。",
			value:  func(c *Config) string { return maskNonEmpty(c.BootstrapAdminPassword) },
		},
		{
			Name: "ROUNDPEN_BOOTSTRAP_ADMIN_PIN_PASSWORD", Group: "core", Default: "false",
			Hint:   "When true with a bootstrap password, restore that password on every boot (local dev).",
			HintZh: "与引导密码一起开启时，每次启动重置为该密码（本地开发用）。",
			value:  func(c *Config) string { return boolStr(c.BootstrapAdminPinPassword) },
		},
		{
			Name: "ROUNDPEN_API_KEY", Group: "core", Secret: true,
			Hint:   "Seeds the bootstrap admin API key. Empty = generate one.",
			HintZh: "引导管理员 API Key；空 = 自动生成。",
			value:  func(c *Config) string { return maskNonEmpty(c.APIKey) },
		},
		{
			Name: "ROUNDPEN_ALLOW_PUBLIC_REGISTRATION", Group: "core", Default: "false",
			Hint:   "When true, anyone can create an account (password sign-up and OAuth).",
			HintZh: "为 true 时允许公开自助注册（密码注册与 OAuth）。",
			value:  func(c *Config) string { return boolStr(c.AllowPublicRegistration) },
		},
		{
			Name: "ROUNDPEN_IM_ENABLED", Group: "core", Default: "false",
			Hint:   "Start IM channel engines. Keep off on secondary instances sharing the same chat account.",
			HintZh: "是否启动 IM 通道。共用同一聊天账号的副实例请保持关闭。",
			value:  func(c *Config) string { return boolStr(c.IMEnabled) },
		},
		{
			Name: "ROUNDPEN_OAUTH_RELAY", Group: "core", Default: "https://auth.eaxi.com",
			Hint:   "Official OAuth relay origin. Set off/none to disable.",
			HintZh: "官方 OAuth 中继地址；设为 off/none 可关闭。",
			value:  func(c *Config) string { return c.OAuthRelay },
		},

		{
			Name: "ROUNDPEN_DEFAULT_IMAGE", Group: "sandbox", Default: defaultAgentImage,
			Hint:   "Default OCI image / template reference for new sandboxes.",
			HintZh: "新建沙箱的默认镜像 / 模板引用。",
			value:  func(c *Config) string { return c.DefaultImage },
		},
		{
			Name: "ROUNDPEN_AGENT_IMAGE", Group: "sandbox", Default: defaultAgentImage,
			Hint:   "OCI image used to start Agent containers.",
			HintZh: "启动 Agent 容器使用的 OCI 镜像。",
			value:  func(c *Config) string { return c.AgentImage },
		},
		{
			Name: "ROUNDPEN_DEFAULT_TTL", Group: "sandbox", Default: "1800",
			Hint:   "Default sandbox TTL in seconds (idle reclaim).",
			HintZh: "默认沙箱 TTL（秒），空闲后回收。",
			value:  func(c *Config) string { return durationSecs(c.DefaultTTL) },
		},
		{
			Name: "ROUNDPEN_DEFAULT_AGENT_TEMPLATE", Group: "sandbox", Default: "code-agent",
			Hint:   "Template id for the Agent slot.",
			HintZh: "Agent 槽位使用的模板 id。",
			value:  func(c *Config) string { return c.DefaultAgentTemplate },
		},
		{
			Name: "ROUNDPEN_DEFAULT_BROWSER_TEMPLATE", Group: "sandbox", Default: "browser",
			Hint:   "Template id for the Browser slot.",
			HintZh: "Browser 槽位使用的模板 id。",
			value:  func(c *Config) string { return c.DefaultBrowserTemplate },
		},
		{
			Name: "ROUNDPEN_BROWSER_IMAGE", Group: "sandbox", Default: "ghcr.io/browserless/chrome:v2.56.7",
			Hint:   "OCI image for the managed Browser container.",
			HintZh: "托管 Browser 容器的 OCI 镜像。",
			value:  func(c *Config) string { return c.BrowserImage },
		},
		{
			Name: "ROUNDPEN_IMAGE_MIRRORS", Group: "sandbox",
			Hint:   "Comma-separated pull mirror templates, or none/off.",
			HintZh: "镜像拉取加速模板（逗号分隔），或 none/off。",
			value:  func(c *Config) string { return c.ImageMirrors },
		},
		{
			Name: "ROUNDPEN_IMAGE_MIRROR_RACE_DELAY", Group: "sandbox", Default: "15",
			Hint:   "Seconds before racing mirror pulls; 0 disables the race.",
			HintZh: "镜像竞速拉取的等待秒数；0 关闭竞速。",
			value:  func(c *Config) string { return mirrorRaceDefault(c) },
		},
		{
			Name: "ROUNDPEN_QEMU_ENABLED", Group: "sandbox", Default: "true",
			Hint:   "Allow QEMU for reserved desktop/mobile slots.",
			HintZh: "是否允许为桌面/手机预留槽位使用 QEMU。",
			value:  func(c *Config) string { return boolStr(c.QEMUEnabled) },
		},
		{
			Name: "ROUNDPEN_TRUSTED_PROXIES", Group: "sandbox",
			Hint:   "Comma-separated CIDRs trusted to send X-Forwarded-* headers.",
			HintZh: "信任的反向代理 CIDR（逗号分隔），可转发 X-Forwarded-*。",
			value:  func(c *Config) string { return c.TrustedProxies },
		},

		{
			Name: "ROUNDPEN_CDP_PROVIDER", Group: "browser", Default: "auto",
			Hint:   "Browser CDP source: auto | docker | host | remote | cloud. Prefer items under Browser settings.",
			HintZh: "浏览器 CDP 来源：auto / docker / host / remote / cloud。一般在「浏览器」里配条目。",
			value:  func(c *Config) string { return c.CDP.Provider },
		},
		{
			Name: "ROUNDPEN_CDP_PORT", Group: "browser", Default: strconv.Itoa(DefaultCDPPort),
			Hint:   "CDP port inside the managed Browser container.",
			HintZh: "托管 Browser 容器内的 CDP 端口。",
			value:  func(c *Config) string { return strconv.Itoa(c.CDP.Port) },
		},
		{
			Name: "ROUNDPEN_CDP_ENDPOINT", Group: "browser",
			Hint:   "External CDP URL for remote/cloud (or host override).",
			HintZh: "remote/cloud（或 host 覆盖）的外部 CDP 地址。",
			value:  func(c *Config) string { return c.CDP.Endpoint },
		},
		{
			Name: "ROUNDPEN_CDP_TOKEN", Group: "browser", Secret: true,
			Hint:   "Optional bearer/query token for remote/cloud CDP.",
			HintZh: "remote/cloud CDP 的可选令牌。",
			value:  func(c *Config) string { return maskNonEmpty(c.CDP.Token) },
		},

		{
			Name: "ROUNDPEN_LLMGW_PUBLIC_URL", Group: "llm",
			Hint:   "URL Agents inside Docker can reach for the model relay.",
			HintZh: "Docker 内 Agent 访问模型中继用的控制面 URL。",
			value:  func(c *Config) string { return c.LLMGW.PublicURL },
		},
		{
			Name: "ROUNDPEN_LLMGW_LOG_BODY_MAX_BYTES", Group: "llm", Default: "0",
			Hint:   "Max bytes of each relayed request/response body to store for audit. 0 = metadata only.",
			HintZh: "中继请求/响应正文审计保留的最大字节数；0 = 只记元数据。",
			value:  func(c *Config) string { return strconv.Itoa(c.LLMGW.LogBodyMaxBytes) },
		},
		{
			Name: "ROUNDPEN_LLMGW_EMBEDDING_MODEL", Group: "llm", Default: "text-embedding-3-small",
			Hint:   "Upstream embedding model for the roundpen-embed alias.",
			HintZh: "roundpen-embed 别名对应的上游 Embedding 模型。",
			value:  func(c *Config) string { return c.LLMGW.EmbeddingModel },
		},
		{
			Name: "ROUNDPEN_LLMGW_DEFAULT_MODEL", Group: "llm",
			Hint:   "Fallback model when the request model is not in the upstream map.",
			HintZh: "请求模型不在上游映射中时的兜底模型。",
			value:  func(c *Config) string { return c.LLMGW.DefaultModel },
		},
		{
			Name: "ROUNDPEN_WEB_SEARCH_ENDPOINT", Group: "llm",
			Hint:   "Seeds a Tavily-compatible search backend on first boot. Prefer Network search settings.",
			HintZh: "首次启动时播种 Tavily 兼容搜索；日常请在「网络搜索」中配置。",
			value:  func(c *Config) string { return c.WebTools.SearchEndpoint },
		},
		{
			Name: "ROUNDPEN_WEB_SEARCH_API_KEY", Group: "llm", Secret: true,
			Hint:   "Seeds the search API key on first boot.",
			HintZh: "首次启动时播种搜索 API Key。",
			value:  func(c *Config) string { return maskNonEmpty(c.WebTools.SearchAPIKey) },
		},

		{
			Name: "ROUNDPEN_PREVIEW_PUBLIC_URL", Group: "preview",
			Hint:   "Absolute base URL for path-based preview links.",
			HintZh: "路径型预览链接的绝对根地址。",
			value:  func(c *Config) string { return c.PreviewPublicURL },
		},
		{
			Name: "ROUNDPEN_PREVIEW_DOMAIN", Group: "preview",
			Hint:   "Host suffix for per-port preview subdomains (optional https:// scheme prefix).",
			HintZh: "按端口预览的子域后缀（可带 https:// 前缀）。",
			value:  func(c *Config) string { return previewDomainValue(c) },
		},
		{
			Name: "ROUNDPEN_PREVIEW_TOKEN_TTL", Group: "preview", Default: "900",
			Hint:   "Preview token lifetime in seconds.",
			HintZh: "预览令牌有效期（秒）。",
			value:  func(c *Config) string { return durationSecs(c.PreviewTokenTTL) },
		},

		{
			Name: "ROUNDPEN_VAPID_PUBLIC_KEY", Group: "push",
			Hint:   "Web Push VAPID public key. Empty = auto-generate under data root.",
			HintZh: "Web Push VAPID 公钥；空 = 在数据目录自动生成。",
			value:  func(c *Config) string { return c.VAPIDPublicKey },
		},
		{
			Name: "ROUNDPEN_VAPID_PRIVATE_KEY", Group: "push", Secret: true,
			Hint:   "Web Push VAPID private key.",
			HintZh: "Web Push VAPID 私钥。",
			value:  func(c *Config) string { return maskNonEmpty(c.VAPIDPrivateKey) },
		},
		{
			Name: "ROUNDPEN_VAPID_SUBJECT", Group: "push",
			Hint:   "VAPID contact (mailto: or https:).",
			HintZh: "VAPID 联系方式（mailto: 或 https:）。",
			value:  func(c *Config) string { return c.VAPIDSubject },
		},

		{
			Name: "ROUNDPEN_FNOS_GATEWAY_SOCKET", Group: "advanced",
			Hint:   "fnOS unified-gateway Unix socket. Empty = gateway SSO off.",
			HintZh: "飞牛统一网关 Unix socket；空 = 关闭网关 SSO。",
			value:  func(c *Config) string { return c.FnosGatewaySocket },
		},
		{
			Name: "ROUNDPEN_FNOS_GATEWAY_PREFIX", Group: "advanced", Default: "/app/roundpen",
			Hint:   "Path prefix the fnOS web console mounts at.",
			HintZh: "飞牛控制台挂载的路径前缀。",
			value:  func(c *Config) string { return c.FnosGatewayPrefix },
		},
	}

	out := make([]EnvVar, 0, len(specs))
	for _, s := range specs {
		out = append(out, EnvVar{
			Name:    s.Name,
			Group:   s.Group,
			Default: s.Default,
			Value:   s.value(cfg),
			Secret:  s.Secret,
			Hint:    s.Hint,
			HintZh:  s.HintZh,
		})
	}
	return out
}

func maskNonEmpty(v string) string {
	if v == "" {
		return ""
	}
	return maskedSecret
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func durationSecs(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	return strconv.FormatInt(int64(d/time.Second), 10)
}

func mirrorRaceDefault(cfg *Config) string {
	if cfg.ImageMirrorRaceDelay == nil {
		return "15"
	}
	return durationSecs(*cfg.ImageMirrorRaceDelay)
}

func previewDomainValue(cfg *Config) string {
	if cfg.PreviewDomain == "" {
		return ""
	}
	if cfg.PreviewDomainScheme != "" {
		return fmt.Sprintf("%s://%s", cfg.PreviewDomainScheme, cfg.PreviewDomain)
	}
	return cfg.PreviewDomain
}
