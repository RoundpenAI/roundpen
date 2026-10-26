import { meWorkspace } from '../api'

/**
 * Trigger browser downloads for workspace-relative file paths.
 * Uses same-origin <a download> clicks (cookie auth) instead of window.open,
 * which popup blockers often swallow for Content-Disposition: attachment.
 */
export function downloadWorkspacePaths(paths: string[]): void {
  const unique = [...new Set(paths.map((p) => p.trim()).filter(Boolean))]
  for (const path of unique) {
    const a = document.createElement('a')
    a.href = meWorkspace.downloadUrl(path)
    a.download = path.split('/').pop() || 'download'
    a.rel = 'noopener'
    a.style.display = 'none'
    document.body.appendChild(a)
    a.click()
    a.remove()
  }
}
