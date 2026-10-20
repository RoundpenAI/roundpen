import { Navigate, useParams } from 'react-router-dom'
import { AgentEnvironmentPanel } from '../../components/AgentEnvironmentPanel'
import { ChangePasswordForm } from '../../components/ChangePasswordForm'
import { ConnectedAppsPanel } from '../../components/ConnectedAppsPanel'
import { NotificationsPanel } from '../../components/NotificationsPanel'
import { PersonalSlotsPanel } from '../../components/PersonalSlotsPanel'
import { useAuth } from '../../auth'
import { isAdminSectionKey, resolvePersonalSection } from '../../lib/appNav'
import { isPushSupported } from '../../lib/pushSupport'

export function PersonalSettingsPage() {
  const { section: sectionParam } = useParams()
  const auth = useAuth()
  const isAdmin = auth.status === 'ok' && auth.user.role === 'admin'
  const section = resolvePersonalSection(sectionParam, isPushSupported())

  // Platform sections used to live under /settings; send admins following an
  // old bookmark on to the admin area, everyone else to the default section.
  if (isAdmin && isAdminSectionKey(sectionParam)) {
    return <Navigate to={`/admin/settings/${sectionParam}`} replace />
  }

  if (sectionParam && sectionParam !== section) {
    const qs = window.location.search
    return <Navigate to={`/settings/${section}${qs}`} replace />
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
      {section === 'apps' && <ConnectedAppsPanel />}

      {section === 'password' && <ChangePasswordForm />}

      {section === 'notifications' && <NotificationsPanel />}

      {section === 'agent' && <AgentEnvironmentPanel />}

      {section === 'slots' && <PersonalSlotsPanel />}
    </div>
  )
}
