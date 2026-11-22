import { useRef, useState } from 'react'
import { Alert, Button, Empty, Input, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Redirect, useLocation } from 'wouter'
import { getWorkspaceContext } from '@/api/iam'
import { getConversation, listConversations, type Conversation } from '@/api/conversations'
import { getChatError, streamChat } from '@/api/chat'
import { isSessionValid, useAuthStore } from '@/store/authStore'

const timeLabel = (value: string) => Number.isNaN(Date.parse(value)) ? '—' : new Date(value).toLocaleString('zh-CN', { hour12: false })
const statusColor: Record<string, string> = { active: 'processing', running: 'warning', completed: 'green', error: 'red' }

export function ConversationsPage() {
  const session = useAuthStore()
  const [, navigate] = useLocation()
  const [selected, setSelected] = useState<Conversation | null>(null)
  if (!isSessionValid(session) || !session.user || !session.token) return <Redirect to="/login" />
  const workspaceID = session.workspaceId
  const context = useQuery({
    queryKey: ['workspace-context', session.user.id, workspaceID ?? ''],
    enabled: Boolean(workspaceID), retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => getWorkspaceContext(workspaceID!, signal),
  })
  const conversations = useQuery({
    queryKey: ['conversations', session.user.id, workspaceID ?? ''],
    enabled: Boolean(workspaceID), retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => listConversations(workspaceID!, signal),
  })
  const detail = useQuery({
    queryKey: ['conversation', session.user.id, workspaceID ?? '', selected?.id ?? ''],
    enabled: Boolean(workspaceID && selected), retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => getConversation(workspaceID!, selected!.id, signal),
  })

  if (!workspaceID) return <Empty description="请先选择工作空间"><Button type="primary" onClick={() => navigate('/workspaces')}>前往选择空间</Button></Empty>
  if (!context.isFetchedAfterMount || context.isPending) return <Skeleton active />
  if (context.isError) return <Alert role="alert" type="error" showIcon message="无法读取空间权限，请刷新或重新选择空间" />

  return <section aria-labelledby="conversations-heading">
    <div className="page-heading"><div><Typography.Title level={2} id="conversations-heading">会话历史</Typography.Title>
      <Typography.Paragraph className="page-description">会话固定创建时的 Agent 版本；普通成员只能查看自己的会话，owner/admin 可查看全空间。</Typography.Paragraph></div></div>
    <div className="workspace-current model-workspace"><Space wrap>
      <Typography.Text strong>当前空间</Typography.Text>
      <Typography.Text className="workspace-id" copyable>{workspaceID}</Typography.Text>
      <Tag color="blue">空间角色：{context.data.workspace_role}</Tag></Space></div>
    {conversations.isError ? <Alert role="alert" type="error" showIcon message="无法读取会话，请刷新后重试" />
      : <Table<Conversation> rowKey="id" dataSource={conversations.data ?? []} loading={conversations.isFetching}
        pagination={{ pageSize: 10, hideOnSinglePage: true }} locale={{ emptyText: <Empty description="暂无会话" /> }}
        columns={[
          { title: '会话 / ID', render: (_, row) => <Space direction="vertical" size={2}>
            <Tag color={statusColor[row.status] ?? 'default'}>{row.status}</Tag>
            <Typography.Text className="workspace-id" copyable>{row.id}</Typography.Text></Space> },
          { title: '用户', dataIndex: 'user_id', render: (value: string) => <Typography.Text copyable>{value.slice(0, 8)}</Typography.Text> },
          { title: 'Agent 版本', dataIndex: 'agent_version_id', render: (value: string) => <Typography.Text copyable>{value.slice(0, 8)}</Typography.Text> },
          { title: '开始时间', dataIndex: 'started_at', width: 185, render: timeLabel },
          { title: '操作', width: 110, render: (_, row) => <Button size="small" type={selected?.id === row.id ? 'primary' : 'default'}
            onClick={() => setSelected(row)}>查看</Button> },
        ]} />}
    {selected && <ConversationDetail key={selected.id} workspaceID={workspaceID} conversation={selected}
      queryKey={['conversation', session.user.id, workspaceID, selected.id]}
      messages={detail.data?.messages ?? []} loading={!detail.isFetchedAfterMount}
      error={detail.isError} />}
  </section>
}

function ConversationDetail({ workspaceID, conversation, queryKey, messages, loading, error }: {
  workspaceID: string
  conversation: Conversation
  queryKey: unknown[]
  messages: import('@/api/conversations').ConversationMessage[]
  loading: boolean
  error: boolean
}) {
  const qc = useQueryClient()
  return <section className="prompt-detail" aria-label={`会话 ${conversation.id} 详情`}>
    <div className="model-section-heading"><div><Typography.Title level={4}>会话消息</Typography.Title>
      <Typography.Text>{conversation.id}</Typography.Text></div></div>
    {error ? <Alert role="alert" type="error" showIcon message="无法读取会话消息" />
      : loading ? <Skeleton active />
      : <Space direction="vertical" style={{ width: '100%' }}>
        {messages.length === 0 && <Typography.Text type="secondary">暂无消息，先聊一句。</Typography.Text>}
        {messages.map(message => <div key={message.id} className="prompt-env-item">
          <Space wrap><Tag color={message.role === 'user' ? 'blue' : message.role === 'tool' ? 'gold' : 'purple'}>{message.role}</Tag>
            <Typography.Text type="secondary">{timeLabel(message.created_at)}</Typography.Text>
            {message.tool_call_id && <Typography.Text type="secondary">tool_call: {message.tool_call_id.slice(0, 8)}</Typography.Text>}</Space>
          <pre className="prompt-text">{message.content || '（空内容）'}</pre>
          {message.tool_calls?.length ? <pre className="prompt-text">{JSON.stringify(message.tool_calls, null, 2)}</pre> : null}
        </div>)}
        <ChatComposer workspaceID={workspaceID} conversationID={conversation.id}
          onDone={() => void qc.invalidateQueries({ queryKey })} />
      </Space>}
  </section>
}

function ChatComposer({ workspaceID, conversationID, onDone }: {
  workspaceID: string
  conversationID: string
  onDone: () => void
}) {
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const abortRef = useRef<AbortController | null>(null)

  async function send() {
    const text = input.trim()
    if (!text || busy) return
    const controller = new AbortController()
    abortRef.current = controller
    setBusy(true)
    setError('')
    setStreaming('')
    try {
      for await (const event of streamChat(workspaceID, conversationID, text, undefined, controller.signal)) {
        if (event.type === 'model_delta') {
          const delta = event.data as { content?: unknown } | undefined
          if (typeof delta?.content === 'string') setStreaming(prev => prev + delta.content)
        }
      }
      setInput('')
      onDone()
    } catch (err) {
      setError(getChatError(err))
    } finally {
      setBusy(false)
      abortRef.current = null
    }
  }

  return <div className="prompt-env-item" aria-label="聊天输入">
    {error && <Alert className="user-create-error" role="alert" type="error" showIcon message={error} />}
    {streaming && <div><Tag color="purple">assistant</Tag>
      <pre className="prompt-text">{streaming || '…'}</pre></div>}
    <Space.Compact style={{ width: '100%' }}>
      <Input.TextArea autoSize={{ minRows: 1, maxRows: 4 }} value={input}
        onChange={event => setInput(event.target.value)} placeholder="输入消息，Enter 发送"
        disabled={busy} onPressEnter={event => { event.preventDefault(); void send() }} />
      {busy
        ? <Button danger onClick={() => abortRef.current?.abort()}>停止</Button>
        : <Button type="primary" disabled={!input.trim()} onClick={() => void send()}>发送</Button>}
    </Space.Compact>
  </div>
}
