import {
  useLayoutEffect,
  useMemo,
  useRef,
  type ComponentProps,
  type RefObject,
} from 'react'
import { AIChatDialogue } from '@douyinfe/semi-ui-19'
import type { SemiChatMessage } from '../lib/semiChatAdapter'
import { groupChatTurns } from '../pages/chat/groupChatTurns'

type DialogueProps = ComponentProps<typeof AIChatDialogue>

type Props = {
  chats: SemiChatMessage[]
  scrollRootRef: RefObject<HTMLElement | null>
  roleConfig: DialogueProps['roleConfig']
  dialogueRenderConfig: DialogueProps['dialogueRenderConfig']
}

const STUCK_TOP_SLACK_PX = 2

/**
 * One AIChatDialogue per Q&A turn so sticky user questions are contained by
 * the turn box — when the turn scrolls away, the question leaves with it
 * instead of stacking under the next sticky header.
 */
export function ChatTurnList({
  chats,
  scrollRootRef,
  roleConfig,
  dialogueRenderConfig,
}: Props) {
  const turns = useMemo(() => groupChatTurns(chats), [chats])
  const listRef = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    const root = scrollRootRef.current
    const list = listRef.current
    if (!root || !list) return

    const syncStuck = () => {
      const rootTop = root.getBoundingClientRect().top
      for (const turn of list.querySelectorAll<HTMLElement>('.chat-turn')) {
        const sticky = turn.querySelector<HTMLElement>(
          '.semi-ai-chat-dialogue-wrapper:has(> .semi-ai-chat-dialogue-container-right)',
        )
        if (!sticky) {
          turn.classList.remove('is-stuck')
          continue
        }
        const rect = sticky.getBoundingClientRect()
        // Pinned when the sticky row's top sits on the scrollport top and the
        // row is still visible. Unrelated to turn preamble length.
        const stuck =
          rect.top <= rootTop + STUCK_TOP_SLACK_PX &&
          rect.bottom > rootTop + STUCK_TOP_SLACK_PX
        turn.classList.toggle('is-stuck', stuck)
      }
    }

    syncStuck()
    root.addEventListener('scroll', syncStuck, { passive: true })
    window.addEventListener('resize', syncStuck)
    return () => {
      root.removeEventListener('scroll', syncStuck)
      window.removeEventListener('resize', syncStuck)
    }
  }, [turns, scrollRootRef])

  return (
    <div ref={listRef} className="chat-turns">
      {turns.map((turn, i) => (
        <div key={turn[0]?.id ?? `turn-${i}`} className="chat-turn">
          <AIChatDialogue
            align="leftRight"
            mode="userBubble"
            chats={turn}
            roleConfig={roleConfig}
            dialogueRenderConfig={dialogueRenderConfig}
          />
        </div>
      ))}
    </div>
  )
}
