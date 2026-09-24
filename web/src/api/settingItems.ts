import { api } from './client'

export type ItemKind = 'llm' | 'proxy' | 'search' | 'browser'

export type FieldType =
  | 'string'
  | 'secret'
  | 'url'
  | 'int'
  | 'bool'
  | 'enum'
  | 'json'
  | 'itemRef'

/** One config key of a kind, as declared by the backend registry. */
export type ItemField = {
  key: string
  type: FieldType
  label?: string
  hint?: string
  required?: boolean
  options?: string[]
  refKind?: ItemKind
  advanced?: boolean
}

export type KindDef = {
  kind: ItemKind
  name: string
  fields: ItemField[]
  selectable?: string[]
}

/** A usage site that selects one item of a kind. */
export type SlotDef = {
  key: string
  kind: ItemKind
  name: string
  description?: string
  protocols?: string[]
  bindingFields?: ItemField[]
  userOverride: boolean
  rebuildsEnv?: boolean
  defaultItem?: string
}

/** A named, typed configuration entry within a kind. */
export type SettingItem = {
  kind: ItemKind
  id: string
  name: string
  description?: string
  enabled: boolean
  position: number
  config: Record<string, unknown>
  updatedAt?: string
}

export type SettingBinding = {
  slot: string
  kind: ItemKind
  itemId?: string
  params?: Record<string, unknown>
}

export type UserBindingEntry = SettingBinding & {
  effectiveItemId?: string
  effectiveParams?: Record<string, unknown>
  source?: string
}

export type SetBindingResult = {
  slot: string
  itemId: string
  status?: string
  environment?: unknown
  rebuildError?: string
}

export const settingItems = {
  schema: () => api<{ kinds: KindDef[]; slots: SlotDef[] }>('/v1/setting-schema'),

  list: (kind?: ItemKind) =>
    api<{ items: SettingItem[] }>(
      `/v1/admin/setting-items${kind ? `?kind=${encodeURIComponent(kind)}` : ''}`,
    ),
  save: (item: SettingItem) =>
    api<{ item: SettingItem }>(
      `/v1/admin/setting-items/${encodeURIComponent(item.kind)}/${encodeURIComponent(item.id)}`,
      { method: 'PUT', body: JSON.stringify(item) },
    ),
  remove: (kind: ItemKind, id: string, force = false) =>
    api<void>(
      `/v1/admin/setting-items/${encodeURIComponent(kind)}/${encodeURIComponent(id)}${
        force ? '?force=true' : ''
      }`,
      { method: 'DELETE' },
    ),
  reorder: (kind: ItemKind, ids: string[]) =>
    api<{ kind: ItemKind; ids: string[] }>('/v1/admin/setting-items/order', {
      method: 'PUT',
      body: JSON.stringify({ kind, ids }),
    }),

  bindings: () =>
    api<{ bindings: SettingBinding[]; slots: SlotDef[]; kinds: KindDef[] }>(
      '/v1/admin/setting-bindings',
    ),
  setBinding: (slot: string, itemId: string, params?: Record<string, unknown>) =>
    api<SetBindingResult>(`/v1/admin/setting-bindings/${encodeURIComponent(slot)}`, {
      method: 'PUT',
      body: JSON.stringify({ itemId, params }),
    }),
  clearBinding: (slot: string) =>
    api<void>(`/v1/admin/setting-bindings/${encodeURIComponent(slot)}`, { method: 'DELETE' }),

  userItems: (kind: ItemKind) =>
    api<{ items: SettingItem[] }>(`/v1/me/setting-items?kind=${encodeURIComponent(kind)}`),
  userBindings: () =>
    api<{ slots: Record<string, UserBindingEntry>; kinds: KindDef[] }>(
      '/v1/me/setting-bindings',
    ),
  setUserBinding: (slot: string, itemId: string, params?: Record<string, unknown>) =>
    api<SetBindingResult>(`/v1/me/setting-bindings/${encodeURIComponent(slot)}`, {
      method: 'PUT',
      body: JSON.stringify({ itemId, params }),
    }),
  clearUserBinding: (slot: string) =>
    api<SetBindingResult>(`/v1/me/setting-bindings/${encodeURIComponent(slot)}`, {
      method: 'DELETE',
    }),
}

/** A pickable item as shown to a user (never carries secrets). */
export type SlotChoice = {
  id: string
  name: string
  description?: string
}

/**
 * Per-user slot selection: what a user can pick, what they picked, and how to
 * change it. Shared by the environment panels so every kind gets the same
 * behavior (rebuild-on-change included, handled by the server).
 */
export const slotChoices = {
  options: async (kind: ItemKind): Promise<SlotChoice[]> => {
    const res = await settingItems.userItems(kind)
    return (res.items ?? []).map((item) => ({
      id: item.id,
      name: item.name,
      description: item.description,
    }))
  },
  current: async (slot: string): Promise<string> => {
    const res = await settingItems.userBindings()
    return res.slots?.[slot]?.itemId ?? ''
  },
  select: (slot: string, itemId: string) => settingItems.setUserBinding(slot, itemId),
}
