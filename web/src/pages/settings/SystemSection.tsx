import { Form, Typography } from '@douyinfe/semi-ui-19'
import type { SystemInfo } from '../../api'
import { Loading } from '../../components/Loading'
import { sectionGap } from './constants'
import { SystemRow } from './parts'
import type { Translate } from './helpers'

type Props = {
  isAdmin: boolean
  loading: boolean
  t: Translate
  sys: SystemInfo | undefined
}

export function SystemSection({ isAdmin, loading, t, sys }: Props) {
  return (
    isAdmin && loading ? (
      <Loading tip={t('settings.loadingSystem')} />
    ) : isAdmin ? (
      <Form labelPosition="top" labelAlign="left" style={sectionGap}>
      <div style={{ paddingTop: 16 }}>
        {sys ? (
          <div
            style={{
              border: '1px solid var(--semi-color-border)',
              borderRadius: 8,
              padding: 16,
            }}
          >
            <Typography.Title heading={5} style={{ margin: '0 0 12px' }}>
              {t('settings.system.title')}
            </Typography.Title>
            <dl
              style={{
                margin: 0,
                display: 'grid',
                gridTemplateColumns: '7.5rem 1fr',
                columnGap: 16,
                rowGap: 8,
              }}
            >
              <SystemRow label={t('settings.system.backend')} value={sys.backend} />
              <SystemRow label={t('settings.system.httpAddr')} value={sys.httpAddr} />
              <SystemRow label={t('settings.system.dataRoot')} value={sys.dataRoot} />
              <SystemRow label={t('settings.system.dockerHost')} value={sys.dockerHost} />
              <SystemRow
                label={t('settings.system.llmgw')}
                value={
                  sys.llmgwActive
                    ? t('settings.system.llmgwActive')
                    : sys.llmgwMounted
                      ? t('settings.system.llmgwMounted')
                      : t('settings.system.llmgwOff')
                }
              />
              <SystemRow
                label={t('settings.system.cdpProvider')}
                value={sys.cdpProviderActive || 'auto'}
              />
              <SystemRow
                label={t('settings.system.hostChrome')}
                value={sys.cdpHostChromeFound ? t('settings.system.hostChromeFound') : t('settings.system.hostChromeMissing')}
              />
            </dl>
            {sys.cdpHint && (
              <Typography.Text
                type="tertiary"
                size="small"
                style={{ display: 'block', marginTop: 12 }}
              >
                {sys.cdpHint}
              </Typography.Text>
            )}
            <Typography.Text
                  type="tertiary"
                  size="small"
                  style={{ display: 'block', marginTop: 12 }}
                >
                  {t('settings.system.footer')}
                </Typography.Text>
          </div>
        ) : (
          <Typography.Text type="tertiary" size="small">
            {t('settings.system.unavailable')}
          </Typography.Text>
        )}
      </div>
      </Form>
    ) : null
  )
}
