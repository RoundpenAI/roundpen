import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

type Props = {
  text: string
  className?: string
}

function toHref(url: string): string {
  if (/^https?:\/\//i.test(url)) return url
  return `http://${url}`
}

/**
 * LLM replies sometimes emit HTML breaks/entities instead of Markdown.
 * Also common: **http://example.com** which GFM autolink mangles into one huge URL.
 */
function normalizeMarkdown(text: string): string {
  return (
    text
      .replace(/<br\s*\/?>/gi, '\n\n')
      .replace(/&nbsp;/gi, '\u00a0')
      .replace(/&amp;/gi, '&')
      .replace(/&lt;/gi, '<')
      .replace(/&gt;/gi, '>')
      .replace(/&quot;/gi, '"')
      // **https://…** / **www.…** / **example.com** → bold markdown link (avoid autolink eating **)
      .replace(
        /\*\*((?:https?:\/\/|www\.)[^\s*]+|(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}(?:\/[^\s*]*)?)\*\*/gi,
        (_m, raw: string) => {
          const url = raw.replace(/[.,;:!?，。；：！？、]+$/u, '')
          return `**[${url}](${toHref(url)})**`
        },
      )
  )
}

/** Trim href when autolink already absorbed markdown/CJK (defensive). */
function sanitizeHref(href: string | undefined): string | undefined {
  if (!href) return href
  let h = href
  const star = h.indexOf('**')
  if (star >= 0) h = h.slice(0, star)
  h = h.replace(/[。，；：！？、].*$/u, '')
  return h || href
}

/** Renders chat content as Markdown (GFM). Plain text still looks fine. */
export function MarkdownBody({ text, className = '' }: Props) {
  const source = normalizeMarkdown(text)
  return (
    <div className={`md-body ${className}`.trim()}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: ({ href, children }) => {
            const safe = sanitizeHref(href)
            return (
              <a href={safe} target="_blank" rel="noreferrer noopener">
                {children}
              </a>
            )
          },
          // Avoid huge margins from default browser styles inside tight chat bubbles.
          p: ({ children }) => <p className="md-p">{children}</p>,
          pre: ({ children }) => <pre className="md-pre">{children}</pre>,
          code: ({ className: codeClass, children, ...props }) => {
            const inline = !codeClass
            if (inline) {
              return (
                <code className="md-code-inline" {...props}>
                  {children}
                </code>
              )
            }
            return (
              <code className={codeClass} {...props}>
                {children}
              </code>
            )
          },
          strong: ({ children }) => <strong>{children}</strong>,
        }}
      >
        {source}
      </ReactMarkdown>
    </div>
  )
}
