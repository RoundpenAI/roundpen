import type { MessageKey } from '../i18n'

type TFn = (key: MessageKey, vars?: Record<string, string | number>) => string

const OPTION_KEYS: Record<string, MessageKey> = {
  allow: 'chat.perm.option.allowOnce',
  allow_once: 'chat.perm.option.allowOnce',
  allow_tool: 'chat.perm.option.allowTool',
  allow_all: 'chat.perm.option.allowAll',
  reject: 'chat.perm.option.reject',
  reject_tool: 'chat.perm.option.rejectTool',
}

/** Stable Auto/L2/L3 rule ids → i18n (classifier prose rules stay as-is). */
const RULE_KEYS: Record<string, MessageKey> = {
  high_side_effect: 'chat.perm.rule.high_side_effect',
  'Cloud Metadata': 'chat.perm.rule.cloud_metadata',
  'Control Plane': 'chat.perm.rule.control_plane',
  'Remote Shell Pipe': 'chat.perm.rule.remote_shell_pipe',
  'Secret Exfiltration': 'chat.perm.rule.secret_exfiltration',
  classifier_unavailable: 'chat.perm.rule.classifier_unavailable',
}

const FIXED_REASON_KEYS: Record<string, MessageKey> = {
  high_side_effect: 'chat.perm.reason.high_side_effect',
  'Remote Shell Pipe': 'chat.perm.reason.remote_shell_pipe',
  'Secret Exfiltration': 'chat.perm.reason.secret_exfiltration',
}

const HOST_REASON_PATTERNS: {
  rule: string
  re: RegExp
  key: MessageKey
}[] = [
  {
    rule: 'Cloud Metadata',
    re: /^blocked access to cloud instance metadata \((.+)\)$/,
    key: 'chat.perm.reason.cloud_metadata',
  },
  {
    rule: 'Control Plane',
    re: /^blocked access to the Roundpen control plane on loopback \((.+)\)$/,
    key: 'chat.perm.reason.control_plane',
  },
]

const CLASSIFIER_UNAVAILABLE_RE =
  /^auto mode classifier unavailable: (.+)$/

/** ACP option id → localized label; unknown ids keep the server name. */
export function localizePermissionOptionName(
  optionId: string,
  fallbackName: string,
  t: TFn,
): string {
  const key = OPTION_KEYS[optionId]
  return key ? t(key) : fallbackName || optionId
}

/**
 * Localize `annotatePermissionTitle` output:
 * `{tool} · {rule} — {reason}`. Unknown/classifier rules keep original text.
 */
export function localizePermissionTitle(title: string, t: TFn): string {
  const parsed = parseAnnotatedTitle(title)
  if (!parsed.rule) return title

  const ruleLabel = RULE_KEYS[parsed.rule]
    ? t(RULE_KEYS[parsed.rule])
    : parsed.rule
  const reasonLabel = parsed.reason
    ? localizePermissionReason(parsed.rule, parsed.reason, t)
    : undefined

  if (reasonLabel) return `${parsed.tool} · ${ruleLabel} — ${reasonLabel}`
  return `${parsed.tool} · ${ruleLabel}`
}

export function parseAnnotatedTitle(title: string): {
  tool: string
  rule?: string
  reason?: string
} {
  const ruleSep = ' · '
  const reasonSep = ' — '
  const i = title.indexOf(ruleSep)
  if (i < 0) return { tool: title }
  const tool = title.slice(0, i)
  const rest = title.slice(i + ruleSep.length)
  const j = rest.indexOf(reasonSep)
  if (j < 0) return { tool, rule: rest }
  return {
    tool,
    rule: rest.slice(0, j),
    reason: rest.slice(j + reasonSep.length),
  }
}

function localizePermissionReason(
  rule: string,
  reason: string,
  t: TFn,
): string {
  const fixed = FIXED_REASON_KEYS[rule]
  if (fixed) return t(fixed)

  for (const p of HOST_REASON_PATTERNS) {
    if (p.rule !== rule) continue
    const m = reason.match(p.re)
    if (m?.[1]) return t(p.key, { host: m[1] })
  }

  if (rule === 'classifier_unavailable') {
    const m = reason.match(CLASSIFIER_UNAVAILABLE_RE)
    if (m?.[1]) {
      return t('chat.perm.reason.classifier_unavailable', { detail: m[1] })
    }
  }

  return reason
}
