import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { RequireAuth } from './auth'
import { ChatLayout } from './components/ChatLayout'
import { BrowserPage } from './pages/BrowserPage'
import { ChatSessionPage } from './pages/ChatSessionPage'
import { ChatsPage } from './pages/ChatsPage'
import { LoginPage } from './pages/LoginPage'
import { SettingsPage } from './pages/SettingsPage'
import { TemplatesPage } from './pages/TemplatesPage'
import { WorkbenchPage } from './pages/WorkbenchPage'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/" element={<Navigate to="/chats" replace />} />
        <Route
          path="/chats"
          element={
            <RequireAuth>
              <ChatLayout />
            </RequireAuth>
          }
        >
          <Route index element={<ChatsPage />} />
          <Route path=":id" element={<ChatSessionPage />} />
        </Route>
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
        <Route path="*" element={<Navigate to="/chats" replace />} />
      </Routes>
    </BrowserRouter>
  )
}
