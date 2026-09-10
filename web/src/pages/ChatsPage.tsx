import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { agents, ApiError } from '../api'
import { ChatComposer, ChatComposerDock } from '../components/ChatComposer'
import { useChatLayout } from '../components/ChatLayout'

function titleFrom(text: string): string {
  const line = text.trim().split(/\n/, 1)[0] ?? ''
  return line.length > 48 ? `${line.slice(0, 45)}…` : line || 'New chat'
}

export function ChatsPage() {
  const navigate = useNavigate()
  const { providers, refresh } = useChatLayout()
  const [providerId, setProviderId] = useState('sysadmin')
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (providers.length && !providers.some((p) => p.id === providerId)) {
      setProviderId(providers[0].id)
    }
  }, [providers, providerId])

  const start = async () => {
    const text = input.trim()
    if (!text || busy) return
    setBusy(true)
    setError(null)
    try {
      const sess = await agents.createSession({
        providerId,
        title: titleFrom(text),
      })
      await refresh()
      try {
        sessionStorage.setItem(`roundpen.pendingPrompt.${sess.id}`, text)
      } catch {
        /* ignore */
      }
      navigate(`/chats/${sess.id}`, { state: { pendingPrompt: text } })
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
      setBusy(false)
    }
  }

  return (
    <div className="chat-pane chat-landing">
      <div className="chat-pane-scroll chat-landing-body">
        <div className="max-w-xl text-center">
          <h1 className="font-display text-3xl font-semibold tracking-tight sm:text-4xl">
            What are we working on?
          </h1>
          <p className="mt-2 text-sm opacity-50">
            Ask the agent. Sessions stay in the sidebar — start typing to open a
            new one.
          </p>
        </div>
      </div>

      <ChatComposerDock>
        <div className="chat-composer-dock-inner">
          {error && (
            <p className="mb-3 text-center text-sm text-error" role="alert">
              {error}
            </p>
          )}
          <ChatComposer
            value={input}
            onChange={setInput}
            onSend={() => void start()}
            busy={busy}
            disabled={!providers.length}
            placeholder="Message the agent…"
            leading={
              <div className="flex items-center gap-2 px-3 pt-2.5">
                <label className="flex items-center gap-2 text-xs opacity-60">
                  Agent
                  <select
                    className="select select-bordered select-sm text-base"
                    value={providerId}
                    onChange={(e) => setProviderId(e.target.value)}
                    disabled={busy}
                  >
                    {providers.map((p) => (
                      <option key={p.id} value={p.id} disabled={!p.enabled}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                </label>
              </div>
            }
          />
        </div>
      </ChatComposerDock>
    </div>
  )
}
