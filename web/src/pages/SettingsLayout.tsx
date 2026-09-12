import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Button, Layout, Typography } from '@douyinfe/semi-ui-19'
import { useAuth } from '../auth'
import { useT } from '../i18n'
import {
  resolveSettingsSection,
  visibleSettingsSections,
} from '../lib/appNav'

const { Sider, Content } = Layout

export function SettingsLayout() {
  const auth = useAuth()
  const t = useT()
  const isAdmin = auth.status === 'ok' && auth.user.role === 'admin'
  const location = useLocation()
  const navigate = useNavigate()
  const rawSection = location.pathname.split('/')[2]
  const active = resolveSettingsSection(rawSection, isAdmin)
  const sections = visibleSettingsSections(isAdmin)

  return (
    <Layout style={{ height: '100%', background: 'var(--semi-color-bg-0)' }}>
      <Sider
        style={{
          width: 220,
          minWidth: 220,
          maxWidth: 220,
          background: 'var(--semi-color-bg-1)',
          borderRight: '1px solid var(--semi-color-border)',
          padding: 8,
        }}
      >
        <Typography.Text
          type="tertiary"
          size="small"
          style={{ display: 'block', padding: '8px 8px 4px' }}
        >
          {t('settings.title')}
        </Typography.Text>
        {sections.map((s) => (
          <Button
            key={s.key}
            theme={active === s.key ? 'light' : 'borderless'}
            type={active === s.key ? 'primary' : 'tertiary'}
            style={{
              justifyContent: 'flex-start',
              width: '100%',
              marginBottom: 2,
            }}
            onClick={() => navigate(`/settings/${s.key}`)}
          >
            {t(s.labelKey)}
          </Button>
        ))}
      </Sider>
      <Content
        style={{
          minWidth: 0,
          minHeight: 0,
          overflow: 'auto',
          flex: 1,
        }}
      >
        <Outlet />
      </Content>
    </Layout>
  )
}
