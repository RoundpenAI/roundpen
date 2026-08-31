import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { RequireAuth } from './auth'
import { LoginPage } from './pages/LoginPage'
import { SandboxesPage } from './pages/SandboxesPage'
import { TemplatesPage } from './pages/TemplatesPage'
import { WorkbenchPage } from './pages/WorkbenchPage'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          path="/"
          element={
            <RequireAuth>
              <SandboxesPage />
            </RequireAuth>
          }
        />
        <Route
          path="/templates"
          element={
            <RequireAuth>
              <TemplatesPage />
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
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  )
}
