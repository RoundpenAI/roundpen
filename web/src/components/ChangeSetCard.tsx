import { useState } from 'react'
import { Link } from 'react-router-dom'
import {
  shouldFoldDiff,
  type ChangeSet,
  type FileChange,
  workspacePreviewHref,
} from '../lib/changeSet'

type Props = {
  changeSet: ChangeSet
  /** Optional heading override. */
  title?: string
}

export function ChangeSetCard({ changeSet, title }: Props) {
  const [open, setOpen] = useState(true)
  const n = changeSet.files.length
  const heading =
    title ||
    (n === 1 ? '1 file changed' : `${n} files changed`)

  return (
    <div className="rp-changeset">
      <button
        type="button"
        className="rp-changeset-summary"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className={`chat-tool-chevron ${open ? 'open' : ''}`} aria-hidden>
          ▸
        </span>
        <span className="rp-changeset-title">{heading}</span>
        {changeSet.plus > 0 ? (
          <span className="chat-tool-plus"> +{changeSet.plus}</span>
        ) : null}
        {changeSet.minus > 0 ? (
          <span className="chat-tool-minus"> -{changeSet.minus}</span>
        ) : null}
      </button>
      {open && (
        <ul className="rp-changeset-list">
          {changeSet.files.map((f) => (
            <FileChangeRow key={f.path} file={f} />
          ))}
        </ul>
      )}
    </div>
  )
}

function FileChangeRow({ file }: { file: FileChange }) {
  const large = shouldFoldDiff(file.unified)
  const [expanded, setExpanded] = useState(!large)
  const href = workspacePreviewHref(file.path)

  return (
    <li className="rp-changeset-file">
      <div className="rp-changeset-file-head">
        <button
          type="button"
          className="rp-changeset-file-toggle"
          aria-expanded={expanded}
          onClick={() => setExpanded((v) => !v)}
        >
          <span className={`chat-tool-chevron ${expanded ? 'open' : ''}`} aria-hidden>
            ▸
          </span>
          <code className="rp-changeset-path">{file.path}</code>
          {file.plus > 0 ? (
            <span className="chat-tool-plus"> +{file.plus}</span>
          ) : null}
          {file.minus > 0 ? (
            <span className="chat-tool-minus"> -{file.minus}</span>
          ) : null}
        </button>
        <Link className="rp-changeset-open" to={href} title="Open in Files">
          Open
        </Link>
      </div>
      {expanded ? (
        <pre className="rp-changeset-diff" tabIndex={0}>
          {file.unified.split(/\r?\n/).map((line, i) => (
            <span
              key={i}
              className={
                line.startsWith('+') && !line.startsWith('+++')
                  ? 'rp-diff-add'
                  : line.startsWith('-') && !line.startsWith('---')
                    ? 'rp-diff-del'
                    : line.startsWith('@@')
                      ? 'rp-diff-hunk'
                      : undefined
              }
            >
              {line}
              {'\n'}
            </span>
          ))}
        </pre>
      ) : (
        <button
          type="button"
          className="rp-changeset-fold"
          onClick={() => setExpanded(true)}
        >
          Large diff folded — show {file.unified.split(/\r?\n/).length} lines
        </button>
      )}
    </li>
  )
}
