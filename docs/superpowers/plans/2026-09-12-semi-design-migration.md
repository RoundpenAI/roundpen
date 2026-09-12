# Semi Design Full Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Roundpen `web/` console UI (Tailwind + DaisyUI) with Semi Design official components in one delivery, including theme toggle, zh_CN locale wiring, AI chat components, Tree file browser, and Semi shells around terminal/VNC.

**Architecture:** Install React-19 Semi packages (`@douyinfe/semi-ui-19`, `@douyinfe/semi-icons`), wrap the app in `ConfigProvider` + UI preference (theme/locale), rewrite every page/shell to Semi primitives, add a pure data adapter from ACP/`AgentMessage` to Semi `AIChatDialogue` messages, then delete DaisyUI/Tailwind. Intermediate commits may still list Daisy in `package.json` until the final cleanup task; no new Daisy classes after a file is migrated.

**Tech Stack:** React 19, Vite 8, `@douyinfe/semi-ui-19` / `@douyinfe/semi-icons`, Playwright e2e, Node test runner for adapter unit tests.

**Spec:** `docs/superpowers/specs/2026-09-12-semi-design-migration-design.md`

---

## File map

| Path | Responsibility |
|------|----------------|
| `web/package.json` | Add Semi-19; remove Daisy/Tailwind at end |
| `web/vite.config.ts` | Drop `@tailwindcss/vite` |
| `web/index.html` | Drop Daisy body classes / `data-theme` |
| `web/src/index.css` | Minimal reset only (height, chat-lock, safe-area) |
| `web/src/lib/uiPreference.ts` | Read/write `roundpen.ui`; apply `theme-mode` on `body` |
| `web/src/lib/uiPreference.test.ts` | Unit tests for preference defaults/parse |
| `web/src/components/SemiAppProvider.tsx` | `ConfigProvider` + preference context + theme apply |
| `web/src/components/ThemeToggle.tsx` | Header Switch for dark/light |
| `web/src/main.tsx` | Wrap `<App />` with `SemiAppProvider` |
| `web/src/lib/semiChatAdapter.ts` | ACP/`AgentMessage` → Semi chat `Message` |
| `web/src/lib/semiChatAdapter.test.ts` | Adapter unit tests |
| `web/src/pages/LoginPage.tsx` | Semi Form login |
| `web/src/components/PageShell.tsx` | Semi Layout + Nav + ThemeToggle |
| `web/src/components/AssistantLayout.tsx` | Semi Layout/Sider/Nav assistant shell |
| `web/src/components/ChangePasswordDialog.tsx` | Semi Modal + Form |
| `web/src/pages/*` (Settings, Templates, Sandboxes, Browser, Workbench, Assistants, Chat) | Semi controls |
| `web/src/components/FileTree.tsx` | Semi Tree |
| `web/src/components/TerminalPane.tsx` / `AgentBrowserPanel.tsx` | Keep kernels; Semi chrome |
| Delete after unused | `ChatComposer.tsx`, `ToolCallCard.tsx` (if fully replaced), Daisy-only CSS |
| `web/e2e/*.ts` | Update copy/selectors for Semi + Chinese where applicable |

---

### Task 1: Install Semi (React 19 packages)

**Files:**
- Modify: `web/package.json`
- Modify: `web/vite.config.ts` (leave Tailwind until final cleanup so `npm run build` still works mid-migration)

- [ ] **Step 1: Install packages**

Run:

```bash
cd web && npm install @douyinfe/semi-ui-19 @douyinfe/semi-icons
```

Expected: `package.json` dependencies include both; lockfile updated.

- [ ] **Step 2: Smoke import in `main.tsx` (temporary)**

Add at top of `web/src/main.tsx` (will expand in Task 3):

```tsx
import '@douyinfe/semi-ui-19/dist/css/semi.css'
```

- [ ] **Step 3: Build**

Run: `cd web && npm run build`

Expected: PASS (CSS imported; no component usage required yet).

- [ ] **Step 4: Commit**

```bash
git add web/package.json web/package-lock.json web/src/main.tsx
git commit -m "chore(web): add Semi Design React 19 packages"
```

---

### Task 2: UI preference module (TDD)

**Files:**
- Create: `web/src/lib/uiPreference.ts`
- Create: `web/src/lib/uiPreference.test.ts`

- [ ] **Step 1: Write failing tests**

```ts
import assert from 'node:assert/strict'
import { describe, it, beforeEach } from 'node:test'
import {
  DEFAULT_UI_PREFERENCE,
  loadUiPreference,
  saveUiPreference,
  applyThemeToDocument,
  type UiPreference,
} from './uiPreference.ts'

describe('uiPreference', () => {
  beforeEach(() => {
    localStorage.clear()
    document.body.removeAttribute('theme-mode')
  })

  it('defaults to dark zh_CN', () => {
    assert.deepEqual(loadUiPreference(), DEFAULT_UI_PREFERENCE)
    assert.equal(DEFAULT_UI_PREFERENCE.theme, 'dark')
    assert.equal(DEFAULT_UI_PREFERENCE.locale, 'zh_CN')
  })

  it('round-trips save/load', () => {
    const next: UiPreference = { theme: 'light', locale: 'en' }
    saveUiPreference(next)
    assert.deepEqual(loadUiPreference(), next)
  })

  it('ignores corrupt JSON', () => {
    localStorage.setItem('roundpen.ui', '{not-json')
    assert.deepEqual(loadUiPreference(), DEFAULT_UI_PREFERENCE)
  })

  it('applyThemeToDocument sets body theme-mode', () => {
    applyThemeToDocument('dark')
    assert.equal(document.body.getAttribute('theme-mode'), 'dark')
    applyThemeToDocument('light')
    assert.equal(document.body.hasAttribute('theme-mode'), false)
  })
})
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `cd web && node --test --experimental-strip-types src/lib/uiPreference.test.ts`

Expected: FAIL (module missing).

- [ ] **Step 3: Implement**

```ts
export type UiTheme = 'dark' | 'light'
export type UiLocale = 'zh_CN' | 'en'

export type UiPreference = {
  theme: UiTheme
  locale: UiLocale
}

export const UI_PREFERENCE_KEY = 'roundpen.ui'

export const DEFAULT_UI_PREFERENCE: UiPreference = {
  theme: 'dark',
  locale: 'zh_CN',
}

function isTheme(v: unknown): v is UiTheme {
  return v === 'dark' || v === 'light'
}

function isLocale(v: unknown): v is UiLocale {
  return v === 'zh_CN' || v === 'en'
}

export function loadUiPreference(): UiPreference {
  try {
    const raw = localStorage.getItem(UI_PREFERENCE_KEY)
    if (!raw) return { ...DEFAULT_UI_PREFERENCE }
    const parsed = JSON.parse(raw) as Partial<UiPreference>
    return {
      theme: isTheme(parsed.theme) ? parsed.theme : DEFAULT_UI_PREFERENCE.theme,
      locale: isLocale(parsed.locale) ? parsed.locale : DEFAULT_UI_PREFERENCE.locale,
    }
  } catch {
    return { ...DEFAULT_UI_PREFERENCE }
  }
}

export function saveUiPreference(pref: UiPreference): void {
  localStorage.setItem(UI_PREFERENCE_KEY, JSON.stringify(pref))
}

/** Semi global dark mode: body[theme-mode=dark]. Light = remove attribute. */
export function applyThemeToDocument(theme: UiTheme): void {
  if (theme === 'dark') {
    document.body.setAttribute('theme-mode', 'dark')
  } else {
    document.body.removeAttribute('theme-mode')
  }
}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `cd web && node --test --experimental-strip-types src/lib/uiPreference.test.ts`

Note: if `localStorage`/`document` are missing in plain node, use a tiny stub at top of the test file:

```ts
const store = new Map<string, string>()
;(globalThis as { localStorage?: Storage }).localStorage = {
  getItem: (k) => store.get(k) ?? null,
  setItem: (k, v) => void store.set(k, String(v)),
  removeItem: (k) => void store.delete(k),
  clear: () => store.clear(),
  key: () => null,
  get length() {
    return store.size
  },
} as Storage
;(globalThis as { document?: Document }).document = {
  body: {
    attrs: new Map<string, string>(),
    setAttribute(name: string, value: string) {
      this.attrs.set(name, value)
    },
    removeAttribute(name: string) {
      this.attrs.delete(name)
    },
    getAttribute(name: string) {
      return this.attrs.get(name) ?? null
    },
    hasAttribute(name: string) {
      return this.attrs.has(name)
    },
  },
} as unknown as Document
```

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/uiPreference.ts web/src/lib/uiPreference.test.ts
git commit -m "feat(web): add UI theme/locale preference helpers"
```

---

### Task 3: SemiAppProvider + ThemeToggle

**Files:**
- Create: `web/src/components/SemiAppProvider.tsx`
- Create: `web/src/components/ThemeToggle.tsx`
- Modify: `web/src/main.tsx`
- Modify: `web/index.html` (remove Daisy body utility classes that fight Semi)

- [ ] **Step 1: Implement provider**

```tsx
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { ConfigProvider } from '@douyinfe/semi-ui-19'
import zh_CN from '@douyinfe/semi-ui-19/lib/es/locale/source/zh_CN'
import en_GB from '@douyinfe/semi-ui-19/lib/es/locale/source/en_GB'
import {
  applyThemeToDocument,
  loadUiPreference,
  saveUiPreference,
  type UiLocale,
  type UiPreference,
  type UiTheme,
} from '../lib/uiPreference'

type Ctx = {
  preference: UiPreference
  setTheme: (theme: UiTheme) => void
  setLocale: (locale: UiLocale) => void
}

const UiContext = createContext<Ctx | null>(null)

export function useUiPreference(): Ctx {
  const ctx = useContext(UiContext)
  if (!ctx) throw new Error('useUiPreference requires SemiAppProvider')
  return ctx
}

const LOCALE_MAP = {
  zh_CN,
  en: en_GB,
} as const

export function SemiAppProvider({ children }: { children: ReactNode }) {
  const [preference, setPreference] = useState<UiPreference>(() => loadUiPreference())

  useEffect(() => {
    applyThemeToDocument(preference.theme)
  }, [preference.theme])

  const setTheme = useCallback((theme: UiTheme) => {
    setPreference((prev) => {
      const next = { ...prev, theme }
      saveUiPreference(next)
      return next
    })
  }, [])

  const setLocale = useCallback((locale: UiLocale) => {
    setPreference((prev) => {
      const next = { ...prev, locale }
      saveUiPreference(next)
      return next
    })
  }, [])

  const value = useMemo(
    () => ({ preference, setTheme, setLocale }),
    [preference, setTheme, setLocale],
  )

  return (
    <UiContext.Provider value={value}>
      <ConfigProvider locale={LOCALE_MAP[preference.locale]}>{children}</ConfigProvider>
    </UiContext.Provider>
  )
}
```

If `en_GB` import path differs in installed version, resolve from `node_modules/@douyinfe/semi-ui-19/lib/es/locale/source/` and adjust.

- [ ] **Step 2: ThemeToggle**

```tsx
import { Switch } from '@douyinfe/semi-ui-19'
import { IconMoon, IconSun } from '@douyinfe/semi-icons'
import { useUiPreference } from './SemiAppProvider'

export function ThemeToggle() {
  const { preference, setTheme } = useUiPreference()
  const dark = preference.theme === 'dark'
  return (
    <Switch
      checked={dark}
      onChange={(checked) => setTheme(checked ? 'dark' : 'light')}
      checkedText={<IconMoon />}
      uncheckedText={<IconSun />}
      aria-label="切换深色模式"
    />
  )
}
```

- [ ] **Step 3: Wire main + html**

`web/src/main.tsx`:

```tsx
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { SemiAppProvider } from './components/SemiAppProvider'
import App from './App'
import '@douyinfe/semi-ui-19/dist/css/semi.css'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <SemiAppProvider>
      <App />
    </SemiAppProvider>
  </StrictMode>,
)
```

`web/index.html`: set `<html lang="zh-CN">`, remove `data-theme="roundpen"`, body class → empty or `min-h-screen` only (no Daisy tokens).

- [ ] **Step 4: Build**

Run: `cd web && npm run build`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/SemiAppProvider.tsx web/src/components/ThemeToggle.tsx web/src/main.tsx web/index.html
git commit -m "feat(web): Semi ConfigProvider with theme/locale preference"
```

---

### Task 4: Login page → Semi Form

**Files:**
- Modify: `web/src/pages/LoginPage.tsx`
- Modify: `web/e2e/login.spec.ts` and `web/e2e/helpers.ts` if labels change

- [ ] **Step 1: Rewrite LoginPage**

Keep `doLogin` / auth redirect behavior. Replace Daisy markup with:

```tsx
import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Banner, Button, Form, Typography } from '@douyinfe/semi-ui-19'
import { doLogin, useAuth } from '../auth'

export function LoginPage() {
  const auth = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const from =
    (location.state as { from?: string } | null)?.from &&
    (location.state as { from: string }).from !== '/login'
      ? (location.state as { from: string }).from
      : '/'

  const [user, setUser] = useState('admin')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  if (auth.status === 'ok') {
    return <Navigate to={from} replace />
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await doLogin(user, password)
      navigate(from, { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'login failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div
      style={{
        minHeight: '100%',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 24,
        background: 'var(--semi-color-bg-0)',
      }}
    >
      <div style={{ width: '100%', maxWidth: 360 }}>
        <Typography.Title heading={2} style={{ textAlign: 'center' }}>
          Roundpen
        </Typography.Title>
        <Typography.Text type="tertiary" style={{ display: 'block', textAlign: 'center', marginBottom: 24 }}>
          Sign in to your agent sandbox
        </Typography.Text>
        <form onSubmit={(e) => void onSubmit(e)}>
          <Form.Input
            field="user"
            label="Username or email"
            pure
            value={user}
            onChange={setUser}
            autoComplete="username"
            style={{ width: '100%', marginBottom: 16 }}
          />
          {/* If Form.Input uncontrolled API fights controlled mode, use Input + label instead */}
          <Form.Input
            field="password"
            label="Password"
            mode="password"
            pure
            value={password}
            onChange={setPassword}
            autoComplete="current-password"
            style={{ width: '100%', marginBottom: 16 }}
          />
          {error && (
            <Banner fullMode={false} type="danger" description={error} closeIcon={null} style={{ marginBottom: 16 }} />
          )}
          <Button htmlType="submit" theme="solid" type="primary" block loading={busy}>
            Sign in
          </Button>
        </form>
      </div>
    </div>
  )
}
```

**Important for e2e:** Keep accessible name `Username or email`, `Password`, button `Sign in`, and error in an element with `role="alert"` (Banner may need `role="alert"` wrapper if Semi does not).

If `Form.Input` controlled API is awkward, use:

```tsx
import { Input } from '@douyinfe/semi-ui-19'
// ...
<label>
  <span>Username or email</span>
  <Input value={user} onChange={setUser} aria-label="Username or email" />
</label>
```

- [ ] **Step 2: Build + fix e2e labels if needed**

Run: `cd web && npm run build`

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/LoginPage.tsx web/e2e/login.spec.ts web/e2e/helpers.ts
git commit -m "feat(web): rewrite login with Semi Form"
```

---

### Task 5: PageShell + ChangePasswordDialog

**Files:**
- Modify: `web/src/components/PageShell.tsx`
- Modify: `web/src/components/ChangePasswordDialog.tsx`

- [ ] **Step 1: Rewrite PageShell with Semi Layout/Nav**

Use `Layout`, `Nav`, `Button`, `Dropdown`, `Typography` from `@douyinfe/semi-ui-19`. Mount `<ThemeToggle />` in the header next to account actions. Preserve `NAV` export and `admin`/`advanced` filtering logic from the current file. Use `Link`/`NavLink` via `Nav.Item` `itemKey` + `useNavigate`, or wrap labels with `react-router-dom` `Link`.

Sketch:

```tsx
import { Layout, Nav, Button, Typography, Dropdown } from '@douyinfe/semi-ui-19'
import { ThemeToggle } from './ThemeToggle'
// keep NAV, auth, ChangePasswordDialog wiring
```

Header right: username, ThemeToggle, Change password, Sign out.

- [ ] **Step 2: ChangePasswordDialog → Modal + Form**

Replace Daisy modal classes with Semi `Modal` + `Input`/`Form` + `Toast` on success. Keep API call behavior identical.

- [ ] **Step 3: Build + commit**

```bash
cd web && npm run build
git add web/src/components/PageShell.tsx web/src/components/ChangePasswordDialog.tsx
git commit -m "feat(web): Semi PageShell and change-password modal"
```

---

### Task 6: AssistantLayout shell

**Files:**
- Modify: `web/src/components/AssistantLayout.tsx`

- [ ] **Step 1: Replace Daisy sidebar/header with Semi `Layout` + `Nav` + `List`/`Button`/`Badge`/`SideSheet` (mobile)**

Preserve:
- assistants list fetch / refresh context
- pending tickets popover/sheet
- collapse state key `roundpen.assistants.sidebarCollapsed`
- routes via `Outlet`

Use Semi icons for collapse/menu. Include `<ThemeToggle />` in header.

- [ ] **Step 2: Build + commit**

```bash
cd web && npm run build
git add web/src/components/AssistantLayout.tsx
git commit -m "feat(web): Semi AssistantLayout shell"
```

---

### Task 7: Standard admin/assistant pages

**Files (rewrite each to Semi; no Daisy classes):**
- `web/src/pages/SettingsPage.tsx`
- `web/src/pages/TemplatesPage.tsx`
- `web/src/pages/SandboxesPage.tsx`
- `web/src/pages/BrowserPage.tsx`
- `web/src/pages/AssistantCreatePage.tsx`
- `web/src/pages/AssistantDetailPage.tsx`
- `web/src/components/SetupWorkstation.tsx`
- Dialog components under `web/src/components/*Dialog.tsx` / `*Panel.tsx` that still use Daisy (`Sandbox*`, `Template*`, `GitCredentialsPanel`, `RuntimePanel`)

- [ ] **Step 1: Migrate SettingsPage**

Replace selects/inputs/buttons with Semi `Form`/`Select`/`Input`/`Button`/`Tabs`/`Banner`/`Toast`. Keep API payloads identical. Prefer Semi `Toast` over ad-hoc error divs where appropriate.

- [ ] **Step 2: Migrate Templates + Sandboxes + their dialogs**

`Table` for lists, `Modal` for create/edit/build, `Popconfirm` for destructive actions, `Tag` for status.

- [ ] **Step 3: Migrate BrowserPage chrome**

Keep VNC/screenshot behavior; toolbar → Semi `Button`/`Spin`/`Banner`.

- [ ] **Step 4: Migrate AssistantCreatePage + SetupWorkstation + AssistantDetailPage**

`Steps` for wizard; `Input`/`TextArea`/`RadioGroup`/`Switch` for fields; SetupWorkstation action list → Semi `List`/`Collapse`/`Progress`/`Button`.

- [ ] **Step 5: Build after each logical group; commit in 2–3 commits if diff is huge**

Example commits:

```bash
git commit -m "feat(web): Semi Settings and registry/sandbox pages"
git commit -m "feat(web): Semi assistant create/detail and setup UI"
```

Run: `cd web && npm run build` after each group. Expected: PASS.

---

### Task 8: Chat message adapter (TDD)

**Files:**
- Create: `web/src/lib/semiChatAdapter.ts`
- Create: `web/src/lib/semiChatAdapter.test.ts`

Semi `Message` shape (minimal fields used):

```ts
export type SemiChatMessage = {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string | SemiContentItem[]
  status?: 'completed' | 'in_progress' | 'failed' | 'cancelled' | 'queued' | 'incomplete'
  createAt?: number
}

export type SemiContentItem =
  | { type: 'text' | 'input_text' | 'output_text'; text: string }
  | { type: 'reasoning'; summary: { type: 'summary_text'; text: string }[] }
  | {
      type: 'tool_call'
      id?: string
      name?: string
      input?: unknown
      output?: unknown
      status?: string
    }
```

Adjust `type` strings to match the installed Semi AIChatDialogue typings (`node_modules/@douyinfe/semi-ui-19` AI chat types). Prefer official content types; if `tool_call` type name differs, use the official one or `renderDialogueContentItem`.

- [ ] **Step 1: Write failing tests**

```ts
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { agentMessageToSemi, chatLinesToSemi } from './semiChatAdapter.ts'
import type { AgentMessage } from '../api.ts'

describe('agentMessageToSemi', () => {
  it('maps user text', () => {
    const m: AgentMessage = {
      id: '1',
      sessionId: 's',
      role: 'user',
      content: 'hello',
      createdAt: '2026-01-01T00:00:00Z',
    }
    const out = agentMessageToSemi(m)
    assert.equal(out.role, 'user')
    assert.equal(out.id, '1')
    assert.equal(out.content, 'hello')
  })

  it('maps assistant streaming to in_progress', () => {
    const m: AgentMessage = {
      id: '2',
      sessionId: 's',
      role: 'assistant',
      content: 'partial',
      meta: { status: 'in_progress' },
      createdAt: '2026-01-01T00:00:00Z',
    }
    const out = agentMessageToSemi(m)
    assert.equal(out.role, 'assistant')
    assert.equal(out.status, 'in_progress')
  })

  it('maps thought to reasoning content', () => {
    const m: AgentMessage = {
      id: '3',
      sessionId: 's',
      role: 'assistant',
      content: 'thinking…',
      meta: { type: 'thought' },
      createdAt: '2026-01-01T00:00:00Z',
    }
    const out = agentMessageToSemi(m)
    assert.ok(Array.isArray(out.content))
  })

  it('maps tool_call meta into tool content or degraded text', () => {
    const m: AgentMessage = {
      id: '4',
      sessionId: 's',
      role: 'tool',
      content: '',
      meta: {
        type: 'tool_call',
        toolId: 't1',
        title: 'Bash',
        status: 'completed',
        input: { cmd: 'ls' },
        output: 'ok',
      },
      createdAt: '2026-01-01T00:00:00Z',
    }
    const out = agentMessageToSemi(m)
    assert.equal(out.role, 'assistant')
    assert.ok(out.content)
  })

  it('degrades unknown types to system text', () => {
    const m: AgentMessage = {
      id: '5',
      sessionId: 's',
      role: 'system',
      content: 'notice',
      meta: { type: 'event' },
      createdAt: '2026-01-01T00:00:00Z',
    }
    const out = agentMessageToSemi(m)
    assert.equal(out.role, 'system')
    assert.equal(out.content, 'notice')
  })
})
```

- [ ] **Step 2: Run — expect FAIL**

Run: `cd web && node --test --experimental-strip-types src/lib/semiChatAdapter.test.ts`

- [ ] **Step 3: Implement adapter**

Implement `agentMessageToSemi` and `chatLinesToSemi` (if ChatSessionPage still uses intermediate `ChatLine`, either map `AgentMessage[]` directly or keep a thin `ChatLine`→Semi mapper). Unknown/permission types: `role: 'system'` text or leave permission handling to Modal outside the dialogue.

- [ ] **Step 4: Tests PASS + commit**

```bash
git add web/src/lib/semiChatAdapter.ts web/src/lib/semiChatAdapter.test.ts
git commit -m "feat(web): ACP to Semi AIChat message adapter"
```

---

### Task 9: ChatSessionPage → AIChatDialogue + AIChatInput

**Files:**
- Modify: `web/src/pages/ChatSessionPage.tsx`
- Delete when unused: `web/src/components/ChatComposer.tsx`, `web/src/components/ToolCallCard.tsx` (only if no remaining imports)
- Optionally keep `MarkdownBody.tsx` only if still needed outside AIChat

- [ ] **Step 1: Replace message list + composer**

Keep existing WS / send / cancel / permission resolution logic. State for display becomes Semi `chats: SemiChatMessage[]` derived via adapter whenever messages update.

UI core:

```tsx
import { AIChatDialogue, AIChatInput, Modal, Button } from '@douyinfe/semi-ui-19'
import { agentMessageToSemi } from '../lib/semiChatAdapter'

// chats = messages.map(agentMessageToSemi)  (or incremental update helper)
// <AIChatDialogue chats={chats} onChatsChange={setChats} align="left-right" />
// <AIChatInput onSend={(content) => void sendFromInput(content)} />
```

Map `AIChatInput` send payload → existing plain-text send API (extract text parts). Permission prompts → Semi `Modal` with allow/reject calling existing handlers.

Use Semi MCP / installed typings for exact prop names (`chats` vs `messages`, etc.).

- [ ] **Step 2: Build**

Run: `cd web && npm run build`  
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/ChatSessionPage.tsx web/src/components/
git commit -m "feat(web): chat UI on Semi AIChatDialogue and AIChatInput"
```

---

### Task 10: FileTree → Semi Tree; Workbench/Terminal/VNC shells

**Files:**
- Modify: `web/src/components/FileTree.tsx`
- Modify: `web/src/pages/WorkbenchPage.tsx`
- Modify: `web/src/components/TerminalPane.tsx`
- Modify: `web/src/components/AgentBrowserPanel.tsx`
- Modify: `web/src/components/ChatLayout.tsx` if still referenced

- [ ] **Step 1: FileTree with Semi Tree**

Keep `files.list` API. Convert `DirEntry[]` to Tree `treeData` with `key`/`label`/`isLeaf`. Lazy-load directories via `onLoadData` if using nested paths; or flat current-directory model matching today’s UX (parent `..` + entries) mapped into Tree nodes.

Refresh: Semi `Button` + `IconRefresh`.

- [ ] **Step 2: TerminalPane / AgentBrowserPanel chrome**

Keep xterm and noVNC. Wrap headers/toolbars in Semi `Button`/`Spin`/`Typography`/`Tabs`. Remove Daisy `btn` classes.

- [ ] **Step 3: WorkbenchPage layout**

Semi `Layout`/`Tabs`/`Card` for panes; compose FileTree + Terminal + editor/preview as today.

- [ ] **Step 4: Build + commit**

```bash
cd web && npm run build
git add web/src/components/FileTree.tsx web/src/components/TerminalPane.tsx web/src/components/AgentBrowserPanel.tsx web/src/pages/WorkbenchPage.tsx
git commit -m "feat(web): Semi Tree and workbench/terminal/VNC chrome"
```

---

### Task 11: Remove DaisyUI + Tailwind (hard cut)

**Files:**
- Modify: `web/package.json` — remove `daisyui`, `tailwindcss`, `@tailwindcss/vite`
- Modify: `web/vite.config.ts` — remove tailwind plugin import/usage
- Replace: `web/src/index.css` with minimal CSS only
- Grep purge: any remaining `btn `, `input-bordered`, `bg-base-`, `className=.*daisy`, Tailwind utility-only layouts that break without Tailwind — convert to inline style or Semi `style`/`className` using Semi CSS variables (`var(--semi-color-bg-0)` etc.)

- [ ] **Step 1: Grep for leftovers**

Run:

```bash
cd web && rg -n "btn |daisy|input-bordered|bg-base-|@tailwind|@plugin \"daisyui\"|tailwindcss" src e2e vite.config.ts package.json index.html || true
```

Expected: only hits you are about to delete; fix any remaining component usages first.

- [ ] **Step 2: Minimal `index.css`**

```css
html,
body,
#root {
  height: 100%;
  height: 100dvh;
  overflow-x: hidden;
}

html {
  text-size-adjust: 100%;
  -webkit-text-size-adjust: 100%;
}

body {
  margin: 0;
  background: var(--semi-color-bg-0);
  color: var(--semi-color-text-0);
}

body.chat-lock {
  overflow: hidden;
  overscroll-behavior: none;
}

input,
select,
textarea,
button {
  touch-action: manipulation;
}
```

- [ ] **Step 3: Uninstall**

```bash
cd web && npm uninstall daisyui tailwindcss @tailwindcss/vite
```

Update `vite.config.ts`:

```ts
import react from '@vitejs/plugin-react'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'

const root = dirname(fileURLToPath(import.meta.url))
const apiProxy = process.env.ROUNDPEN_API_PROXY || 'http://127.0.0.1:19001'

export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 19000,
    proxy: {
      '/v1': { target: apiProxy, ws: true },
      '/p': apiProxy,
      '/health': apiProxy,
    },
  },
  build: {
    outDir: '../internal/ui/dist',
    emptyOutDir: true,
    rollupOptions: {
      input: {
        main: resolve(root, 'index.html'),
        vnc: resolve(root, 'vnc.html'),
      },
    },
  },
})
```

- [ ] **Step 4: Build must pass with zero Tailwind**

Run: `cd web && npm run build`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/package.json web/package-lock.json web/vite.config.ts web/src/index.css web/src web/index.html
git commit -m "chore(web): remove DaisyUI and Tailwind after Semi migration"
```

---

### Task 12: E2E + acceptance

**Files:**
- Modify: `web/e2e/*.ts` as needed for Semi roles/text
- Add `data-testid` on critical controls if Semi DOM is unstable (login submit, theme toggle, assistant create)

- [ ] **Step 1: Unit tests**

Run:

```bash
cd web && node --test --experimental-strip-types src/lib/uiPreference.test.ts src/lib/semiChatAdapter.test.ts
```

Expected: PASS.

- [ ] **Step 2: Build**

Run: `cd web && npm run build`  
Expected: PASS.

- [ ] **Step 3: E2E (against smoke stack per Makefile)**

Run from repo root: `make test-e2e` (or project’s documented Playwright target).

Fix failures by adjusting selectors — prefer `getByRole` / `getByLabel` / `data-testid`.

- [ ] **Step 4: Manual acceptance checklist**

1. No Daisy/Tailwind in `web/package.json`
2. Theme toggle flips dark/light and survives refresh
3. Default Semi zh_CN strings on components (DatePicker etc. if present)
4. Chat uses AIChatDialogue + AIChatInput; files use Tree; terminal/VNC have Semi chrome
5. Login → assistant → send message path works

- [ ] **Step 5: Final commit if e2e fixes remain**

```bash
git add web/e2e web/src
git commit -m "test(web): align e2e with Semi Design UI"
```

---

## Self-review (vs spec)

| Spec item | Task |
|-----------|------|
| Add semi-ui / icons (React 19 → semi-ui-19) | Task 1 |
| ConfigProvider + zh_CN + locale reserved | Task 3 |
| Theme toggle + persistence | Tasks 2–3, 5–6 |
| PageShell / AssistantLayout Semi | Tasks 5–6 |
| Standard pages Semi | Task 7 |
| AIChatDialogue + AIChatInput + adapter | Tasks 8–9 |
| Tree file browser | Task 10 |
| Terminal/VNC shells | Task 10 |
| Remove Daisy/Tailwind | Task 11 |
| E2E + acceptance | Task 12 |
| MCP config | Already committed in `.cursor/mcp.json` |

No intentional dual-stack product strategy; Daisy may remain in package.json only until Task 11.
