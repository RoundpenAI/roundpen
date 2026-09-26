import { Extension } from '@tiptap/core'
import type { Editor } from '@tiptap/core'

/**
 * Bridge to the composer's tiptap editor. Semi's AIChatInput exposes no
 * "clear content" API, so the editor instance is captured from an injected
 * extension (the `extensions` prop is spread into the editor's extension
 * list). Remounts (dev StrictMode, generating-state rebuilds) can leave more
 * than one instance around, so the slot refreshes on every interaction — by
 * the time the custom send key is clicked, it points at the editor that has
 * the typed content.
 */
const state: { editor: Editor | null } = { editor: null }

export function composerEditorBridge() {
  return Extension.create({
    name: 'roundpenComposerBridge',
    onCreate() {
      state.editor = this.editor
    },
    onUpdate() {
      state.editor = this.editor
    },
    onSelectionUpdate() {
      state.editor = this.editor
    },
  })
}

/** Clear the composer, emitting an update so onContentChange fires. */
export function clearComposer() {
  const editor = state.editor
  if (!editor) return
  try {
    editor.commands.clearContent(true)
  } catch {
    /* the captured editor can be a replaced instance mid-remount */
  }
}
