# App Shell + Settings Sections Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unify 助手 / 设置 / 镜像 under a shared `AppShell` (collapsible primary rail + optional secondary column + content), and move Agent runtime + Git personal tokens into settings secondary sections alongside existing admin tabs.

**Architecture:** Nested layouts. `AppShell` owns the primary icon rail (折叠持久化). Module layouts under `/a` and `/settings` add a secondary column; `/registry` renders content only. Pure helpers in `web/src/lib/appNav.ts` own menu/section tables and default-section resolution. `PageShell` remains only for advanced pages (`/browser`, sandboxes).

**Tech Stack:** React 19, react-router-dom 7, Semi Design (`@douyinfe/semi-ui-19`), `node:test` unit tests via `node --test --experimental-strip-types`.

**Spec:** `docs/superpowers/specs/2026-09-12-app-shell-settings-tabs-design.md`

---

## File map

| Path | Responsibility |
|------|----------------|
| `web/src/lib/appNav.ts` | Top-level menus, settings sections, `resolveSettingsSection`, primary collapse key helpers |
| `web/src/lib/appNav.test.ts` | Unit tests for section resolution / admin filtering |
| `web/src/components/AppShell.tsx` | Primary rail + content `Outlet`; collapse; mobile drawer chrome |
| `web/src/components/AssistantLayout.tsx` | Becomes secondary column (assistant list) + header/content under `/a`; drop bottom Settings/镜像 links |
| `web/src/pages/SettingsLayout.tsx` | Secondary column of settings sections + `Outlet` |
| `web/src/pages/SettingsPage.tsx` | Render one section’s content; drop `PageShell` + horizontal `Tabs`; keep dirty admin form |
| `web/src/pages/TemplatesPage.tsx` | Drop `PageShell`; scrollable content pane styles |
| `web/src/App.tsx` | Mount modules under `AppShell` routes |
| `web/src/components/PageShell.tsx` | Keep for `/browser` (+ sandboxes); import shared labels from `appNav` only if needed — do not drive 助手/设置/镜像 |

---

### Task 1: `appNav` helpers + tests

**Files:**
- Create: `web/src/lib/appNav.ts`
- Create: `web/src/lib/appNav.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  PRIMARY_MENUS,
  SETTINGS_SECTIONS,
  resolveSettingsSection,
  visibleSettingsSections,
  visiblePrimaryMenus,
} from './appNav.ts'

describe('visiblePrimaryMenus', () => {
  it('hides registry for non-admin', () => {
    const keys = visiblePrimaryMenus(false).map((m) => m.id)
    assert.deepEqual(keys, ['assistants', 'settings'])
  })

  it('shows registry for admin', () => {
    const keys = visiblePrimaryMenus(true).map((m) => m.id)
    assert.deepEqual(keys, ['assistants', 'settings', 'registry'])
  })
})

describe('visibleSettingsSections', () => {
  it('non-admin only runtime + git', () => {
    assert.deepEqual(
      visibleSettingsSections(false).map((s) => s.key),
      ['runtime', 'git'],
    )
  })

  it('admin gets all sections in order', () => {
    assert.deepEqual(
      visibleSettingsSections(true).map((s) => s.key),
      SETTINGS_SECTIONS.map((s) => s.key),
    )
  })
})

describe('resolveSettingsSection', () => {
  it('defaults to runtime', () => {
    assert.equal(resolveSettingsSection(undefined, false), 'runtime')
    assert.equal(resolveSettingsSection('', true), 'runtime')
  })

  it('accepts known visible section', () => {
    assert.equal(resolveSettingsSection('git', false), 'git')
    assert.equal(resolveSettingsSection('general', true), 'general')
  })

  it('rejects unknown or unauthorized section', () => {
    assert.equal(resolveSettingsSection('nope', true), 'runtime')
    assert.equal(resolveSettingsSection('general', false), 'runtime')
  })
})

describe('PRIMARY_MENUS paths', () => {
  it('matches product routes', () => {
    assert.equal(PRIMARY_MENUS.find((m) => m.id === 'assistants')?.to, '/a')
    assert.equal(PRIMARY_MENUS.find((m) => m.id === 'settings')?.to, '/settings')
    assert.equal(PRIMARY_MENUS.find((m) => m.id === 'registry')?.to, '/registry')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && node --test --experimental-strip-types src/lib/appNav.test.ts`

Expected: FAIL (module not found)

- [ ] **Step 3: Write minimal implementation**

```ts
// web/src/lib/appNav.ts
export type PrimaryMenuId = 'assistants' | 'settings' | 'registry'

export type PrimaryMenu = {
  id: PrimaryMenuId
  to: string
  label: string
  admin?: boolean
}

export const PRIMARY_MENUS: PrimaryMenu[] = [
  { id: 'assistants', to: '/a', label: '助手' },
  { id: 'settings', to: '/settings', label: '设置' },
  { id: 'registry', to: '/registry', label: '镜像', admin: true },
]

export type SettingsSectionKey =
  | 'runtime'
  | 'git'
  | 'general'
  | 'preview'
  | 'builds'
  | 'browser'
  | 'llmgw'
  | 'system'

export type SettingsSection = {
  key: SettingsSectionKey
  label: string
  admin?: boolean
}

export const SETTINGS_SECTIONS: SettingsSection[] = [
  { key: 'runtime', label: 'Agent runtime' },
  { key: 'git', label: 'Git personal tokens' },
  { key: 'general', label: 'General', admin: true },
  { key: 'preview', label: 'Preview', admin: true },
  { key: 'builds', label: 'Builds', admin: true },
  { key: 'browser', label: 'Browser', admin: true },
  { key: 'llmgw', label: 'LLM gateway', admin: true },
  { key: 'system', label: 'System', admin: true },
]

export const PRIMARY_COLLAPSED_KEY = 'roundpen.app.primaryCollapsed'

export function visiblePrimaryMenus(isAdmin: boolean): PrimaryMenu[] {
  return PRIMARY_MENUS.filter((m) => !m.admin || isAdmin)
}

export function visibleSettingsSections(isAdmin: boolean): SettingsSection[] {
  return SETTINGS_SECTIONS.filter((s) => !s.admin || isAdmin)
}

export function resolveSettingsSection(
  raw: string | undefined,
  isAdmin: boolean,
): SettingsSectionKey {
  const key = (raw ?? '').trim() as SettingsSectionKey
  const allowed = visibleSettingsSections(isAdmin)
  if (allowed.some((s) => s.key === key)) return key
  return 'runtime'
}

export function readPrimaryCollapsed(): boolean {
  try {
    return localStorage.getItem(PRIMARY_COLLAPSED_KEY) === '1'
  } catch {
    return false
  }
}

export function writePrimaryCollapsed(collapsed: boolean): void {
  try {
    localStorage.setItem(PRIMARY_COLLAPSED_KEY, collapsed ? '1' : '0')
  } catch {
    /* ignore */
  }
}

/** Which primary menu matches the current pathname. */
export function matchPrimaryMenu(pathname: string): PrimaryMenuId {
  if (pathname.startsWith('/settings')) return 'settings'
  if (pathname.startsWith('/registry')) return 'registry'
  return 'assistants'
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && node --test --experimental-strip-types src/lib/appNav.test.ts`

Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/appNav.ts web/src/lib/appNav.test.ts
git commit -m "$(cat <<'EOF'
feat(web): add app nav and settings section helpers

EOF
)"
```

---

### Task 2: `AppShell` primary rail

**Files:**
- Create: `web/src/components/AppShell.tsx`
- Modify: `web/src/App.tsx` (minimal wrap — full route rewire in Task 5; this task can temporarily wrap existing routes or land shell unused until Task 5 — prefer creating the component fully here and wiring in Task 5)

- [ ] **Step 1: Implement `AppShell`**

Create `web/src/components/AppShell.tsx` that:

1. Reads auth via `useAuth()`; `isAdmin = user?.role === 'admin'`.
2. Renders a horizontal flex `Layout` filling `height: 100%` with `chat-lock` on `document.body` (same as current `AssistantLayout`).
3. **Primary rail** (left):
   - Width `64` when collapsed, `88`–`120` when expanded (pick ~96 expanded so label fits under icon); border-right Semi border.
   - Toggle button at top calling `writePrimaryCollapsed` + local state from `readPrimaryCollapsed`.
   - For each `visiblePrimaryMenus(isAdmin)`: icon button (`IconUserGroup` or `IconUser` for 助手, `IconSetting` for 设置, `IconImage` or `IconAbsolute` for 镜像 — use whatever Semi icons already imported nearby; if unsure `IconApps` / `IconSetting` / `IconTemplate`).
   - Active state from `matchPrimaryMenu(location.pathname)`.
   - Click → `navigate(item.to)`.
   - When expanded, show label text under or beside icon.
   - Footer: `ThemeToggle` (expanded or icon-only), Sign out button (reuse `doLogout` pattern from `AssistantLayout`).
4. **Content**: `<Outlet />` in a flex column `minWidth: 0; flex: 1; overflow: hidden`.
5. **Mobile** (`max-width: 767px`): hide desktop primary rail; show hamburger in a thin top bar that opens `SideSheet` listing primary menus (+ theme / logout). Content area still full width.
6. Export nothing else required beyond `AppShell` default/named export.

Skeleton (adapt styles to match `AssistantLayout` density):

```tsx
import { useEffect, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Button, Layout, SideSheet, Typography } from '@douyinfe/semi-ui-19'
import { IconMenu, IconSetting, IconExit } from '@douyinfe/semi-icons'
import { doLogout, useAuth } from '../auth'
import { ThemeToggle } from './ThemeToggle'
import {
  matchPrimaryMenu,
  readPrimaryCollapsed,
  visiblePrimaryMenus,
  writePrimaryCollapsed,
  type PrimaryMenuId,
} from '../lib/appNav'

const { Sider, Content } = Layout

function menuIcon(id: PrimaryMenuId) {
  if (id === 'settings') return <IconSetting />
  // assistants / registry: use IconMenu or other Semi icons available in the project
  return <IconMenu />
}

export function AppShell() {
  const auth = useAuth()
  const user = auth.status === 'ok' ? auth.user : null
  const isAdmin = user?.role === 'admin'
  const navigate = useNavigate()
  const location = useLocation()
  const [collapsed, setCollapsed] = useState(readPrimaryCollapsed)
  const [mobileOpen, setMobileOpen] = useState(false)
  const active = matchPrimaryMenu(location.pathname)
  const menus = visiblePrimaryMenus(Boolean(isAdmin))

  useEffect(() => {
    document.body.classList.add('chat-lock')
    return () => document.body.classList.remove('chat-lock')
  }, [])

  useEffect(() => {
    setMobileOpen(false)
  }, [location.pathname])

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
          aria-label={opts.collapsedView ? '展开一级菜单' : '收起一级菜单'}
          onClick={toggleCollapsed}
        />
      </div>
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 4, padding: 8 }}>
        {menus.map((m) => (
          <Button
            key={m.id}
            theme={active === m.id ? 'light' : 'borderless'}
            type={active === m.id ? 'primary' : 'tertiary'}
            icon={menuIcon(m.id)}
            style={{ justifyContent: opts.collapsedView ? 'center' : 'flex-start' }}
            onClick={() => {
              opts.onNavigate?.()
              navigate(m.to)
            }}
          >
            {opts.collapsedView ? null : m.label}
          </Button>
        ))}
      </div>
      <div style={{ padding: 8, borderTop: '1px solid var(--semi-color-border)' }}>
        {!opts.collapsedView && <ThemeToggle />}
        {user && (
          <Button
            theme="borderless"
            type="tertiary"
            icon={<IconExit />}
            style={{ justifyContent: 'flex-start', width: '100%' }}
            onClick={() => void doLogout().then(() => navigate('/login'))}
          >
            {opts.collapsedView ? null : `退出 (${user.username})`}
          </Button>
        )}
      </div>
    </div>
  )

  return (
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
        title="菜单"
        visible={mobileOpen}
        onCancel={() => setMobileOpen(false)}
        placement="left"
        width={280}
        bodyStyle={{ padding: 0 }}
      >
        {rail({ collapsedView: false, onNavigate: () => setMobileOpen(false) })}
      </SideSheet>
      <Layout>
        <div
          className="rp-app-mobile-bar"
          style={{
            display: 'none',
            alignItems: 'center',
            height: 48,
            padding: '0 12px',
            borderBottom: '1px solid var(--semi-color-border)',
            background: 'var(--semi-color-bg-1)',
          }}
        >
          <Button
            theme="borderless"
            type="tertiary"
            icon={<IconMenu />}
            aria-label="打开菜单"
            onClick={() => setMobileOpen(true)}
          />
          <Typography.Text strong style={{ marginLeft: 8 }}>
            Roundpen
          </Typography.Text>
        </div>
        <Content
          style={{
            minHeight: 0,
            minWidth: 0,
            overflow: 'hidden',
            display: 'flex',
            flexDirection: 'column',
            flex: 1,
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
  )
}
```

Pick concrete Semi icons for assistants/registry by grepping `@douyinfe/semi-icons` usage in the repo; prefer icons already depended on.

- [ ] **Step 2: Typecheck shell in isolation**

Run: `cd web && npx tsc -b --pretty false 2>&1 | head -40`

Expected: no errors in `AppShell.tsx` (other pre-existing branch errors may appear — fix only what this task introduced).

- [ ] **Step 3: Commit**

```bash
git add web/src/components/AppShell.tsx
git commit -m "$(cat <<'EOF'
feat(web): add AppShell primary rail

EOF
)"
```

---

### Task 3: Settings layout + sectioned page

**Files:**
- Create: `web/src/pages/SettingsLayout.tsx`
- Modify: `web/src/pages/SettingsPage.tsx`

- [ ] **Step 1: Add `SettingsLayout`**

```tsx
// web/src/pages/SettingsLayout.tsx
import { Outlet, useNavigate, useParams } from 'react-router-dom'
import { Button, Layout, Typography } from '@douyinfe/semi-ui-19'
import { useAuth } from '../auth'
import {
  resolveSettingsSection,
  visibleSettingsSections,
} from '../lib/appNav'

const { Sider, Content } = Layout

export function SettingsLayout() {
  const auth = useAuth()
  const isAdmin = auth.status === 'ok' && auth.user.role === 'admin'
  const { section } = useParams()
  const navigate = useNavigate()
  const active = resolveSettingsSection(section, isAdmin)
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
          设置
        </Typography.Text>
        {sections.map((s) => (
          <Button
            key={s.key}
            theme={active === s.key ? 'light' : 'borderless'}
            type={active === s.key ? 'primary' : 'tertiary'}
            style={{ justifyContent: 'flex-start', width: '100%', marginBottom: 2 }}
            onClick={() => navigate(`/settings/${s.key}`)}
          >
            {s.label}
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
```

On mobile, secondary may squeeze; acceptable for v1 (full responsive polish optional). If width &lt; 768, stack or put section list in a compact select — prefer keeping sider and relying on horizontal scroll only if needed; do not block on perfect mobile secondary.

- [ ] **Step 2: Refactor `SettingsPage` content switching**

In `web/src/pages/SettingsPage.tsx`:

1. Remove `PageShell` wrapper; return a fragment / `div` with padding (`chat-pane`-like scroll: `padding: 16px 12px; maxWidth: 768; margin: 0 auto`).
2. Remove horizontal `<Tabs>` / `<TabPane>`.
3. Read `const { section: sectionParam } = useParams()`; `const section = resolveSettingsSection(sectionParam, isAdmin)`.
4. If `sectionParam` is present and `resolveSettingsSection` remapped it, `<Navigate to={`/settings/${section}`} replace />`.
5. Render by `section`:
   - `runtime` → `<RuntimePanel />`
   - `git` → `<GitCredentialsPanel />`
   - admin keys → existing form bodies (keep one shared `form` / `dirty` / `load` / `onSave` state so switching sections does not wipe dirty admin fields).
6. Bottom Save/Reload bar: show only when `isAdmin` and section is an admin form section (`general|preview|builds|browser|llmgw|system`) — or always show for admin when any admin form is dirty (prefer: show whenever `isAdmin && !loading`, same as today, so dirty save remains reachable from runtime/git too).
7. Remove the stacked `<RuntimePanel />` / `<GitCredentialsPanel />` above tabs.
8. Keep `Navigate to="/login"` when unauthenticated.

Key structure after change (illustrative):

```tsx
export function SettingsPage() {
  // ... existing state/load/save unchanged ...
  const { section: sectionParam } = useParams()
  const section = resolveSettingsSection(sectionParam, isAdmin)

  if (auth.status !== 'ok') return <Navigate to="/login" replace />
  if (sectionParam && sectionParam !== section) {
    return <Navigate to={`/settings/${section}`} replace />
  }

  return (
    <div style={{ padding: '16px 12px 96px', maxWidth: 768, margin: '0 auto' }}>
      {section === 'runtime' && <RuntimePanel />}
      {section === 'git' && <GitCredentialsPanel />}
      {isAdmin && section === 'general' && (/* former General TabPane body */)}
      {/* ... other admin sections ... */}
      {/* sticky Save bar for admin */}
    </div>
  )
}
```

- [ ] **Step 3: Typecheck**

Run: `cd web && npx tsc -b --pretty false 2>&1 | head -50`

Expected: no new errors from Settings files.

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/SettingsLayout.tsx web/src/pages/SettingsPage.tsx
git commit -m "$(cat <<'EOF'
feat(web): settings secondary sections instead of page tabs

EOF
)"
```

---

### Task 4: Slim `AssistantLayout` into secondary column

**Files:**
- Modify: `web/src/components/AssistantLayout.tsx`

- [ ] **Step 1: Remove primary concerns from assistant layout**

Edit `AssistantLayout` so it no longer owns the full-app chrome:

1. Remove bottom footer buttons that navigate to Settings / 镜像 (`NAV.filter...`); remove `ThemeToggle` / logout from this sidebar (now on `AppShell`).
2. Remove import of `NAV` from `PageShell` if unused.
3. Keep assistant list, pending tickets, new-assistant buttons, `useAssistantLayout` context, header (“详情”), and `<Outlet />`.
4. Remove `document.body.classList.add('chat-lock')` if `AppShell` already does it (avoid double toggle bugs — only one owner).
5. Change outer structure to: secondary `Sider` (width 260, not the old collapse-to-64 for the whole left — **assistant list secondary is not the collapsible primary**; drop `SIDEBAR_KEY` collapse of this column, or keep a local “hide list” only if cheap — YAGNI: fixed 260 desktop secondary).
6. Mobile: keep SideSheet for assistant list (hamburger can live in assistant header). Note: `AppShell` also has a mobile menu — both are OK: AppShell = top-level switch; Assistant = list.
7. Title header logic unchanged.

Do not break `useAssistantLayout` consumers (`AssistantDetailPage`, `AssistantCreatePage`).

- [ ] **Step 2: Smoke typecheck**

Run: `cd web && npx tsc -b --pretty false 2>&1 | head -50`

- [ ] **Step 3: Commit**

```bash
git add web/src/components/AssistantLayout.tsx
git commit -m "$(cat <<'EOF'
refactor(web): AssistantLayout as secondary column under AppShell

EOF
)"
```

---

### Task 5: Wire routes in `App.tsx` + Templates page

**Files:**
- Modify: `web/src/App.tsx`
- Modify: `web/src/pages/TemplatesPage.tsx`

- [ ] **Step 1: Nest routes under `AppShell`**

Replace the authenticated assistant / settings / registry routes with:

```tsx
import { AppShell } from './components/AppShell'
import { SettingsLayout } from './pages/SettingsLayout'

// inside <Routes>:
<Route
  element={
    <RequireAuth>
      <AppShell />
    </RequireAuth>
  }
>
  <Route path="/a" element={<AssistantLayout />}>
    <Route index element={<AssistantsIndexRedirect />} />
    <Route path="new" element={<AssistantCreatePage />} />
    <Route path=":assistantId" element={<AssistantDetailPage />} />
    <Route path=":assistantId/chat" element={<AssistantChatRedirect />} />
    <Route path=":assistantId/s/:id" element={<ChatSessionPage />} />
  </Route>
  <Route path="/settings" element={<SettingsLayout />}>
    <Route index element={<Navigate to="runtime" replace />} />
    <Route path=":section" element={<SettingsPage />} />
  </Route>
  <Route path="/registry" element={<TemplatesPage />} />
</Route>

{/* keep outside AppShell */}
<Route path="/browser" element={<RequireAuth><BrowserPage /></RequireAuth>} />
<Route path="/s/:id" element={<RequireAuth><WorkbenchPage /></RequireAuth>} />
```

Ensure `AssistantLayout` is imported as the layout element (not double-wrapped in its own `RequireAuth` if already on parent).

- [ ] **Step 2: Strip `PageShell` from `TemplatesPage`**

Replace:

```tsx
return (
  <PageShell ...>
    ...
  </PageShell>
)
```

with:

```tsx
return (
  <div
    className="chat-pane-scroll"
    style={{ padding: 16, height: '100%', boxSizing: 'border-box', overflow: 'auto' }}
  >
    {/* existing content */}
  </div>
)
```

Remove unused `PageShell` import.

- [ ] **Step 3: Build**

Run: `cd web && npm run build`

Expected: success (tsc + vite build).

- [ ] **Step 4: Commit**

```bash
git add web/src/App.tsx web/src/pages/TemplatesPage.tsx
git commit -m "$(cat <<'EOF'
feat(web): mount assistants, settings, registry under AppShell

EOF
)"
```

---

### Task 6: PageShell / nav cleanup + regression pass

**Files:**
- Modify: `web/src/components/PageShell.tsx` (optional comment / leave as-is for browser)
- Modify: any leftover links to bare `/settings` that should still work (index redirect handles it)
- Check: `AssistantDetailPage` / `AssistantCreatePage` links to `/settings` — OK via index → `runtime`

- [ ] **Step 1: Grep for stale shell usage**

Run: `rg "PageShell|from '\\./PageShell'|NAV" web/src`

Expected:
- `PageShell` still used by `BrowserPage` / `SandboxesPage` only.
- `AssistantLayout` no longer imports `NAV`.
- No settings/templates wrapped in `PageShell`.

- [ ] **Step 2: Re-run unit tests**

Run: `cd web && node --test --experimental-strip-types src/lib/appNav.test.ts src/lib/assistants.test.ts`

Expected: PASS

- [ ] **Step 3: Manual checklist (dev server)**

Run: `cd web && npm run dev` (against local API if available)

Verify:
1. Primary rail collapse persists across refresh.
2. 助手 shows list + chat; 设置 shows section list + form; 镜像 has no secondary column.
3. Non-admin: no 镜像; settings only runtime + git.
4. Admin: Save/Reload still works after editing General then switching to Preview (dirty retained).
5. `/settings` redirects to `/settings/runtime`.
6. `/browser` still loads via PageShell.

- [ ] **Step 4: Final commit if cleanup edits landed**

```bash
git add -u web/src
git commit -m "$(cat <<'EOF'
chore(web): clean nav after AppShell migration

EOF
)"
```

Skip empty commit if nothing changed.

---

## Spec coverage check

| Spec requirement | Task |
|------------------|------|
| Shared `AppShell`, collapsible primary | Task 2 |
| Menus 助手/设置/镜像 + admin filter | Task 1–2 |
| Assistants: secondary list + content | Task 4–5 |
| Settings: secondary sections include runtime/git | Task 3 |
| Path `/settings/:section` + default/redirect | Task 3, 5 |
| Registry: primary + content only | Task 5 |
| No PageShell for settings/registry | Task 3, 5 |
| Dirty admin form retained across section switch | Task 3 |
| Advanced `/browser` untouched shell | Task 5–6 |
| localStorage primary collapse | Task 1–2 |

## Plan self-review notes

- No TBD placeholders; icons for assistants/registry left as “pick from Semi already in repo” with a fallback — implementer must choose concrete imports in Task 2.
- Types `SettingsSectionKey` / `PrimaryMenuId` used consistently across tasks.
- TDD applied to pure nav helpers; UI tasks verified via `tsc` / `npm run build` / manual checklist (no React component test runner in `web/` today).
