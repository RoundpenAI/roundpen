import { Loading } from '../../components/Loading'
import { ItemListSection } from './items/ItemListSection'
import type { SectionBase } from './helpers'

// Proxy profiles are setting items (kind "proxy"): each saves on its own, so
// this section ignores the surrounding settings form and save bar.
export function ProxySection({ isAdmin, loading, t }: SectionBase) {
  if (!isAdmin) return null
  if (loading) return <Loading tip={t('settings.loadingSystem')} />
  return (
    <ItemListSection
      kind="proxy"
      introKey="settings.proxy.intro"
      noteKey="settings.proxy.note"
      t={t}
    />
  )
}
