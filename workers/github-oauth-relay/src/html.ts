export function browserError(title: string, detail: string): Response {
  const safe = escapeHtml
  const html = `<!doctype html>
<meta charset="utf-8">
<title>${safe(title)}</title>
<style>
  body { font: 16px/1.45 system-ui, sans-serif; margin: 3rem auto; max-width: 36rem; color: #111; }
  h1 { font-size: 1.2rem; }
  p { color: #444; }
</style>
<h1>${safe(title)}</h1>
<p>${safe(detail)}</p>
<p>Close this tab and try GitHub login again from Roundpen.</p>`
  return new Response(html, {
    status: 400,
    headers: { 'content-type': 'text/html; charset=utf-8' },
  })
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] || c,
  )
}
