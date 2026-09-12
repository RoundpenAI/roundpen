import { Spin } from '@douyinfe/semi-ui-19'

// Semi's Spin without children is a fixed 20x20 box with the `tip` rendered
// inside it, so CJK tips wrap one character per line (and long English words
// overflow). Keep the spinner and the tip as siblings instead.
export function Loading({
  tip,
  size = 'middle',
}: {
  tip?: string
  size?: 'small' | 'middle' | 'large'
}) {
  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        gap: 8,
      }}
    >
      <Spin size={size} />
      {tip ? (
        <div style={{ color: 'var(--semi-color-primary)', fontSize: 14 }}>
          {tip}
        </div>
      ) : null}
    </div>
  )
}
