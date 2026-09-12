import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { RequireAuth } from './auth'
import { AppShell } from './components/AppShell'
import {
  AssistantLayout,
  AssistantsIndexRedirect,
} from './components/AssistantLayout'
import { AssistantChatRedirect } from './pages/AssistantChatRedirect'
import { AssistantCreatePage } from './pages/AssistantCreatePage'
import { AssistantDetailPage } from './pages/AssistantDetailPage'
import { BrowserPage } from './pages/BrowserPage'
import { ChatSessionPage } from './pages/ChatSessionPage'
import { LoginPage } from './pages/LoginPage'
import { SettingsLayout } from './pages/SettingsLayout'
import { SettingsPage } from './pages/SettingsPage'
import { TemplatesPage } from './pages/TemplatesPage'
import { WorkbenchPage } from './pages/WorkbenchPage'
import { WorkspacePage } from './pages/WorkspacePage'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/" element={<Navigate to="/a" replace />} />
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
            <Route
              path=":assistantId/chat"
              element={<AssistantChatRedirect />}
            />
            <Route path=":assistantId/s/:id" element={<ChatSessionPage />} />
          </Route>
          <Route path="/settings" element={<SettingsLayout />}>
            <Route index element={<Navigate to="runtime" replace />} />
            <Route path=":section" element={<SettingsPage />} />
          </Route>
          <Route path="/workspace" element={<WorkspacePage />} />
          <Route path="/registry" element={<TemplatesPage />} />
        </Route>
        <Route path="/chats" element={<Navigate to="/a" replace />} />
        <Route path="/chats/*" element={<Navigate to="/a" replace />} />
        <Route
          path="/browser"
          element={
            <RequireAuth>
              <BrowserPage />
            </RequireAuth>
          }
        />
        <Route
          path="/s/:id"
          element={
            <RequireAuth>
              <WorkbenchPage />
            </RequireAuth>
          }
        />
        <Route path="*" element={<Navigate to="/a" replace />} />
      </Routes>
    </BrowserRouter>
  )
}
