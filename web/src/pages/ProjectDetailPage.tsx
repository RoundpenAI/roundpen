import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  Banner,
  Button,
  Spin,
  Tag,
  Toast,
  Typography,
} from '@douyinfe/semi-ui-19'
import { IconArrowLeft } from '@douyinfe/semi-icons'
import { projects, type Project } from '../api'
import { useT, type MessageKey } from '../i18n'

function statusKey(status: Project['status']): MessageKey {
  return `projects.status.${status}`
}

export function ProjectDetailPage() {
  const t = useT()
  const navigate = useNavigate()
  const { key = '' } = useParams()
  const [project, setProject] = useState<Project | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [opening, setOpening] = useState(false)
  const [archiving, setArchiving] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    projects
      .get(key)
      .then((res) => {
        if (cancelled) return
        setProject(res.project)
        setError(null)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setProject(null)
        setError(e instanceof Error ? e.message : t('projects.loadFailed'))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [key, t])

  async function openCoordinator() {
    if (!project || opening) return
    setOpening(true)
    try {
      const res = await projects.ensureSession(project.key)
      const assistantId =
        res.project.coordinatorAssistantId || project.coordinatorAssistantId
      if (!assistantId) {
        throw new Error(t('projects.noCoordinator'))
      }
      navigate(`/a/${assistantId}/s/${res.sessionId}`)
    } catch (e) {
      const message =
        e instanceof Error ? e.message : t('projects.openChatFailed')
      Toast.error(message)
    } finally {
      setOpening(false)
    }
  }

  async function toggleArchive() {
    if (!project || archiving) return
    setArchiving(true)
    const next = project.status === 'active' ? 'archived' : 'active'
    try {
      const res = await projects.update(project.key, { status: next })
      setProject(res.project)
      Toast.success(
        next === 'archived'
          ? t('projects.archived')
          : t('projects.restored'),
      )
    } catch (e) {
      Toast.error(e instanceof Error ? e.message : t('projects.updateFailed'))
    } finally {
      setArchiving(false)
    }
  }

  if (loading) {
    return (
      <div style={{ padding: 48, textAlign: 'center' }}>
        <Spin />
      </div>
    )
  }

  if (error || !project) {
    return (
      <div style={{ padding: 24, maxWidth: 800, margin: '0 auto' }}>
        <Link to="/projects">{t('projects.back')}</Link>
        <Banner
          type="danger"
          description={error ?? t('projects.loadFailed')}
          closeIcon={null}
          style={{ marginTop: 16 }}
        />
      </div>
    )
  }

  return (
    <div style={{ padding: 24, maxWidth: 800, margin: '0 auto', width: '100%' }}>
      <Button
        theme="borderless"
        icon={<IconArrowLeft />}
        onClick={() => navigate('/projects')}
        style={{ marginBottom: 12, paddingLeft: 0 }}
      >
        {t('projects.back')}
      </Button>

      <div
        style={{
          display: 'flex',
          alignItems: 'flex-start',
          gap: 12,
          flexWrap: 'wrap',
          marginBottom: 8,
        }}
      >
        <Typography.Title heading={3} style={{ margin: 0, flex: 1 }}>
          {project.icon ? `${project.icon} ` : ''}
          {project.name}
        </Typography.Title>
        <Tag color={project.status === 'active' ? 'green' : 'grey'}>
          {t(statusKey(project.status))}
        </Tag>
      </div>

      <Typography.Text
        type="tertiary"
        size="small"
        style={{ fontFamily: 'var(--semi-font-family-code)' }}
      >
        {project.key}
      </Typography.Text>

      <Typography.Paragraph type="tertiary" style={{ marginTop: 16 }}>
        {t('projects.detail.guide')}
      </Typography.Paragraph>

      <div
        style={{
          display: 'grid',
          gap: 12,
          marginTop: 20,
          marginBottom: 24,
        }}
      >
        <MetaRow label={t('projects.field.slug')} value={project.slug} code />
        <MetaRow
          label={t('projects.field.path')}
          value={project.repoPath}
          code
        />
        <MetaRow
          label={t('projects.field.repoUrl')}
          value={project.repoUrl || t('projects.field.repoUrlEmpty')}
        />
        <MetaRow
          label={t('projects.field.context')}
          value={`${project.repoPath}/.roundpen/context`}
          code
        />
      </div>

      <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
        <Button
          theme="solid"
          type="primary"
          loading={opening}
          disabled={project.status !== 'active'}
          onClick={() => void openCoordinator()}
        >
          {t('projects.openChat')}
        </Button>
        <Link
          to={`/workspace?path=${encodeURIComponent(project.repoPath)}`}
          style={{ textDecoration: 'none' }}
        >
          <Button>{t('projects.openFiles')}</Button>
        </Link>
        <Button loading={archiving} onClick={() => void toggleArchive()}>
          {project.status === 'active'
            ? t('projects.archive')
            : t('projects.restore')}
        </Button>
      </div>
    </div>
  )
}

function MetaRow({
  label,
  value,
  code,
}: {
  label: string
  value: string
  code?: boolean
}) {
  return (
    <div>
      <Typography.Text size="small" type="tertiary">
        {label}
      </Typography.Text>
      <div>
        <Typography.Text
          style={
            code
              ? { fontFamily: 'var(--semi-font-family-code)' }
              : undefined
          }
        >
          {value}
        </Typography.Text>
      </div>
    </div>
  )
}
