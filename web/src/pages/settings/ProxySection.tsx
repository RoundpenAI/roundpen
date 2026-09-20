import { Button, Form, Input, Typography } from '@douyinfe/semi-ui-19'
import type { ProxyProfile } from '../../api'
import { Loading } from '../../components/Loading'
import { sectionGap } from './constants'
import { Field } from './parts'
import type { SectionBase } from './helpers'

type Props = SectionBase & {
  patchProxy: (index: number, next: Partial<ProxyProfile>) => void
}

export function ProxySection({ isAdmin, loading, t, form, patch, patchProxy }: Props) {
  return (
    isAdmin && loading ? (
      <Loading tip={t('settings.loadingSystem')} />
    ) : isAdmin ? (
      <Form labelPosition="top" labelAlign="left" style={sectionGap}>
        <div style={{ ...sectionGap, paddingTop: 16 }}>
          <Typography.Text type="tertiary">
            {t('settings.proxy.intro')}
          </Typography.Text>
          {form.proxies.map((p, i) => (
            <div
              key={i}
              style={{
                display: 'grid',
                gap: 16,
                gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
                alignItems: 'end',
                borderTop: '1px solid var(--semi-color-border)',
                paddingTop: 16,
              }}
            >
              <Field label={t('settings.proxy.id')} hint={t('settings.proxy.idHint')}>
                <Input
                  autoComplete="off"
                  placeholder="us-egress"
                  value={p.id}
                  onChange={(v) => patchProxy(i, { id: v })}
                />
              </Field>
              <Field label={t('settings.proxy.name')}>
                <Input
                  autoComplete="off"
                  value={p.name}
                  onChange={(v) => patchProxy(i, { name: v })}
                />
              </Field>
              <Field label={t('settings.proxy.url')} hint={t('settings.proxy.urlHint')}>
                <Input
                  autoComplete="off"
                  placeholder="socks5://10.0.0.9:1080"
                  value={p.url}
                  onChange={(v) => patchProxy(i, { url: v })}
                />
              </Field>
              <Field label={t('settings.proxy.description')}>
                <Input
                  autoComplete="off"
                  value={p.description ?? ''}
                  onChange={(v) => patchProxy(i, { description: v })}
                />
              </Field>
              <Button
                type="danger"
                onClick={() => patch({ proxies: form.proxies.filter((_, idx) => idx !== i) })}
              >
                {t('settings.proxy.remove')}
              </Button>
            </div>
          ))}
          <Button
            onClick={() =>
              patch({
                proxies: [...form.proxies, { id: '', name: '', url: '', description: '' }],
              })
            }
          >
            {t('settings.proxy.add')}
          </Button>
          <Typography.Text type="tertiary" size="small">
            {t('settings.proxy.note')}
          </Typography.Text>
        </div>
      </Form>
    ) : null
  )
}
