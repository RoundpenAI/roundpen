import { Input, TextArea, Typography } from '@douyinfe/semi-ui-19'
import type { AppSettings, SuggestedPrompts } from '../../api'
import type { Translate } from './helpers'
import { sectionGap } from './constants'

function linesToList(raw: string): string[] {
  return raw
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean)
    .slice(0, 5)
}

function listToLines(list: string[] | undefined): string {
  return (list ?? []).join('\n')
}

type KindKey = 'user' | 'system' | 'coordinator'

export function ProductSection({
  isAdmin,
  loading,
  t,
  form,
  patch,
}: {
  isAdmin: boolean
  loading: boolean
  t: Translate
  form: AppSettings
  patch: (partial: Partial<AppSettings>) => void
}) {
  const prompts: SuggestedPrompts = form.suggestedPrompts ?? {
    global: [],
    byKind: {},
  }

  function patchPrompts(next: SuggestedPrompts) {
    patch({ suggestedPrompts: next })
  }

  function setKind(kind: KindKey, raw: string) {
    patchPrompts({
      ...prompts,
      byKind: { ...(prompts.byKind ?? {}), [kind]: linesToList(raw) },
    })
  }

  return (
    <div style={sectionGap}>
      <div>
        <Typography.Title heading={4} style={{ margin: 0 }}>
          {t('settings.product.feedbackTitle')}
        </Typography.Title>
        <Typography.Text type="tertiary">
          {t('settings.product.feedbackHint')}
        </Typography.Text>
      </div>
      <div>
        <Typography.Text strong>{t('settings.product.feedbackUrl')}</Typography.Text>
        <Input
          disabled={!isAdmin || loading}
          value={form.feedbackFormUrl ?? ''}
          placeholder={t('settings.product.feedbackUrlPlaceholder')}
          onChange={(v) => patch({ feedbackFormUrl: v })}
          style={{ marginTop: 6 }}
        />
      </div>

      <div style={{ marginTop: 8 }}>
        <Typography.Title heading={4} style={{ margin: 0 }}>
          {t('settings.product.promptsTitle')}
        </Typography.Title>
        <Typography.Text type="tertiary">
          {t('settings.product.promptsHint')}
        </Typography.Text>
      </div>

      {(
        [
          ['global', t('settings.product.promptsGlobal')],
          ['user', t('settings.product.promptsKindUser')],
          ['system', t('settings.product.promptsKindSystem')],
          ['coordinator', t('settings.product.promptsKindCoordinator')],
        ] as const
      ).map(([key, label]) => (
        <div key={key}>
          <Typography.Text strong>{label}</Typography.Text>
          <TextArea
            disabled={!isAdmin || loading}
            value={
              key === 'global'
                ? listToLines(prompts.global)
                : listToLines(prompts.byKind?.[key])
            }
            placeholder={t('settings.product.promptsPlaceholder')}
            autosize={{ minRows: 2, maxRows: 6 }}
            onChange={(v) => {
              if (key === 'global') {
                patchPrompts({ ...prompts, global: linesToList(v) })
              } else {
                setKind(key, v)
              }
            }}
            style={{ marginTop: 6 }}
          />
        </div>
      ))}
    </div>
  )
}
