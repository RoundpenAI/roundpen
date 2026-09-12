import { useState, type CSSProperties, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Layout, Nav, Typography } from '@douyinfe/semi-ui-19'
import { doLogout, useAuth } from '../auth'
import { ChangePasswordDialog } from './ChangePasswordDialog'
import { ThemeToggle } from './ThemeToggle'

export type AppSection =
  | 'assistants'
  | 'browser'
  | 'templates'
  | 'settings'
  | 'sandboxes'

/** Top nav for advanced pages outside AppShell (/browser, sandboxes).
 *  Primary product nav (助手 / 设置 / 镜像) lives in AppShell. */
export const NAV: {
  id: AppSection
  to: string
  label: string
  admin?: boolean
  advanced?: boolean
}[] = [
  { id: 'assistants', to: '/a', label: '助手' },
  { id: 'settings', to: '/settings', label: '设置' },
  { id: 'browser', to: '/browser', label: '浏览器', advanced: true },
  { id: 'templates', to: '/registry', label: '镜像', admin: true },
]

type Props = {
  subtitle: string
  current: AppSection
  children: ReactNode
  maxWidthClass?: string
  className?: string
  style?: CSSProperties
}

const MAX_WIDTH: Record<string, number | undefined> = {
  'max-w-3xl': 768,
  'max-w-4xl': 896,
  'max-w-5xl': 1024,
  'max-w-6xl': 1152,
  'max-w-full': undefined,
}

export function PageShell({
  subtitle,
  current,
  children,
  maxWidthClass = 'max-w-3xl',
  className = '',
  style,
}: Props) {
  const authState = useAuth()
  const navigate = useNavigate()
  const user = authState.status === 'ok' ? authState.user : null
  const [changePasswordOpen, setChangePasswordOpen] = useState(false)
  const maxWidth = MAX_WIDTH[maxWidthClass] ?? 768

  const navItems = NAV.filter((item) => {
    if (item.advanced) return false
    if (item.admin && user?.role !== 'admin') return false
    return true
  })

  return (
    <Layout
      className={className}
      style={{
        minHeight: '100%',
        maxWidth: maxWidth ?? '100%',
        margin: '0 auto',
        padding: '24px 16px max(2rem, env(safe-area-inset-bottom))',
        background: 'var(--semi-color-bg-0)',
        ...style,
      }}
    >
      <Layout.Header
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'flex-end',
          justifyContent: 'space-between',
          gap: 12,
          padding: '0 0 16px',
          background: 'transparent',
        }}
      >
        <div style={{ minWidth: 0 }}>
          <Typography.Title heading={3} style={{ margin: 0 }}>
            Roundpen
          </Typography.Title>
          <Typography.Text type="tertiary">{subtitle}</Typography.Text>
        </div>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            flexWrap: 'wrap',
          }}
        >
          {user && (
            <Typography.Text
              type="tertiary"
              ellipsis={{ showTooltip: true }}
              style={{ maxWidth: 160 }}
            >
              {user.username}
            </Typography.Text>
          )}
          <ThemeToggle />
          {user && (
            <Button type="tertiary" onClick={() => setChangePasswordOpen(true)}>
              Change password
            </Button>
          )}
          <Button
            type="tertiary"
            onClick={() => void doLogout().then(() => navigate('/login'))}
          >
            Sign out
          </Button>
        </div>
      </Layout.Header>

      <Nav
        mode="horizontal"
        selectedKeys={[current]}
        items={navItems.map((item) => ({
          itemKey: item.id,
          text: item.label,
        }))}
        onSelect={(data) => {
          const item = navItems.find((n) => n.id === data.itemKey)
          if (item) navigate(item.to)
        }}
        style={{
          background: 'transparent',
          borderBottom: '1px solid var(--semi-color-border)',
          marginBottom: 24,
        }}
      />

      <Layout.Content>{children}</Layout.Content>

      <ChangePasswordDialog
        open={changePasswordOpen}
        onClose={() => setChangePasswordOpen(false)}
      />
    </Layout>
  )
}
