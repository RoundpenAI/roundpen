import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { RequireAdminRoute, RequireAuth } from './auth'
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
import { IssueDetailPage } from './pages/IssueDetailPage'
import { IssuesPage } from './pages/IssuesPage'
import { LoginPage } from './pages/LoginPage'
import { RoutineDetailPage } from './pages/RoutineDetailPage'
import { SettingsLayout } from './pages/SettingsLayout'
import { AdminSettingsPage } from './pages/settings/AdminSettingsPage'
import { PersonalSettingsPage } from './pages/settings/PersonalSettingsPage'
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
              path=":assistantId/routines/:key"
              element={<RoutineDetailPage />}
            />
            <Route
              path=":assistantId/chat"
              element={<AssistantChatRedirect />}
            />
            <Route path=":assistantId/s/:id" element={<ChatSessionPage />} />
          </Route>
          <Route path="/settings" element={<SettingsLayout area="personal" />}>
            <Route index element={<Navigate to="accounts" replace />} />
            <Route path=":section" element={<PersonalSettingsPage />} />
          </Route>
          <Route
            path="/admin/settings"
            element={
              <RequireAdminRoute>
                <SettingsLayout area="admin" />
              </RequireAdminRoute>
            }
          >
            <Route index element={<Navigate to="general" replace />} />
            <Route path=":section" element={<AdminSettingsPage />} />
          </Route>
          <Route path="/workspace" element={<WorkspacePage />} />
          <Route path="/issues" element={<IssuesPage />} />
          <Route path="/issues/:key" element={<IssueDetailPage />} />
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
