import type { ReactNode } from 'react'
import { Form, Switch, Typography } from '@douyinfe/semi-ui-19'

export function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: ReactNode
}) {
  return (
    <Form.Slot label={label}>
      {children}
      {hint ? (
        <Typography.Text
          type="tertiary"
          size="small"
          style={{ display: 'block', marginTop: 4, whiteSpace: 'pre-line' }}
        >
          {hint}
        </Typography.Text>
      ) : null}
    </Form.Slot>
  )
}

export function Toggle({
  checked,
  onChange,
  children,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  children: ReactNode
}) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        minHeight: 44,
      }}
    >
      <Switch checked={checked} onChange={onChange} />
      <Typography.Text>{children}</Typography.Text>
    </div>
  )
}

export function SystemRow({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ minWidth: 0, display: 'contents' }}>
      <Typography.Text type="tertiary" size="small" component="dt">
        {label}
      </Typography.Text>
      <Typography.Text
        component="dd"
        style={{
          margin: 0,
          fontFamily: 'var(--semi-font-family-code)',
          fontSize: 12,
          wordBreak: 'break-all',
        }}
      >
        {value}
      </Typography.Text>
    </div>
  )
}
