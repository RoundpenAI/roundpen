import { useRef, useState } from 'react'
import { Input, Table, Typography } from '@douyinfe/semi-ui-19'
import { IconFile, IconFolder } from '@douyinfe/semi-icons'
import type { DirEntry } from '../../api'
import type { MessageKey } from '../../i18n'
import {
  fileKind,
  formatBytes,
  formatTime,
  joinPath,
} from '../../lib/workspaceFiles'

/** Inline rename box: Enter/blur commit once, Escape cancels once. */
function RenameInput({
  initial,
  onSubmit,
  onCancel,
}: {
  initial: string
  onSubmit: (next: string) => void
  onCancel: () => void
}) {
  const [value, setValue] = useState(initial)
  const done = useRef(false)
  const submit = (next: string) => {
    if (done.current) return
    done.current = true
    onSubmit(next)
  }
  const cancel = () => {
    if (done.current) return
    done.current = true
    onCancel()
  }
  return (
    <Input
      size="small"
      value={value}
      autoFocus
      style={{ width: '100%' }}
      onClick={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
      onChange={setValue}
      onKeyDown={(e) => {
        e.stopPropagation()
        if (e.key === 'Enter') submit(value)
        if (e.key === 'Escape') cancel()
      }}
      onBlur={() => submit(value)}
    />
  )
}

type Props = {
  path: string
  entries: DirEntry[]
  selectedKeys: string[]
  /** Absolute (workspace-relative) paths whose next paste is a move. */
  cutPaths: ReadonlySet<string>
  renaming: string | null
  onSelectionChange: (keys: string[]) => void
  onRowClick: (name: string, e: React.MouseEvent) => void
  onRowDoubleClick: (entry: DirEntry) => void
  onRowContextMenu: (entry: DirEntry, e: React.MouseEvent) => void
  onRenameSubmit: (name: string, next: string) => void
  onRenameCancel: () => void
  t: (key: MessageKey, vars?: Record<string, string | number>) => string
}

export function WorkspaceTable({
  path,
  entries,
  selectedKeys,
  cutPaths,
  renaming,
  onSelectionChange,
  onRowClick,
  onRowDoubleClick,
  onRowContextMenu,
  onRenameSubmit,
  onRenameCancel,
  t,
}: Props) {
  // Row-level props only cover click + style (Semi wires onClick itself);
  // double-click and the context menu ride on every cell, which is the hook
  // the table reliably forwards.
  const cellProps = (record: DirEntry) => ({
    onDoubleClick: () => onRowDoubleClick(record),
    onContextMenu: (e: React.MouseEvent) => {
      e.preventDefault()
      onRowContextMenu(record, e)
    },
  })

  return (
    <Table<DirEntry>
      dataSource={entries}
      rowKey="name"
      size="small"
      pagination={false}
      empty={<Typography.Text type="tertiary">{t('workspace.empty')}</Typography.Text>}
      rowSelection={{
        selectedRowKeys: selectedKeys,
        onChange: (keys) => onSelectionChange((keys ?? []).map(String)),
        clickRow: false,
      }}
      onRow={(record) => {
        const entry = record as DirEntry
        const abs = joinPath(path, entry.name)
        return {
          style: cutPaths.has(abs) ? { opacity: 0.5 } : undefined,
          onClick: (e: React.MouseEvent) => onRowClick(entry.name, e),
        }
      }}
      columns={[
        {
          title: t('workspace.column.name'),
          dataIndex: 'name',
          onCell: (record) => cellProps(record as DirEntry),
          render: (name: string, record: DirEntry) => {
            if (renaming === name) {
              return (
                <RenameInput
                  initial={name}
                  onSubmit={(next) => onRenameSubmit(name, next)}
                  onCancel={onRenameCancel}
                />
              )
            }
            return (
              <span
                className="rp-ws-name"
                // Bound here rather than relying on the table's cell/row
                // forwarding: the name is our own node, so the events always
                // reach it (double-click opens, right-click opens the menu).
                onDoubleClick={() => onRowDoubleClick(record)}
                onContextMenu={(e) => {
                  e.preventDefault()
                  onRowContextMenu(record, e)
                }}
              >
                {record.is_dir ? <IconFolder /> : <IconFile />}
                <span className="rp-ws-name-text">{name}</span>
              </span>
            )
          },
        },
        {
          title: t('workspace.column.size'),
          dataIndex: 'size',
          width: 110,
          onCell: (record) => cellProps(record as DirEntry),
          render: (size: number, record: DirEntry) =>
            record.is_dir ? '—' : formatBytes(size),
        },
        {
          title: t('workspace.column.type'),
          width: 110,
          onCell: (record) => cellProps(record as DirEntry),
          render: (_: unknown, record: DirEntry) =>
            t(`workspace.type.${fileKind(record)}` as MessageKey),
        },
        {
          title: t('workspace.column.modified'),
          width: 170,
          onCell: (record) => cellProps(record as DirEntry),
          render: (_: unknown, record: DirEntry) => formatTime(record.mod_time),
        },
      ]}
    />
  )
}
