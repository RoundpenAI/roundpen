import { createContext, useContext, useEffect, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Button, Layout, SideSheet, Typography } from '@douyinfe/semi-ui-19'
import {
  IconApps,
  IconCheckList,
  IconExit,
  IconFolder,
  IconMenu,
  IconServer,
  IconSetting,
  IconUser,
} from '@douyinfe/semi-icons'
import { auth as authApi } from '../api'
import { doLogout, useAuth } from '../auth'
import { useT } from '../i18n'
import { ThemeToggle } from './ThemeToggle'
import { LocaleToggle } from './LocaleToggle'
import {
  matchPrimaryMenu,
  readPrimaryCollapsed,
  visiblePrimaryMenus,
  writePrimaryCollapsed,
  type PrimaryMenuId,
} from '../lib/appNav'

const { Sider, Content } = Layout

const OpenAppMenuContext = createContext<(() => void) | null>(null)

export function useOpenAppMenu(): () => void {
  return useContext(OpenAppMenuContext) ?? (() => {})
}

function menuIcon(id: PrimaryMenuId) {
  if (id === 'projects') return <IconApps />
  if (id === 'issues') return <IconCheckList />
  if (id === 'workspace') return <IconFolder />
  if (id === 'settings') return <IconSetting />
  if (id === 'admin') return <IconServer />
  return <IconUser />
}

export function AppShell() {
  const auth = useAuth()
  const t = useT()
  const user = auth.status === 'ok' ? auth.user : null
  const isAdmin = user?.role === 'admin'
  const navigate = useNavigate()
  const location = useLocation()
  const [collapsed, setCollapsed] = useState(readPrimaryCollapsed)
  const [mobileOpen, setMobileOpen] = useState(false)
  const [appVersion, setAppVersion] = useState('')
  const active = matchPrimaryMenu(location.pathname)
  const menus = visiblePrimaryMenus(Boolean(isAdmin))

  useEffect(() => {
    document.body.classList.add('chat-lock')
    return () => document.body.classList.remove('chat-lock')
  }, [])

  useEffect(() => {
    setMobileOpen(false)
  }, [location.pathname])

  useEffect(() => {
    let alive = true
    authApi
      .bootstrapHint()
      .then((hint) => {
        if (alive && hint.version) setAppVersion(hint.version)
      })
      .catch(() => {
        /* version is decorative */
      })
    return () => {
      alive = false
    }
  }, [])

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      const next = !prev
      writePrimaryCollapsed(next)
      return next
    })
  }

  const rail = (opts: { collapsedView: boolean; onNavigate?: () => void }) => (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        background: 'var(--semi-color-bg-1)',
      }}
    >
      <div style={{ padding: 8 }}>
        <Button
          theme="borderless"
          type="tertiary"
          icon={<IconMenu />}
          aria-label={
            opts.collapsedView ? t('nav.expandPrimary') : t('nav.collapsePrimary')
          }
          onClick={() => {
            if (mobileOpen) {
              setMobileOpen(false)
              return
            }
            toggleCollapsed()
          }}
        />
      </div>
      <div
        style={{
          flex: 1,
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
          padding: 8,
        }}
      >
        {menus.map((m) => (
          <Button
            key={m.id}
            theme={active === m.id ? 'light' : 'borderless'}
            type={active === m.id ? 'primary' : 'tertiary'}
            icon={menuIcon(m.id)}
            style={{
              justifyContent: opts.collapsedView ? 'center' : 'flex-start',
            }}
            onClick={() => {
              opts.onNavigate?.()
              navigate(m.to)
            }}
          >
            {opts.collapsedView ? null : t(m.labelKey)}
          </Button>
        ))}
      </div>
      <div
        style={{
          padding: 8,
          borderTop: '1px solid var(--semi-color-border)',
          display: 'flex',
          flexDirection: 'column',
          gap: 4,
        }}
      >
        {!opts.collapsedView && (
          <>
            <LocaleToggle block />
            <ThemeToggle />
          </>
        )}
        {user && (
          <Button
            theme="borderless"
            type="tertiary"
            icon={<IconExit />}
            style={{
              justifyContent: opts.collapsedView ? 'center' : 'flex-start',
              width: '100%',
            }}
            aria-label={t('nav.signOutUser', { user: user.username })}
            onClick={() => void doLogout().then(() => navigate('/login'))}
          >
            {opts.collapsedView
              ? null
              : t('nav.signOutUser', { user: user.username })}
          </Button>
        )}
        {appVersion ? (
          <Typography.Text
            type="tertiary"
            size="small"
            title={t('nav.version', { version: appVersion })}
            style={{
              display: 'block',
              textAlign: opts.collapsedView ? 'center' : 'left',
              padding: opts.collapsedView ? '4px 0 0' : '4px 8px 0',
              fontFamily: 'var(--semi-font-family-code)',
              fontSize: 11,
              opacity: 0.7,
              userSelect: 'text',
            }}
          >
            {opts.collapsedView ? `v${appVersion}` : t('nav.version', { version: appVersion })}
          </Typography.Text>
        ) : null}
      </div>
    </div>
  )

  // Assistant chrome owns its own menu affordance; other areas use this bar.
  // Do not use pathname.startsWith('/a') — "/admin/…" would match too.
  const inAssistant = active === 'assistants'

  return (
    <OpenAppMenuContext.Provider value={() => setMobileOpen(true)}>
    <Layout style={{ height: '100%', background: 'var(--semi-color-bg-0)' }}>
      <Sider
        className="rp-app-primary-desktop"
        style={{
          width: collapsed ? 64 : 120,
          maxWidth: collapsed ? 64 : 120,
          minWidth: collapsed ? 64 : 120,
          background: 'var(--semi-color-bg-1)',
          borderRight: '1px solid var(--semi-color-border)',
          display: 'none',
        }}
      >
        {rail({ collapsedView: collapsed })}
      </Sider>
      <SideSheet
        title={t('nav.menu')}
        visible={mobileOpen}
        onCancel={() => setMobileOpen(false)}
        placement="left"
        width={280}
        bodyStyle={{ padding: 0 }}
      >
        {rail({
          collapsedView: false,
          onNavigate: () => setMobileOpen(false),
        })}
      </SideSheet>
      <Layout>
        {!inAssistant && (
        <div
          className="rp-app-mobile-bar"
          style={{
            display: 'none',
            alignItems: 'center',
            borderBottom: '1px solid var(--semi-color-border)',
            background: 'var(--semi-color-bg-1)',
          }}
        >
          <Button
            theme="borderless"
            type="tertiary"
            icon={<IconMenu />}
            aria-label={t('nav.openMenu')}
            onClick={() => setMobileOpen(true)}
          />
          <Typography.Text strong style={{ marginLeft: 8 }}>
            Roundpen
          </Typography.Text>
        </div>
        )}
        <Content
          className="rp-app-content"
          style={{
            minHeight: 0,
            minWidth: 0,
            // Document pages (files / issues) scroll here; chat fills height
            // and keeps its own .chat-pane-scroll so it does not grow this box.
            overflowX: 'hidden',
            overflowY: 'auto',
            display: 'flex',
            flexDirection: 'column',
            flex: 1,
            WebkitOverflowScrolling: 'touch',
            overscrollBehavior: 'contain',
          }}
        >
          <Outlet />
        </Content>
      </Layout>
      <style>{`
        @media (min-width: 768px) {
          .rp-app-primary-desktop { display: block !important; }
          .rp-app-mobile-bar { display: none !important; }
        }
        @media (max-width: 767px) {
          .rp-app-mobile-bar { display: flex !important; }
        }
      `}</style>
    </Layout>
    </OpenAppMenuContext.Provider>
  )
}
