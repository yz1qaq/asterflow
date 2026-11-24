import { isAxiosError } from 'axios'
import { useEffect, useRef, useState } from 'react'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import { Alert, Button, Empty, Modal, Select, Skeleton, Tag, Typography } from 'antd'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Redirect, useSearchParams } from 'wouter'
import { listAgents, listAgentVersions } from '@/api/agents'
import { getChatError } from '@/api/chat'
import { createConversation, deleteConversation, getConversation, listConversations, type Conversation, type ConversationMessage } from '@/api/conversations'
import { getWorkspaceContext } from '@/api/iam'
import { ChatComposer, readStringField } from '@/components/ChatComposer'
import { ConversationMessages, timeLabel } from '@/components/ConversationMessages'
import { isSessionValid, useAuthStore } from '@/store/authStore'

const conversationPageSize = 20

export function ChatPage() {
  const session = useAuthStore()
  if (!session.user || !isSessionValid(session)) return <Redirect to="/login" />
  if (!session.workspaceId) {
    return <Empty description="请先选择工作空间"><Link href="/workspaces">前往选择空间</Link></Empty>
  }

  return <section aria-labelledby="chat-heading">
    <div className="page-heading"><Typography.Title level={2} id="chat-heading">聊天</Typography.Title></div>
    <WorkspaceChat key={`${session.user.id}:${session.workspaceId}`}
      userID={session.user.id} workspaceID={session.workspaceId} />
  </section>
}

function WorkspaceChat({ userID, workspaceID }: { userID: string; workspaceID: string }) {
  const [search, setSearch] = useSearchParams()
  const conversationID = search.get('conversation_id') ?? ''
  const presetAgent = search.get('agent_id') ?? ''
  const presetVersion = search.get('agent_version_id') ?? ''
  const qc = useQueryClient()
  const [toDelete, setToDelete] = useState<Conversation | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState('')
  const deleteRequest = useRef<AbortController | null>(null)
  useEffect(() => () => deleteRequest.current?.abort(), [])
  const recentKey = ['chat-conversations', userID, workspaceID]
  const detailKey = ['conversation', userID, workspaceID, conversationID]
  const context = useQuery({
    queryKey: ['workspace-context', userID, workspaceID],
    retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => getWorkspaceContext(workspaceID, signal),
  })
  const authorized = context.isFetchedAfterMount && context.isSuccess && context.data.user_id === userID
  const canDelete = authorized && context.data.workspace_role !== 'viewer'
  const agents = useQuery({
    queryKey: ['agents', userID, workspaceID], enabled: authorized, retry: false,
    queryFn: ({ signal }) => listAgents(workspaceID, signal),
  })
  const conversations = useInfiniteQuery({
    queryKey: recentKey, enabled: authorized, retry: false, refetchOnMount: 'always',
    initialPageParam: 0,
    queryFn: ({ signal, pageParam }) => listConversations(workspaceID, signal, { limit: conversationPageSize, offset: pageParam }),
    getNextPageParam: (rows, _pages, offset) => rows.length === conversationPageSize ? offset + rows.length : undefined,
  })
  const detail = useQuery({
    queryKey: detailKey, enabled: authorized && Boolean(conversationID), retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => getConversation(workspaceID, conversationID, signal),
  })
  const agentNames = new Map(agents.data?.map(agent => [agent.id, agent.name]))
  const ownConversations = new Map<string, Conversation>()
  for (const page of conversations.data?.pages ?? []) {
    for (const row of page) {
      if (row.user_id === userID) ownConversations.set(row.id, row)
    }
  }
  const current = detail.data?.conversation
  const chatURL = (id: string) => `/chat?${new URLSearchParams({ conversation_id: id })}`

  function refreshConversationLists() {
    void qc.invalidateQueries({ queryKey: recentKey })
    void qc.invalidateQueries({ queryKey: ['conversations', userID, workspaceID] })
  }

  async function confirmDelete() {
    if (!toDelete || deleteRequest.current) return
    const id = toDelete.id
    const controller = new AbortController()
    deleteRequest.current = controller
    setDeleting(true)
    setDeleteError('')
    try {
      await deleteConversation(workspaceID, id, controller.signal)
      if (controller.signal.aborted) return
      setSearch(params => {
        const updated = new URLSearchParams(params)
        if (updated.get('conversation_id') === id) updated.delete('conversation_id')
        return updated
      }, { replace: true })
      qc.removeQueries({ queryKey: ['conversation', userID, workspaceID, id], exact: true })
      refreshConversationLists()
      setToDelete(null)
    } catch (err) {
      if (controller.signal.aborted) return
      const status = isAxiosError(err) ? err.response?.status : undefined
      if (status === 409) {
        setDeleteError('此会话正在生成，请停止或等回复完成后再删除。')
      } else if (status === 404) {
        setDeleteError('会话已不存在或已无法访问。')
        refreshConversationLists()
      } else if (status === 403) {
        setDeleteError('当前空间角色无法删除会话。')
      } else {
        setDeleteError('未能确认删除结果，请刷新会话列表后重试。')
      }
    } finally {
      deleteRequest.current = null
      if (!controller.signal.aborted) setDeleting(false)
    }
  }

  if (!context.isFetchedAfterMount || context.isPending) return <Skeleton active />
  if (!authorized) return <Alert role="alert" type="error" showIcon message="无法读取空间权限，请刷新或重新选择空间" />

  return <div className="chat-workspace">
    <aside className="chat-sidebar" aria-label="我的会话">
      <Button type="primary" icon={<PlusOutlined />} onClick={() => setSearch({})}>新建会话</Button>
      <Typography.Title level={5}>我的会话</Typography.Title>
      <nav className="chat-recents" aria-label="近期会话">
        {conversations.isError ? <Alert type="error" message="无法读取会话" />
          : conversations.isPending ? <Skeleton active title={false} paragraph={{ rows: 3 }} />
          : ownConversations.size === 0
            ? <Typography.Text type="secondary">{conversations.hasNextPage ? '当前记录中没有自己的会话' : '暂无会话'}</Typography.Text>
            : Array.from(ownConversations.values()).map(row => {
              const name = agentNames.get(row.agent_id) ?? '会话'
              const started = timeLabel(row.started_at)
              return <div className="chat-recent-row" key={row.id}>
                <Link href={chatURL(row.id)} className="chat-recent"
                  aria-current={row.id === conversationID ? 'page' : undefined}>
                  <span>{name}</span>
                  <small>{started}</small>
                </Link>
                {canDelete && <Button size="small" type="text" danger icon={<DeleteOutlined />}
                  aria-label={`删除会话 ${name} ${started}`} title="删除会话"
                  onClick={() => { setDeleteError(''); setToDelete(row) }} />}
              </div>
            })}
        {conversations.hasNextPage && <Button type="text" loading={conversations.isFetchingNextPage}
          onClick={() => void conversations.fetchNextPage()}>加载更早的会话</Button>}
      </nav>
    </aside>
    {conversationID
      ? detail.isError || (current && current.user_id !== userID)
        ? <div className="chat-start"><Alert role="alert" type="error" showIcon message="无法打开此会话"
          description="请选择当前空间中属于自己的会话。" /></div>
        : !current
          ? <div className="chat-start"><Skeleton active /></div>
          : <ActiveChat key={current.id} conversation={current} messages={detail.data?.messages ?? []}
            userID={userID} workspaceID={workspaceID} agentName={agentNames.get(current.agent_id)}
            onDone={() => qc.invalidateQueries({ queryKey: detailKey })} />
      : <StartChat key={`${presetAgent}:${presetVersion}`} userID={userID} workspaceID={workspaceID}
        presetAgent={presetAgent} presetVersion={presetVersion}
        agents={agents.data ?? []} agentsLoading={agents.isPending} agentsError={agents.isError}
        onCreated={id => {
          refreshConversationLists()
          setSearch({ conversation_id: id })
        }} />}
    <Modal title="删除会话？" open={Boolean(toDelete)} okText="永久删除" cancelText="取消"
      confirmLoading={deleting} okButtonProps={{ danger: true, disabled: !canDelete }} cancelButtonProps={{ disabled: deleting }}
      closable={!deleting} keyboard={!deleting} maskClosable={false}
      onOk={() => void confirmDelete()} onCancel={() => { if (!deleteRequest.current) setToDelete(null) }}>
      {toDelete && <Typography.Paragraph strong>
        {agentNames.get(toDelete.agent_id) ?? '会话'} · {timeLabel(toDelete.started_at)}
      </Typography.Paragraph>}
      <Typography.Paragraph>此操作会永久删除会话及其中的消息和工具调用记录。</Typography.Paragraph>
      {deleteError && <Alert role="alert" type="error" showIcon message={deleteError} />}
    </Modal>
  </div>
}

function StartChat({ userID, workspaceID, presetAgent, presetVersion, agents, agentsLoading, agentsError, onCreated }: {
  userID: string
  workspaceID: string
  presetAgent: string
  presetVersion: string
  agents: { id: string; name: string }[]
  agentsLoading: boolean
  agentsError: boolean
  onCreated: (id: string) => void
}) {
  const [selection, setSelection] = useState({ agentID: presetAgent, versionID: presetVersion })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const creating = useRef<AbortController | null>(null)
  useEffect(() => () => creating.current?.abort(), [])

  const agentID = agents.some(agent => agent.id === selection.agentID) ? selection.agentID : ''
  const versions = useQuery({
    queryKey: ['agent-versions', userID, workspaceID, agentID],
    enabled: Boolean(agentID), retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => listAgentVersions(workspaceID, agentID, signal),
  })
  const options = (versions.data ?? []).map(version => ({
    value: version.id,
    label: `v${version.version}`,
    disabled: !readStringField(version.snapshot_json, 'model_profile_version_id'),
  }))
  const firstUsable = options.find(option => !option.disabled)?.value
  const versionID = options.find(option => option.value === selection.versionID)?.value ?? firstUsable
  const ready = Boolean(agentID && versions.isSuccess
    && options.some(option => option.value === versionID && !option.disabled))
  const versionMissingModel = Boolean(versionID && options.some(option => option.value === versionID && option.disabled))

  async function start() {
    if (creating.current || !ready || !versionID) return
    const controller = new AbortController()
    creating.current = controller
    setBusy(true)
    setError('')
    try {
      const row = await createConversation(workspaceID, agentID, versionID, controller.signal)
      const scope = useAuthStore.getState()
      if (!controller.signal.aborted && isSessionValid(scope) && scope.workspaceId === workspaceID && scope.user?.id === userID) {
        onCreated(row.id)
      }
    } catch (err) {
      if (!controller.signal.aborted) setError(getChatError(err))
    } finally {
      creating.current = null
      if (!controller.signal.aborted) setBusy(false)
    }
  }

  return <div className="chat-start">
    <form className="chat-start-form" onSubmit={event => { event.preventDefault(); void start() }}>
      <Typography.Title level={3}>开始新会话</Typography.Title>
      {error && <Alert role="alert" type="error" showIcon message={error} />}
      {agentsError && <Alert role="alert" type="error" showIcon message="无法读取 Agent 列表" />}
      {versions.isError && <Alert role="alert" type="error" showIcon message="无法读取所选 Agent 的版本" />}
      <div className="chat-start-field">
        <label htmlFor="chat-agent">Agent</label>
        <Select id="chat-agent" value={agentID || undefined} loading={agentsLoading} disabled={busy}
          onChange={value => { setSelection({ agentID: value, versionID: '' }); setError('') }}
          options={agents.map(agent => ({ label: agent.name, value: agent.id }))} placeholder="选择 Agent"
          notFoundContent="当前空间暂无 Agent" />
      </div>
      <div className="chat-start-field">
        <label htmlFor="chat-agent-version">Agent 版本</label>
        <Select id="chat-agent-version" value={versionID || undefined}
          loading={Boolean(agentID && versions.isFetching)} disabled={!agentID || busy}
          onChange={value => { setSelection({ agentID, versionID: value }); setError('') }}
          options={options.map(option => option.disabled ? { ...option, label: `${option.label}（未配置模型）` } : option)}
          placeholder="选择版本" notFoundContent="暂无版本" />
      </div>
      {agentID && versions.isSuccess && (!firstUsable || versionMissingModel)
        && <Typography.Text type="secondary">请选择已配置模型的版本，或前往 <Link href="/agents">Agent 管理</Link>追加版本。</Typography.Text>}
      <Button type="primary" htmlType="submit" disabled={!ready} loading={busy}>开始聊天</Button>
    </form>
  </div>
}

function ActiveChat({ workspaceID, userID, conversation, messages, agentName, onDone }: {
  workspaceID: string
  userID: string
  conversation: Conversation
  messages: ConversationMessage[]
  agentName?: string
  onDone: () => Promise<void>
}) {
  const [streaming, setStreaming] = useState('')
  const timeline = useRef<HTMLDivElement>(null)
  const stickToBottom = useRef(true)
  const versions = useQuery({
    queryKey: ['agent-versions', userID, workspaceID, conversation.agent_id],
    retry: false,
    queryFn: ({ signal }) => listAgentVersions(workspaceID, conversation.agent_id, signal),
  })
  const version = versions.data?.find(row => row.id === conversation.agent_version_id)?.version

  useEffect(() => {
    const view = timeline.current
    if (view && stickToBottom.current) view.scrollTop = view.scrollHeight
  }, [messages, streaming])

  return <section className="chat-active" aria-label="当前会话">
    <header className="chat-thread-header">
      <Typography.Title level={4}>{agentName ?? '当前会话'}</Typography.Title>
      {version !== undefined && <Tag color="blue">v{version}</Tag>}
    </header>
    <div className="chat-timeline" ref={timeline} tabIndex={0} aria-label="会话消息"
      onScroll={event => {
        const view = event.currentTarget
        stickToBottom.current = view.scrollHeight - view.clientHeight - view.scrollTop < 48
      }}>
      {messages.length === 0 && !streaming && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="发送一条消息开始对话" />}
      <ConversationMessages messages={messages} />
      {streaming && <article className="chat-message">
        <Typography.Text strong>助手</Typography.Text>
        <div className="chat-message-content">{streaming}</div>
      </article>}
    </div>
    <ChatComposer workspaceID={workspaceID} userID={userID} conversation={conversation}
      firstTurn={messages.length === 0} onDone={onDone} onDraftChange={setStreaming} />
  </section>
}
