import { Navigate, useParams } from 'react-router-dom'
import { AgentEnvironmentPanel } from '../../components/AgentEnvironmentPanel'
import { ChangePasswordForm } from '../../components/ChangePasswordForm'
import { GitCredentialsPanel } from '../../components/GitCredentialsPanel'
import { LinkedAccountsPanel } from '../../components/LinkedAccountsPanel'
import { useAuth } from '../../auth'
import { isAdminSectionKey, SETTINGS_AREAS } from '../../lib/appNav'

export function PersonalSettingsPage() {
  const { section: sectionParam } = useParams()
  const auth = useAuth()
  const isAdmin = auth.status === 'ok' && auth.user.role === 'admin'
  const section = SETTINGS_AREAS.personal.resolve(sectionParam)

  // Platform sections used to live under /settings; send admins following an
  // old bookmark on to the admin area, everyone else to the default section.
  if (isAdmin && isAdminSectionKey(sectionParam)) {
    return <Navigate to={`/admin/settings/${sectionParam}`} replace />
  }

  if (sectionParam && sectionParam !== section) {
    return <Navigate to={`/settings/${section}`} replace />
  }

  return (
    <div
      style={{
        padding: '16px 12px 24px',
        maxWidth: 768,
        margin: '0 auto',
        width: '100%',
        boxSizing: 'border-box',
      }}
    >
      {section === 'git' && <GitCredentialsPanel />}

      {section === 'accounts' && <LinkedAccountsPanel />}

      {section === 'password' && <ChangePasswordForm />}

      {section === 'agent' && <AgentEnvironmentPanel />}
    </div>
  )
}
