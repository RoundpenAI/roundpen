import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { RequireAuth } from './auth'
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
import { SettingsPage } from './pages/SettingsPage'
import { TemplatesPage } from './pages/TemplatesPage'
import { WorkbenchPage } from './pages/WorkbenchPage'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/" element={<Navigate to="/a" replace />} />
        <Route
          path="/a"
          element={
            <RequireAuth>
              <AssistantLayout />
            </RequireAuth>
          }
        >
          <Route index element={<AssistantsIndexRedirect />} />
          <Route path="new" element={<AssistantCreatePage />} />
          <Route path=":assistantId" element={<AssistantDetailPage />} />
          <Route path=":assistantId/chat" element={<AssistantChatRedirect />} />
          <Route path=":assistantId/s/:id" element={<ChatSessionPage />} />
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
          path="/registry"
          element={
            <RequireAuth>
              <TemplatesPage />
            </RequireAuth>
          }
        />
        <Route
          path="/settings"
          element={
            <RequireAuth>
              <SettingsPage />
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
