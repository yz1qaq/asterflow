import axios from 'axios'
import { useEffect, useRef, useState } from 'react'
import { Alert, App, Button, Empty, Form, Input, Modal, Select, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { VersionEnvironmentTags } from '@/components/VersionEnvironmentTags'
import { Link, Redirect, useLocation } from 'wouter'
import { getWorkspaceContext, getWorkspaceSelectionError, isWorkspaceUnavailable } from '@/api/iam'
import {
  canDeleteAgents, canWriteAgents, createAgent, createAgentVersion, deleteAgent, listAgentEnvs, listAgentVersions, listAgents,
  promoteAgentVersion, type Agent, type AgentVersion,
} from '@/api/agents'
import { canWriteConversations } from '@/api/conversations'
import { listWorkspaceProfileVersions } from '@/api/modelBindings'
import { listModelProfiles } from '@/api/modelProfiles'
import { listPrompts } from '@/api/prompts'
import { isSessionValid, useAuthStore } from '@/store/authStore'

type Scope = { userID: string; token: string; workspaceID: string }
type Request = <T>(signal: AbortSignal, run: () => Promise<T>) => Promise<T>
const contextKey = ({ userID, workspaceID }: Scope) => ['workspace-context', userID, workspaceID]
const agentsKey = ({ userID, workspaceID }: Scope) => ['agents', userID, workspaceID]
const versionsKey = ({ userID, workspaceID }: Scope, agentID: string) => ['agent-versions', userID, workspaceID, agentID]
const envsKey = ({ userID, workspaceID }: Scope, agentID: string) => ['agent-envs', userID, workspaceID, agentID]
const timeLabel = (value: string) => Number.isNaN(Date.parse(value)) ? '—' : new Date(value).toLocaleString('zh-CN', { hour12: false })
const ENVS = ['dev', 'staging', 'prod']
const TEMPLATES = ['general', 'customer_support', 'internal_copilot']

function useFormRequest({ userID, token, workspaceID }: Scope) {
  const request = useRef<AbortController | null>(null)
  useEffect(() => {
    const controller = new AbortController()
    request.current = controller
    const abortIfScopeChanged = (state: ReturnType<typeof useAuthStore.getState>) => {
      if (!isSessionValid(state) || state.token !== token || state.user?.id !== userID || state.workspaceId !== workspaceID) {
        controller.abort()
      }
    }
    abortIfScopeChanged(useAuthStore.getState())
    const unsubscribe = useAuthStore.subscribe(abortIfScopeChanged)
    return () => { unsubscribe(); controller.abort() }
  }, [userID, token, workspaceID])
  return () => {
    const signal = request.current?.signal
    return signal && !signal.aborted && isSessionValid(useAuthStore.getState()) ? signal : undefined
  }
}

function getAgentError(error: unknown) {
  if (axios.isAxiosError<{ error?: unknown; code?: unknown }>(error)) {
    switch (error.response?.status) {
      case 401: return '登录已失效，请重新登录'
      case 403: return '当前空间权限不足，请重新确认空间权限'
      case 404: return 'Agent 或引用的版本不在当前空间'
      case 409:
        if (error.response.data?.code === 'agent_conversation_running') {
          return '此 Agent 还有会话正在生成，请等回复完成或停止生成后再删除。'
        }
        break
      case 400: {
        const detail = error.response.data?.error
        return typeof detail === 'string' ? detail.slice(0, 300) : '请检查名称、系统提示词或引用版本'
      }
    }
    if (!error.response || error.response.status >= 500) return '保存结果未确认，请刷新后重试'
  } else if (error instanceof Error) {
    return error.message
  }
  return '操作失败，请稍后重试'
}

export function AgentsPage() {
  const session = useAuthStore()
  const [, navigate] = useLocation()
  if (!isSessionValid(session) || !session.user || !session.token) return <Redirect to="/login" />
  return <section aria-labelledby="agents-heading">
    <div className="page-heading"><Typography.Title level={2} id="agents-heading">Agent 管理</Typography.Title></div>
    {session.workspaceId ? <WorkspaceAgents key={`${session.token}:${session.user.id}:${session.workspaceId}`}
      scope={{ userID: session.user.id, token: session.token, workspaceID: session.workspaceId }} />
      : <Empty description="请先选择工作空间"><Button type="primary" onClick={() => navigate('/workspaces')}>前往选择空间</Button></Empty>}
  </section>
}

function WorkspaceAgents({ scope }: { scope: Scope }) {
  const { message } = App.useApp()
  const [access, setAccess] = useState<'checking' | 'denied' | null>(null)
  const accessRef = useRef(false)
  const context = useQuery({ queryKey: contextKey(scope), retry: false, refetchOnMount: 'always',
    queryFn: async ({ signal }) => {
      const result = await getWorkspaceContext(scope.workspaceID, signal)
      if (result.user_id !== scope.userID) throw new Error('当前身份已变化，请重新登录')
      return result
    } })
  useEffect(() => {
    const current = useAuthStore.getState()
    if (context.isFetchedAfterMount && isWorkspaceUnavailable(context.error) && current.workspaceId === scope.workspaceID) {
      current.setWorkspace(null)
      void message.warning(getWorkspaceSelectionError(context.error))
    }
  }, [context.error, context.isFetchedAfterMount, scope.workspaceID, message])
  async function recheckAccess(deniedRole?: string) {
    if (accessRef.current) return
    accessRef.current = true
    setAccess('checking')
    try {
      const result = await context.refetch()
      setAccess(result.isSuccess && result.data.workspace_role === deniedRole ? 'denied' : null)
    } finally {
      accessRef.current = false
    }
  }
  const request: Request = async (signal, run) => {
    try {
      return await run()
    } catch (error) {
      if (!signal.aborted && axios.isAxiosError(error) && error.response?.status === 403) void recheckAccess(context.data?.workspace_role)
      throw error
    }
  }
  if (access === 'checking' || !context.isFetchedAfterMount || context.isPending) return <div aria-label="正在确认空间权限"><Skeleton active /></div>
  if (access === 'denied' || context.isError) return <Alert role="alert" type="error" showIcon
    message={context.isError ? getWorkspaceSelectionError(context.error) : '当前无法访问 Agent，请重新确认空间权限'}
    action={<Button onClick={() => void recheckAccess()}>重新确认</Button>} />
  const writable = canWriteAgents(context.data.workspace_role)
  const deletable = canDeleteAgents(context.data.workspace_role)
  const chatAllowed = canWriteConversations(context.data.workspace_role)
  return <>
    <div className="workspace-current model-workspace" aria-label="Agent 管理当前空间"><Space wrap>
      <Typography.Text strong>当前空间</Typography.Text><Typography.Text className="workspace-id" copyable>{scope.workspaceID}</Typography.Text>
      <Tag color="blue">空间角色：{context.data.workspace_role}</Tag>
    </Space></div>
    <AgentPanel key={`${writable}:${deletable}`} scope={scope} writable={writable} deletable={deletable}
      chatAllowed={chatAllowed} request={request} />
  </>
}

function AgentPanel({ scope, writable, deletable, chatAllowed, request }: {
  scope: Scope; writable: boolean; deletable: boolean; chatAllowed: boolean; request: Request
}) {
  const { message } = App.useApp()
  const qc = useQueryClient()
  const { workspaceID } = scope
  const [selected, setSelected] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [appending, setAppending] = useState<Agent | null>(null)
  const [publishing, setPublishing] = useState<AgentVersion | null>(null)
  const [toDelete, setToDelete] = useState<Agent | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const busyRef = useRef(false)
  const formSignal = useFormRequest(scope)
  const [form] = Form.useForm()
  const agents = useQuery({ queryKey: agentsKey(scope), retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => request(signal, () => listAgents(workspaceID, signal)) })
  const prompts = useQuery({
    queryKey: ['agent-prompt-options', scope.userID, workspaceID],
    enabled: creating || Boolean(appending), retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => request(signal, () => listPrompts(workspaceID, signal)),
  })
  const editingVersion = creating || Boolean(appending)
  const profiles = useQuery({
    queryKey: ['agent-model-profiles', scope.userID, workspaceID],
    enabled: editingVersion, retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => request(signal, () => listModelProfiles(workspaceID, signal)),
  })
  const modelVersions = useQuery({
    queryKey: ['agent-model-profile-versions', scope.userID, workspaceID],
    enabled: editingVersion, retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => request(signal, () => listWorkspaceProfileVersions(workspaceID, signal)),
  })
  const profileNames = new Map(profiles.data?.map(profile => [profile.id, profile.name]))
  const modelOptions = profiles.isError || modelVersions.isError ? [] : (modelVersions.data ?? []).map(version => ({
    value: version.id,
    label: `${profileNames.get(version.profile_id) ?? version.profile_id} · v${version.version}`,
  }))
  const current = agents.isSuccess ? agents.data.find(agent => agent.id === selected) : undefined
  const versions = useQuery({ queryKey: versionsKey(scope, current?.id ?? ''), enabled: Boolean(current),
    retry: false, refetchOnMount: 'always', queryFn: ({ signal }) => request(signal, () => listAgentVersions(workspaceID, current!.id, signal)) })
  const envs = useQuery({ queryKey: envsKey(scope, current?.id ?? ''), enabled: Boolean(current),
    retry: false, refetchOnMount: 'always', queryFn: ({ signal }) => request(signal, () => listAgentEnvs(workspaceID, current!.id, signal)) })

  function openCreate() { form.resetFields(); setError(''); setCreating(true) }
  function openAppend(agent: Agent) { form.resetFields(); setError(''); setAppending(agent) }
  function openPublish(version: AgentVersion) { form.resetFields(); setError(''); setPublishing(version) }
  function closeModal() {
    if (busyRef.current) return
    form.resetFields(); setError(''); setCreating(false); setAppending(null); setPublishing(null)
  }
  async function run(action: (signal: AbortSignal) => Promise<unknown>) {
    if (busyRef.current) return null
    const signal = formSignal(); if (!signal) return null
    busyRef.current = true; setBusy(true); setError('')
    try {
      const result = await request(signal, () => action(signal))
      return signal.aborted ? null : result
    } catch (err) {
      if (!signal.aborted) setError(getAgentError(err))
      return null
    } finally {
      busyRef.current = false
      if (!signal.aborted) setBusy(false)
    }
  }
  async function save(values: {
    name?: string; description?: string; template?: string; system_prompt?: string
    user_prompt_id?: string; model_profile_version_id?: string; env?: string
  }) {
    if (!writable) return
    const body = {
      system_prompt: values.system_prompt ?? '',
      user_prompt_id: values.user_prompt_id,
      model_profile_version_id: values.model_profile_version_id,
    }
    if (creating) {
      const created = await run(() => createAgent(workspaceID, { name: values.name ?? '', description: values.description, template: values.template, ...body }))
      if (created) {
        const { agent } = created as { agent: Agent }
        setCreating(false); setSelected(agent.id)
        void message.success('Agent 与 v1 已创建')
        void qc.invalidateQueries({ queryKey: agentsKey(scope) })
      }
      return
    }
    if (appending) {
      const appended = await run(() => createAgentVersion(workspaceID, appending.id, body))
      if (appended) {
        setAppending(null)
        void message.success(`v${(appended as AgentVersion).version} 已保存`)
        void qc.invalidateQueries({ queryKey: versionsKey(scope, appending.id) })
      }
      return
    }
    if (publishing) {
      const values2 = await form.validateFields()
      const result = await run(() => promoteAgentVersion(workspaceID, publishing.agent_id, values2.env as string, publishing.id))
      if (result) {
        setPublishing(null)
        void message.success(`v${publishing.version} 已发布到 ${values2.env}`)
        void qc.invalidateQueries({ queryKey: envsKey(scope, publishing.agent_id) })
      }
    }
  }

  async function confirmDelete() {
    if (!deletable || !toDelete) return
    const { id } = toDelete
    const deleted = await run(async signal => {
      await deleteAgent(workspaceID, id, signal)
      return true
    })
    if (!deleted) return

    setToDelete(null)
    setSelected(value => value === id ? null : value)
    qc.setQueryData<Agent[]>(agentsKey(scope), rows => rows?.filter(row => row.id !== id))
    qc.removeQueries({ queryKey: versionsKey(scope, id), exact: true })
    qc.removeQueries({ queryKey: envsKey(scope, id), exact: true })
    qc.removeQueries({
      queryKey: ['conversation', scope.userID, workspaceID],
      predicate: query => (query.state.data as { conversation?: { agent_id: string } } | undefined)?.conversation?.agent_id === id,
    })
    void qc.invalidateQueries({ queryKey: agentsKey(scope) })
    void qc.invalidateQueries({ queryKey: ['chat-conversations', scope.userID, workspaceID] })
    void qc.invalidateQueries({ queryKey: ['conversations', scope.userID, workspaceID] })
    void message.success('Agent 已删除')
  }

  return <section aria-label="Agent 列表">
    <div className="model-section-heading version-list-toolbar">
      <Space wrap><Button icon={<ReloadOutlined />} loading={agents.isFetching} onClick={() => void agents.refetch()}>刷新</Button>
        {writable && <Button icon={<PlusOutlined />} onClick={openCreate}>新建 Agent</Button>}</Space></div>
    {agents.isError ? <Alert role="alert" type="error" showIcon message={getAgentError(agents.error)} />
      : <Table<Agent> rowKey="id" dataSource={agents.data ?? []} loading={agents.isFetching} scroll={{ x: 720 }}
        pagination={{ pageSize: 10, hideOnSinglePage: true }} locale={{ emptyText: <Empty description="当前空间暂无 Agent" /> }}
        columns={[
          { title: '名称', render: (_, row) => {
            const description = [row.description, row.template !== 'general' ? row.template : ''].filter(Boolean).join(' · ')
            return <Space direction="vertical" size={2}><Typography.Text strong>{row.name}</Typography.Text>
              {description && <Typography.Text type="secondary">{description}</Typography.Text>}</Space>
          } },
          { title: '创建时间', dataIndex: 'created_at', width: 185, render: timeLabel },
          { title: '操作', width: 145, render: (_, row) => <Space>
            <Button size="small" type={selected === row.id ? 'primary' : 'default'}
              onClick={() => setSelected(row.id)} aria-label={`查看 Agent ${row.name}`}>查看</Button>
            {deletable && <Button size="small" danger aria-label={`删除 Agent ${row.name}`}
              onClick={() => { setError(''); setToDelete(row) }}>删除</Button>}
          </Space> },
        ]} />}
    {current && <section aria-label={`${current.name} 详情`} className="prompt-detail">
      <div className="model-section-heading"><Typography.Title level={4}>{current.name} · 版本</Typography.Title>
        {writable && <Button onClick={() => openAppend(current)}>追加版本</Button>}</div>
      {envs.isError && <Alert className="version-env-alert" role="alert" type="warning" showIcon message="无法读取发布环境"
        action={<Button size="small" onClick={() => void envs.refetch()}>重试</Button>} />}
      {versions.isError ? <Alert role="alert" type="error" showIcon message={getAgentError(versions.error)}
        action={<Button size="small" onClick={() => void versions.refetch()}>重试</Button>} />
        : <Table<AgentVersion> rowKey="id" dataSource={versions.data ?? []} loading={versions.isFetching || envs.isPending}
          pagination={{ pageSize: 8, hideOnSinglePage: true }} locale={{ emptyText: <Empty description="暂无版本" /> }}
          columns={[
            { title: '版本', width: 240, render: (_, row) => <VersionEnvironmentTags version={row.version} versionID={row.id}
              bindings={envs.isSuccess ? envs.data : undefined} /> },
            { title: '配置', render: (_, row) => <details className="agent-version-snapshot">
              <summary>查看快照</summary><pre className="prompt-text">{row.snapshot_json}</pre>
            </details> },
            { title: '创建时间', dataIndex: 'created_at', width: 185, render: timeLabel },
            { title: '操作', width: 185, render: (_, row) => <Space>
              {chatAllowed && <Link href={`/chat?${new URLSearchParams({ agent_id: row.agent_id, agent_version_id: row.id })}`}>前往聊天</Link>}
              {writable && <Button size="small" onClick={() => openPublish(row)}>发布</Button>}
            </Space> },
          ]} />}
    </section>}
    {writable && <Modal title={creating ? '新建 Agent' : appending ? `追加版本到 ${appending.name}` : publishing ? `发布 v${publishing.version}` : ''}
      open={creating || Boolean(appending) || Boolean(publishing)} onCancel={closeModal} onOk={() => form.submit()} okText={publishing ? '发布' : '保存'} cancelText="取消"
      confirmLoading={busy} cancelButtonProps={{ disabled: busy }} closable={!busy} keyboard={!busy} maskClosable={!busy}>
      {error && <Alert className="user-create-error" role="alert" type="error" showIcon message={error} />}
      <Form form={form} layout="vertical" onFinish={save} disabled={busy} preserve={false}>
        {creating && <>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}><Input /></Form.Item>
          <Form.Item name="description" label="描述"><Input /></Form.Item>
          <Form.Item name="template" label="模板类型" initialValue="general">
            <Select options={TEMPLATES.map(value => ({ value, label: value }))} />
          </Form.Item>
        </>}
        {(creating || appending) && <Form.Item name="system_prompt" label="系统提示词（字面量）" initialValue="">
          <Input.TextArea rows={4} placeholder="例如：你是订单客服，回答要简洁。" />
        </Form.Item>}
        {(creating || appending) && <Form.Item name="user_prompt_id" label="首轮用户模板">
          <Select allowClear loading={prompts.isFetching} placeholder="可选"
            options={(prompts.isError ? [] : prompts.data ?? []).map(prompt => ({ value: prompt.id, label: prompt.name }))}
            notFoundContent={prompts.isError ? '无法读取提示词' : '暂无提示词'} />
        </Form.Item>}
        {editingVersion && <Form.Item name="model_profile_version_id" label="模型方案版本">
          <Select allowClear loading={profiles.isFetching || modelVersions.isFetching} placeholder="聊天需要选择模型方案"
            options={modelOptions} notFoundContent={profiles.isError || modelVersions.isError ? '无法读取模型方案' : '暂无模型方案版本'} />
        </Form.Item>}
        {publishing && <Form.Item name="env" label="目标环境" rules={[{ required: true, message: '请选择环境' }]} initialValue="dev">
          <Select options={ENVS.map(value => ({ value, label: value }))} />
        </Form.Item>}
      </Form>
    </Modal>}
    {deletable && <Modal title={toDelete ? `删除 ${toDelete.name}？` : '删除 Agent'}
      open={toDelete !== null} okText="永久删除" cancelText="取消" okButtonProps={{ danger: true }}
      onOk={() => void confirmDelete()} onCancel={() => { if (!busyRef.current) setToDelete(null) }}
      confirmLoading={busy} cancelButtonProps={{ disabled: busy }} closable={!busy} keyboard={!busy} maskClosable={false}>
      <Typography.Paragraph>将永久删除此 Agent 的所有版本，以及所有成员与它的会话、消息和工具调用记录。</Typography.Paragraph>
      {error && <Alert role="alert" type="error" showIcon message={error} />}
    </Modal>}
  </section>
}
