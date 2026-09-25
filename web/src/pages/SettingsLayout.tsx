import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Button, Layout, Typography } from '@douyinfe/semi-ui-19'
import { useT } from '../i18n'
import { SETTINGS_AREAS, type SettingsArea } from '../lib/appNav'

const { Sider, Content } = Layout

export function SettingsLayout({ area }: { area: SettingsArea }) {
  const t = useT()
  const location = useLocation()
  const navigate = useNavigate()
  const { basePath, titleKey, groups, sections, resolve } = SETTINGS_AREAS[area]
  const rawSection = location.pathname.slice(basePath.length).replace(/^\//, '')
  const active = resolve(rawSection)

  return (
    <div
      style={{
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
        background: 'var(--semi-color-bg-0)',
      }}
    >
      <div
        className="rp-settings-tabs-mobile"
        style={{
          display: 'none',
          gap: 4,
          overflowX: 'auto',
          padding: 8,
          flex: '0 0 auto',
          background: 'var(--semi-color-bg-1)',
          borderBottom: '1px solid var(--semi-color-border)',
        }}
      >
        {sections.map((s) => (
          <Button
            key={s.key}
            size="small"
            theme={active === s.key ? 'light' : 'borderless'}
            type={active === s.key ? 'primary' : 'tertiary'}
            style={{ flex: '0 0 auto' }}
            onClick={() => navigate(`${basePath}/${s.key}`)}
          >
            {t(s.labelKey)}
          </Button>
        ))}
      </div>
      <Layout style={{ flex: 1, minHeight: 0 }}>
        <Sider
          className="rp-settings-sider-desktop"
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
            {t(titleKey)}
          </Typography.Text>
          {groups.map((g) => (
            <div key={g.key}>
              <Typography.Text
                type="quaternary"
                size="small"
                style={{ display: 'block', padding: '12px 8px 4px' }}
              >
                {t(g.labelKey)}
              </Typography.Text>
              {sections
                .filter((s) => s.group === g.key)
                .map((s) => (
                  <Button
                    key={s.key}
                    theme={active === s.key ? 'light' : 'borderless'}
                    type={active === s.key ? 'primary' : 'tertiary'}
                    style={{
                      justifyContent: 'flex-start',
                      width: '100%',
                      marginBottom: 2,
                    }}
                    onClick={() => navigate(`${basePath}/${s.key}`)}
                  >
                    {t(s.labelKey)}
                  </Button>
                ))}
            </div>
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
      <style>{`
        @media (min-width: 768px) {
          .rp-settings-sider-desktop { display: block !important; }
          .rp-settings-tabs-mobile { display: none !important; }
        }
        @media (max-width: 767px) {
          .rp-settings-sider-desktop { display: none !important; }
          .rp-settings-tabs-mobile { display: flex !important; }
        }
      `}</style>
    </div>
  )
}
