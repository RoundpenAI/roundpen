/** English UI copy (settings + app shell). Source for MessageKey. */
export const en = {
  'nav.assistants': 'Assistants',
  'nav.settings': 'Settings',
  'nav.registry': 'Images',
  'nav.menu': 'Menu',
  'nav.expandPrimary': 'Expand primary menu',
  'nav.collapsePrimary': 'Collapse primary menu',
  'nav.openMenu': 'Open menu',
  'nav.signOut': 'Sign out',
  'nav.signOutUser': 'Sign out ({user})',

  'settings.title': 'Settings',
  'settings.section.runtime': 'Agent runtime',
  'settings.section.git': 'Git personal tokens',
  'settings.section.general': 'General',
  'settings.section.preview': 'Preview',
  'settings.section.builds': 'Builds',
  'settings.section.browser': 'Browser',
  'settings.section.llmgw': 'LLM gateway',
  'settings.section.system': 'System',

  'settings.loading': 'Loading…',
  'settings.loadingSystem': 'Loading system settings…',
  'settings.loadFailed': 'failed to load settings',
  'settings.saveSuccess': 'Settings saved.',
  'settings.saveFailed': 'save failed',
  'settings.discardTitle': 'Discard unsaved changes?',
  'settings.save': 'Save settings',
  'settings.reload': 'Reload',
  'settings.currentSuffix': '{value} (current)',

  'settings.general.allowRegistration': 'Allow public registration',
  'settings.general.defaultImage': 'Default template / image',
  'settings.general.defaultTtl': 'Default sandbox TTL',

  'settings.preview.publicUrl': 'Public preview base URL',
  'settings.preview.tokenTtl': 'Preview token TTL',

  'settings.builds.engine': 'Template build engine',
  'settings.builds.kanikoDest': 'Kaniko destination prefix',
  'settings.builds.kanikoExecutor': 'Kaniko executor binary',
  'settings.builds.kanikoMirrors': 'Kaniko registry mirrors',
  'settings.builds.kanikoInsecure': 'Kaniko insecure registry',
  'settings.builds.kanikoSkipTls': 'Kaniko skip TLS verify',
  'settings.builds.kanikoExtra': 'Kaniko extra args',

  'settings.builder.disabled': 'Disabled (no local builds)',
  'settings.builder.kaniko': 'Local Kaniko',
  'settings.builder.docker': 'Local Docker',
  'settings.builder.ci': 'Remote CI (build elsewhere)',
  'settings.builder.auto': 'Auto (detect from backend / Kaniko config)',

  'settings.cdp.auto':
    'Auto (Docker engine → Docker Chrome; else host Chrome if present)',
  'settings.cdp.docker': 'Docker Chrome (sandbox Dial to guest CDP)',
  'settings.cdp.host': 'Host Chrome / debugging port on this machine',
  'settings.cdp.remote': 'Remote CDP (Browserless or self-hosted)',
  'settings.cdp.cloud': 'Cloud browser (paste session CDP URL)',

  'settings.ttl.10m': '10 minutes',
  'settings.ttl.15m': '15 minutes',
  'settings.ttl.20m': '20 minutes',
  'settings.ttl.30m': '30 minutes',
  'settings.ttl.1h': '1 hour',
  'settings.ttl.2h': '2 hours',
  'settings.ttl.4h': '4 hours',
  'settings.ttl.5m': '5 minutes',
  'settings.duration.1h': '1 hour',
  'settings.duration.nh': '{n} hours',
  'settings.duration.1m': '1 minute',
  'settings.duration.nm': '{n} minutes',
  'settings.duration.s': '{n}s',

  'settings.logBody.off': 'Off (default)',
  'settings.logBody.legacy': 'Legacy default (off)',
  'settings.logBody.4k': '4 KiB',
  'settings.logBody.16k': '16 KiB',
  'settings.logBody.64k': '64 KiB',
  'settings.logBody.256k': '256 KiB',

  'settings.browser.cdpProvider': 'CDP provider',
  'settings.browser.hostCdpUrl':
    'Host CDP URL (optional; empty starts local Chrome)',
  'settings.browser.cdpEndpoint': 'CDP endpoint URL',
  'settings.browser.cdpToken': 'CDP token (optional)',
  'settings.browser.guestPort': 'Guest CDP port',
  'settings.browser.keepMasked': 'Leave masked to keep current',
  'settings.browser.hint':
    'Browser tools attach to a DevTools websocket. NAS and compose should use Docker Chrome or a remote/cloud CDP — do not install Chrome on the NAS OS. Host Chrome is for laptop debugging only.',

  'settings.llmgw.intro':
    'Roundpen relays model calls so Agents never hold your real OpenAI / Anthropic keys. Configure upstream credentials below; Agents and Chats only receive a virtual key that calls /llmgw on this control plane.',
  'settings.llmgw.enable':
    'Enable relay (required for Agent Chats & memory embeddings)',
  'settings.llmgw.upstreamTitle': '1 · Upstream providers',
  'settings.llmgw.upstreamHint':
    'Where Roundpen forwards requests. These API keys stay in the control-plane database — they are never injected into sandboxes.',
  'settings.llmgw.openaiBase': 'OpenAI-compatible base URL',
  'settings.llmgw.openaiBaseHint':
    'Official OpenAI, Azure OpenAI, or any OpenAI-compatible proxy.',
  'settings.llmgw.openaiKey': 'OpenAI-compatible API key',
  'settings.llmgw.keepSecret': 'Leave masked to keep the stored secret.',
  'settings.llmgw.anthropicBase': 'Anthropic base URL',
  'settings.llmgw.anthropicBaseHint':
    'Optional. Leave empty if you only use OpenAI-compatible models.',
  'settings.llmgw.anthropicKey': 'Anthropic API key',
  'settings.llmgw.agentsTitle': '2 · What Agents use',
  'settings.llmgw.agentsHint':
    'Sandboxes get OPENAI_BASE_URL / ANTHROPIC_BASE_URL pointing at this Roundpen, plus a virtual key as OPENAI_API_KEY.',
  'settings.llmgw.publicUrl': 'Control-plane public URL',
  'settings.llmgw.publicUrlHint':
    'URL Agents inside sandboxes can reach (e.g. http://host.docker.internal:9527 or your LAN IP). Not the upstream OpenAI URL.',
  'settings.llmgw.defaultModel': 'Default model',
  'settings.llmgw.defaultModelHint':
    'Used for both OpenAI and Anthropic relays when the request model is not in the upstream model map (and is not already an upstream target name). Leave empty to pass unknown models through.',
  'settings.llmgw.virtualKeys': 'Virtual keys',
  'settings.llmgw.virtualKeysHint':
    'Client credentials for /llmgw. Format: vk-name:label or vk-name (comma-separated). Example: vk-dev:dev,vk-prod:prod. Agents pick a non-internal key automatically.',
  'settings.llmgw.advanced': 'Advanced',
  'settings.llmgw.embedModel': 'Embedding model',
  'settings.llmgw.embedModelHint':
    'Upstream model aliased as roundpen-embed for long-term memory search.',
  'settings.llmgw.logBody': 'Request body logging',
  'settings.llmgw.logBodyHint':
    'How much of each relayed request/response to store for audit. Off = metadata only.',
  'settings.llmgw.secretsNote':
    'Secrets are stored in PostgreSQL and shown masked. Leave a masked field unchanged to keep the existing value. Saves apply immediately — no restart.',

  'settings.system.title': 'System (read-only)',
  'settings.system.backend': 'Backend',
  'settings.system.httpAddr': 'HTTP addr',
  'settings.system.dataRoot': 'Data root',
  'settings.system.dockerHost': 'Docker host',
  'settings.system.activeBuilder': 'Active builder',
  'settings.system.llmgw': 'LLM gateway',
  'settings.system.llmgwActive': 'active',
  'settings.system.llmgwMounted': 'mounted (disabled)',
  'settings.system.llmgwOff': 'not mounted',
  'settings.system.cdpProvider': 'CDP provider',
  'settings.system.hostChrome': 'Host Chrome',
  'settings.system.hostChromeFound': 'found',
  'settings.system.hostChromeMissing': 'not on PATH',
  'settings.system.unavailable': 'System info unavailable.',
  'settings.system.footer':
    'Database and listen address require environment variables and a process restart. The default Agent engine (`ROUNDPEN_BACKEND`) is only a fallback — users pick QEMU, Docker, or Kern in Agent runtime above. Template builds and LLM gateway settings apply at runtime.',
  'settings.system.disabled': 'disabled',

  'runtime.title': 'Agent runtime',
  'runtime.titleQemu': 'Browser / QEMU',
  'runtime.desc':
    'Pick how Cloud Agent runs on this machine. Browser desktops always use QEMU. If the host is missing binaries or images, follow the setup steps — no process restart is required after they are installed.',
  'runtime.descQemu':
    'This slot needs a QEMU VM image on the host. If anything is missing, install it here then retry.',
  'runtime.loading': 'Loading runtimes…',
  'runtime.loadFailed': 'failed to load runtime',
  'runtime.saveFailed': 'save failed',
  'runtime.startFailed': 'start failed',
  'runtime.start': 'Start / resume Agent',
  'runtime.retry': 'Retry after setup',
  'runtime.recheck': 'Recheck host',
  'runtime.ready': 'Ready',
  'runtime.needsSetup': 'Needs setup',
  'runtime.selected': 'Selected',
  'runtime.useThis': 'Use this',

  'git.title': 'Git personal tokens',
  'git.desc':
    'One personal token per git host. Roundpen stores it for your account and injects it into the Cloud Agent workspace for git, and later tea / gh / glab (issues, PRs). Tokens never go into the image, and we do not copy SSH keys from this machine.',
  'git.loadFailed': 'failed to load git credentials',
  'git.saveFailed': 'save failed',
  'git.deleteFailed': 'delete failed',
  'git.tokenSaved': 'personal token saved',
  'git.remove': 'Remove',
  'git.empty': 'No personal tokens yet. Add a host below.',
  'git.provider': 'Provider',
  'git.host': 'Host',
  'git.username': 'Username (optional)',
  'git.token': 'Personal token',
  'git.tokenPlaceholder': 'PAT with repo + issues/PR scope',
  'git.save': 'Save personal token',
  'git.provider.gitea': 'Gitea',
  'git.provider.github': 'GitHub',
  'git.provider.gitlab': 'GitLab',
  'git.provider.generic': 'Other git host',
} as const

export type MessageKey = keyof typeof en
