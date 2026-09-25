import { Loading } from '../../components/Loading'
import { ItemListSection } from './items/ItemListSection'
import { SlotBindings } from './items/SlotBindings'
import type { SectionBase } from './helpers'

// Web search backends are setting items (kind "search"); each saves on its
// own, so this section ignores the surrounding settings form and save bar.
export function WebtoolsSection({ isAdmin, loading, t }: SectionBase) {
  if (!isAdmin) return null
  if (loading) return <Loading tip={t('settings.loadingSystem')} />
  return (
    <>
      <ItemListSection
        kind="search"
        introKey="settings.webtools.intro"
        noteKey="settings.webtools.note"
        t={t}
      />
      <SlotBindings kind="search" t={t} />
    </>
  )
}
