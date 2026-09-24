import type { ItemField, ItemKind, SlotDef } from '../api'
import type { MessageKey } from '../i18n'

type Label = (key: MessageKey, vars?: Record<string, string | number>) => string

// The backend sends English names, descriptions and hints so a new kind works
// without a frontend release. These helpers prefer a catalog entry and fall
// back to the backend text, which keeps the console in one language without
// duplicating every field definition in TypeScript.
function translate(key: string, fallback: string, t: Label): string {
  const asKey = key as MessageKey
  const translated = t(asKey)
  return translated === asKey ? fallback : translated
}

export function slotLabel(slot: SlotDef, t: Label): string {
  return translate(`settings.slotName.${slot.key}`, slot.name, t)
}

export function slotDescription(slot: SlotDef, t: Label): string {
  return translate(`settings.slotDesc.${slot.key}`, slot.description ?? '', t)
}

export function fieldLabel(kind: ItemKind, field: ItemField, t: Label): string {
  const shared = translate(`settings.itemField.${field.key}`, field.label ?? field.key, t)
  return translate(`settings.itemField.${kind}.${field.key}`, shared, t)
}

export function fieldHint(kind: ItemKind, field: ItemField, t: Label): string {
  return translate(`settings.itemFieldHint.${kind}.${field.key}`, field.hint ?? '', t)
}

/** Display label of one enum option; falls back to the raw option id. */
export function optionLabel(kind: ItemKind, field: ItemField, option: string, t: Label): string {
  const shared = translate(`settings.itemOption.${field.key}.${option}`, option, t)
  return translate(`settings.itemOption.${kind}.${field.key}.${option}`, shared, t)
}

/** What the selected option means and what it needs; empty when unannotated. */
export function optionDescription(
  kind: ItemKind,
  field: ItemField,
  option: string,
  t: Label,
): string {
  return translate(`settings.itemOptionHint.${kind}.${field.key}.${option}`, '', t)
}
