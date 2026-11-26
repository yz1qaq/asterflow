import axios from 'axios'
import { useCallback, useEffect, useRef, useState } from 'react'
import { PlayCircleOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { Alert, App, Button, Empty, Form, Input, Modal, Popconfirm, Select, Skeleton, Space, Switch, Table, Tag, Typography } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Redirect } from 'wouter'
import { getWorkspaceContext, getWorkspaceSelectionError, isWorkspaceUnavailable } from '@/api/iam'
import {
  createTool, createToolVersion, listTools, listToolVersions, publishToolVersion, testTool, TOOL_SOURCES,
  type CreateToolInput, type CreateToolVersionInput, type Tool, type ToolSource, type ToolTestRun, type ToolVersion,
} from '@/api/tools'
import { isSessionValid, useAuthStore } from '@/store/authStore'

type Scope = { userID: string; workspaceID: string; token: string }

const endpointExamples: Record<ToolSource, string> = {
  rest_api: '{"url":"https://api.example.com/orders","method":"POST"}',
  mcp_server: '{"url":"https://api.example.com/mcp","transport":"streamable_http"}',
  internal_sdk: '{}',
  code_execution: '{}',
  a2a: '{"card_url":"https://api.example.com/.well-known/agent-card.json"}',
}

function accessDenied(error: unknown) {
  return axios.isAxiosError(error) && error.response?.status === 403
}

function toolError(error: unknown) {
  if (axios.isAxiosError<{ error?: unknown; code?: unknown }>(error)) {
    switch (error.response?.status) {
      case 403: return '当前空间权限不足'
      case 404: return '未找到工具，请刷新工具列表'
      case 409:
        return error.response.data?.code === 'tool_version_conflict'
          ? '已有更新的版本，请关闭表单并基于最新版本重新创建'
          : '当前空间已存在同名工具'
      case 400: {
        const detail = error.response.data?.error
        if (typeof detail === 'string') {
          if (detail.includes('endpoint origin changed')) return '端点来源已更改，请填写新凭据，或填 {} 清空鉴权'
          if (!detail.includes('auth_config')) return detail.slice(0, 260)
        }
        return '请检查工具配置'
      }
    }
  }
  return error instanceof Error && !axios.isAxiosError(error) ? error.message : '请求结果未确认，请刷新列表后重试'
}

const jsonObjectRule = {
  async validator(_: unknown, value?: string) {
    if (!value?.trim()) return
    try {
      const parsed: unknown = JSON.parse(value)
      if (parsed !== null && typeof parsed === 'object' && !Array.isArray(parsed)) return
    } catch {
      // 语法错误和类型不符使用同一条字段提示。
    }
    throw new Error('请输入有效的 JSON 对象')
  },
}

function formatJSON(value: string) {
  if (!value) return '{}'
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

function useToolWriteRequest({ userID, token, workspaceID }: Scope) {
  const request = useRef<AbortController | null>(null)
  const inScope = useCallback((state: ReturnType<typeof useAuthStore.getState>) =>
    isSessionValid(state) && state.token === token && state.user?.id === userID && state.workspaceId === workspaceID,
  [token, userID, workspaceID])

  useEffect(() => {
    const unsubscribe = useAuthStore.subscribe(state => {
      if (!inScope(state)) request.current?.abort()
    })
    return () => { unsubscribe(); request.current?.abort() }
  }, [inScope])

  return {
    begin: () => {
      if (request.current || !inScope(useAuthStore.getState())) return undefined
      const controller = new AbortController()
      request.current = controller
      return controller
    },
    finish: (controller: AbortController) => {
      if (request.current === controller) request.current = null
    },
  }
}

export function ToolsPage() {
  const session = useAuthStore()
  if (!isSessionValid(session) || !session.user || !session.token) return <Redirect to="/login" />
  const { user, token, workspaceId } = session
  return <section aria-labelledby="tools-heading">
    <div className="page-heading"><Typography.Title level={2} id="tools-heading">工具管理</Typography.Title></div>
    {workspaceId
      ? <WorkspaceTools key={JSON.stringify([user.id, workspaceId, token])} scope={{ userID: user.id, workspaceID: workspaceId, token }} />
      : <Empty description="请先选择工作空间"><Link href="/workspaces">前往选择空间</Link></Empty>}
  </section>
}

function WorkspaceTools({ scope }: { scope: Scope }) {
  const { userID, workspaceID, token } = scope
  const { message } = App.useApp()
  const [access, setAccess] = useState<'checking' | 'denied' | null>(null)
  const accessCheck = useRef(false)
  const context = useQuery({
    queryKey: ['workspace-context', userID, workspaceID], retry: false, refetchOnMount: 'always',
    queryFn: async ({ signal }) => {
      const result = await getWorkspaceContext(workspaceID, signal)
      if (result.user_id !== userID) throw new Error('当前身份已变化，请重新登录')
      return result
    },
  })
  const unavailable = context.isFetchedAfterMount && isWorkspaceUnavailable(context.error)
  useEffect(() => {
    const state = useAuthStore.getState()
    if (unavailable && state.token === token && state.workspaceId === workspaceID) {
      state.setWorkspace(null)
      void message.warning('该空间已不存在或你已无权访问，请重新选择空间')
    }
  }, [message, token, unavailable, workspaceID])

  const refetchContext = context.refetch
  const currentRole = context.data?.workspace_role
  const recheckRole = useCallback(async (rejected = true) => {
    if (accessCheck.current) return
    accessCheck.current = true
    setAccess('checking')
    try {
      // 操作被拒绝后，先隐藏列表和表单，重新确认服务端记录的空间角色。
      const result = await refetchContext()
      setAccess(rejected && result.isSuccess && result.data.workspace_role === currentRole ? 'denied' : null)
    } finally {
      accessCheck.current = false
    }
  }, [currentRole, refetchContext])

  if (access === 'checking' || !context.isFetchedAfterMount || context.isPending) return <Skeleton active />
  if (access === 'denied' || context.isError) return <Alert showIcon type="error"
    message={context.isError ? getWorkspaceSelectionError(context.error) : '工具访问被拒绝，请重新确认空间权限'}
    action={<Button onClick={() => void recheckRole(false)}>重新确认</Button>} />

  const role = context.data.workspace_role
  const writable = ['owner', 'admin', 'editor'].includes(role)
  return <>
    <div className="workspace-current model-workspace">
      <Space wrap>
        <Typography.Text strong>当前空间</Typography.Text>
        <Typography.Text className="workspace-id" copyable>{workspaceID}</Typography.Text>
        <Tag color="blue">空间角色：{role}</Tag>
      </Space>
    </div>
    <ToolRegistry key={role} scope={scope} writable={writable} recheckRole={recheckRole} />
  </>
}

function ToolRegistry({ scope, writable, recheckRole }: { scope: Scope; writable: boolean; recheckRole: () => void }) {
  const { userID, workspaceID } = scope
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [selectedID, setSelectedID] = useState<string>()
  const [creating, setCreating] = useState(false)
  const [saving, setSaving] = useState(false)
  const [submitError, setSubmitError] = useState<string>()
  const writeRequest = useToolWriteRequest(scope)
  const toolsKey = ['tools', userID, workspaceID]
  const tools = useQuery({ queryKey: toolsKey, retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => listTools(workspaceID, signal) })
  const selected = tools.data?.find(tool => tool.id === selectedID)
  const denied = tools.isFetchedAfterMount && !tools.isFetching && accessDenied(tools.error)
  useEffect(() => {
    if (denied) recheckRole()
  }, [denied, recheckRole])

  async function register(input: CreateToolInput) {
    if (!writable || !creating) return
    const controller = writeRequest.begin()
    if (!controller) return
    setSaving(true)
    setSubmitError(undefined)
    try {
      const item = await createTool(workspaceID, input, controller.signal)
      await queryClient.cancelQueries({ queryKey: toolsKey, exact: true })
      if (controller.signal.aborted) return

      // POST 返回主记录。选中它后，版本组件再向后端读取初始草稿。
      queryClient.setQueryData<Tool[]>(toolsKey, (current = []) => [item, ...current.filter(tool => tool.id !== item.id)])
      setSelectedID(item.id)
      setCreating(false)
      void queryClient.invalidateQueries({ queryKey: toolsKey, exact: true })
      void message.success('工具已注册')
    } catch (error) {
      if (controller.signal.aborted) return
      if (accessDenied(error)) {
        setCreating(false)
        recheckRole()
        void message.error(toolError(error))
      } else {
        setSubmitError(toolError(error))
      }
    } finally {
      writeRequest.finish(controller)
      if (!controller.signal.aborted) setSaving(false)
    }
  }

  if (!tools.isFetchedAfterMount || denied) return <Skeleton active />

  return <>
    <div className="model-section-heading">
      <Typography.Title level={3}>当前空间工具</Typography.Title>
      <Space wrap>
        <Button icon={<ReloadOutlined />} loading={tools.isFetching} onClick={() => void tools.refetch()}>刷新工具</Button>
        {writable && <Button type="primary" icon={<PlusOutlined />} onClick={() => { setSubmitError(undefined); setCreating(true) }}>注册工具</Button>}
      </Space>
    </div>
    {tools.isError ? <Alert showIcon type="error" message={toolError(tools.error)} />
      : <Table<Tool> rowKey="id" size="middle" loading={tools.isPending} dataSource={tools.data ?? []}
        pagination={{ pageSize: 10, hideOnSinglePage: true }}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前空间还没有工具" /> }}
        columns={[
          { title: '工具名称', dataIndex: 'name', render: (name: string, item) => <Space direction="vertical" size={2}>
            <Typography.Text strong>{name}</Typography.Text>
            {item.description && <Typography.Text type="secondary">{item.description}</Typography.Text>}
          </Space> },
          { title: '来源', dataIndex: 'source_type', render: (source: ToolSource) => TOOL_SOURCES[source] },
          { title: '敏感工具', dataIndex: 'sensitive', render: (sensitive: boolean) => sensitive ? <Tag color="orange">是</Tag> : '—' },
          { title: '操作', key: 'actions', render: (_, item) => <Button type="link" aria-pressed={selectedID === item.id}
            onClick={() => setSelectedID(item.id)}>{selectedID === item.id ? '正在查看' : '查看版本'}</Button> },
        ]} />}
    {!tools.isError && selected && <ToolVersions key={selected.id} scope={scope} tool={selected} writable={writable} recheckRole={recheckRole} />}
    {writable && creating && <CreateToolModal saving={saving} error={submitError} onSave={register}
      onClose={() => { setCreating(false); setSubmitError(undefined) }} />}
  </>
}

function ToolVersions({ scope, tool, writable, recheckRole }: {
  scope: Scope; tool: Tool; writable: boolean; recheckRole: () => void
}) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [baseVersion, setBaseVersion] = useState<ToolVersion>()
  const [publishing, setPublishing] = useState<string>()
  const [publishError, setPublishError] = useState<string>()
  const [saving, setSaving] = useState(false)
  const [submitError, setSubmitError] = useState<string>()
  const writeRequest = useToolWriteRequest(scope)
  const versionsKey = ['tool-versions', scope.userID, scope.workspaceID, tool.id]
  const versions = useQuery({ queryKey: versionsKey,
    retry: false, refetchOnMount: 'always', queryFn: ({ signal }) => listToolVersions(scope.workspaceID, tool.id, signal) })
  const latest = versions.data?.[0]
  const denied = versions.isFetchedAfterMount && !versions.isFetching && accessDenied(versions.error)
  useEffect(() => {
    if (denied) recheckRole()
  }, [denied, recheckRole])

  async function appendVersion(input: CreateToolVersionInput) {
    if (!writable || !baseVersion) return
    const controller = writeRequest.begin()
    if (!controller) return

    setSaving(true)
    setSubmitError(undefined)
    try {
      const version = await createToolVersion(scope.workspaceID, tool.id, input, controller.signal)
      await queryClient.cancelQueries({ queryKey: versionsKey, exact: true })
      if (controller.signal.aborted) return

      // 先显示后端返回的新草稿，再回读完整列表。
      queryClient.setQueryData<ToolVersion[]>(versionsKey, (current = []) =>
        [version, ...current.filter(item => item.id !== version.id)].sort((a, b) => b.version - a.version))
      setBaseVersion(undefined)
      void queryClient.invalidateQueries({ queryKey: versionsKey, exact: true })
      void message.success(`v${version.version} 草稿已创建`)
    } catch (error) {
      if (controller.signal.aborted) return
      if (accessDenied(error)) {
        setBaseVersion(undefined)
        recheckRole()
        void message.error(toolError(error))
      } else {
        setSubmitError(toolError(error))
        if (axios.isAxiosError(error) && error.response?.status === 409) {
          void queryClient.invalidateQueries({ queryKey: versionsKey, exact: true })
        }
      }
    } finally {
      writeRequest.finish(controller)
      if (!controller.signal.aborted) setSaving(false)
    }
  }

  async function publish(version: ToolVersion) {
    if (!writable || version.status !== 'draft') return
    const controller = writeRequest.begin()
    if (!controller) return

    setPublishing(version.id)
    setPublishError(undefined)
    try {
      await publishToolVersion(scope.workspaceID, tool.id, version.id, controller.signal)
      await queryClient.cancelQueries({ queryKey: versionsKey, exact: true })
      if (controller.signal.aborted) return

      // 成功响应只改变目标行的状态，后续以版本列表回读为准。
      queryClient.setQueryData<ToolVersion[]>(versionsKey, current =>
        current?.map(item => item.id === version.id ? { ...item, status: 'published' } : item))
      void queryClient.invalidateQueries({ queryKey: versionsKey, exact: true })
      void message.success(`v${version.version} 已发布`)
    } catch (err) {
      if (controller.signal.aborted) return
      if (accessDenied(err)) {
        recheckRole()
        void message.error(toolError(err))
      } else if (axios.isAxiosError<{ error?: string }>(err) && err.response?.status === 409) {
        setPublishError(`v${version.version} 的状态已变化，已重新查询版本列表`)
        void versions.refetch()
      } else {
        const detail = axios.isAxiosError<{ error?: string }>(err) ? err.response?.data?.error : undefined
        let description = toolError(err)
        if (detail === 'no successful test run for tool version') description = '此版本还没有成功的试跑记录'
        if (detail === 'successful tool test run is more than seven days old') description = '成功试跑已超过七天，需要重新试跑'
        setPublishError(`v${version.version} 发布失败，${description}`)
      }
    } finally {
      writeRequest.finish(controller)
      if (!controller.signal.aborted) setPublishing(undefined)
    }
  }

  if (!versions.isFetchedAfterMount || denied) return <Skeleton active />

  return <section className="prompt-detail" aria-labelledby="tool-versions-heading">
    <div className="model-section-heading">
      <div>
        <Typography.Title id="tool-versions-heading" level={3}>{tool.name} · 版本</Typography.Title>
        <Typography.Text type="secondary">工具 ID <Typography.Text code copyable>{tool.id}</Typography.Text></Typography.Text>
      </div>
      <Space wrap>
        <Button icon={<ReloadOutlined />} loading={versions.isFetching} onClick={() => void versions.refetch()}>刷新版本</Button>
        {writable && latest && !versions.isError && <Button icon={<PlusOutlined />} disabled={Boolean(publishing)} onClick={() => {
          setSubmitError(undefined)
          setBaseVersion(latest)
        }}>新建版本</Button>}
      </Space>
    </div>
    {publishError && <Alert showIcon type="error" message={publishError} style={{ marginBottom: 12 }} />}
    {versions.isError ? <Alert showIcon type="error" message={toolError(versions.error)} />
      : <Table<ToolVersion> rowKey="id" size="middle" loading={versions.isPending} dataSource={versions.data ?? []}
        locale={{ expand: '展开版本配置', collapse: '收起版本配置' }}
        pagination={{ pageSize: 10, hideOnSinglePage: true }}
        expandable={{ expandedRowRender: version => <div>
          <Typography.Text strong>参数 Schema</Typography.Text><pre className="prompt-text">{formatJSON(version.schema_json)}</pre>
          <Typography.Text strong>端点配置</Typography.Text><pre className="prompt-text">{formatJSON(version.endpoint_config)}</pre>
          <Typography.Text strong>重试策略</Typography.Text><pre className="prompt-text">{formatJSON(version.retry_policy)}</pre>
        </div> }}
        columns={[
          { title: '版本', dataIndex: 'version', render: (version: number) => <Typography.Text strong>v{version}</Typography.Text> },
          { title: '状态', dataIndex: 'status', render: (status: string) => (
            <Tag color={status === 'published' ? 'green' : 'default'}>{status === 'draft' ? '草稿' : status === 'published' ? '已发布' : status}</Tag>
          ) },
          { title: '版本 ID', dataIndex: 'id', render: (id: string) => <Typography.Text code copyable>{id}</Typography.Text> },
          { title: '鉴权', dataIndex: 'has_auth', render: (hasAuth: boolean) => hasAuth ? '已配置' : '未配置' },
          { title: '创建时间', dataIndex: 'created_at', render: (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false }) },
          ...(writable ? [{
            title: '操作', key: 'publish', render: (_: unknown, version: ToolVersion) => version.status === 'draft'
              ? <Popconfirm title={`发布 v${version.version}？`} okText="确认发布" cancelText="取消"
                  onConfirm={() => publish(version)} disabled={saving || Boolean(publishing)}>
                  <Button size="small" aria-label={`发布 v${version.version}`} loading={publishing === version.id}
                    disabled={saving || Boolean(publishing)}>发布</Button>
                </Popconfirm>
              : '—',
          }] : []),
        ]} />}
    {writable && latest && !versions.isError && !tool.sensitive &&
      (tool.source_type === 'rest_api' || tool.source_type === 'internal_sdk') &&
      <ToolTrial scope={scope} toolID={tool.id} latest={latest} versions={versions.data ?? []}
        recheckRole={recheckRole} refreshVersions={() => void versions.refetch()} />}
    {writable && baseVersion && <CreateToolVersionModal baseVersion={baseVersion} saving={saving}
      error={submitError} onSave={appendVersion} onClose={() => setBaseVersion(undefined)} />}
  </section>
}

function ToolTrial({ scope, toolID, latest, versions, recheckRole, refreshVersions }: {
  scope: Scope; toolID: string; latest: ToolVersion; versions: ToolVersion[]; recheckRole: () => void; refreshVersions: () => void
}) {
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string>()
  const [result, setResult] = useState<ToolTestRun>()
  const writeRequest = useToolWriteRequest(scope)
  const executedVersion = versions.find(version => version.id === result?.tool_version_id)

  async function run({ input }: { input: string }) {
    const controller = writeRequest.begin()
    if (!controller) return
    setRunning(true)
    setError(undefined)
    setResult(undefined)
    try {
      const trial = await testTool(scope.workspaceID, toolID, input, controller.signal)
      if (controller.signal.aborted) return
      setResult(trial)
      // 服务端返回实际执行的版本。若它刚被追加，回读列表补齐版本号。
      if (!versions.some(version => version.id === trial.tool_version_id)) refreshVersions()
    } catch (err) {
      if (controller.signal.aborted) return
      setError(toolError(err))
      if (accessDenied(err)) recheckRole()
    } finally {
      writeRequest.finish(controller)
      if (!controller.signal.aborted) setRunning(false)
    }
  }

  return <section className="prompt-detail" aria-labelledby="tool-trial-heading">
    <div className="model-section-heading">
      <Typography.Title id="tool-trial-heading" level={3}>工具试跑</Typography.Title>
      <Tag>当前版本 v{latest.version}</Tag>
    </div>
    <Form<{ input: string }> layout="vertical" initialValues={{ input: '{}' }} disabled={running} onFinish={run}>
      <Form.Item name="input" label="输入 JSON" rules={[{ required: true, whitespace: true, message: '请输入工具参数' }]}>
        <Input.TextArea autoSize={{ minRows: 3, maxRows: 10 }} spellCheck={false} className="tool-trial-input" />
      </Form.Item>
      <Button type="primary" htmlType="submit" icon={<PlayCircleOutlined />} loading={running}>开始试跑</Button>
    </Form>
    {error && <Alert className="tool-trial-result" showIcon type="error" message={error} />}
    {result && <div className="tool-trial-result" aria-label="试跑结果">
      <Space wrap role="status">
        <Tag color={result.status === 'success' ? 'green' : 'red'}>{result.status === 'success' ? '试跑成功' : '试跑失败'}</Tag>
        {executedVersion && <Typography.Text strong>执行版本 v{executedVersion.version}</Typography.Text>}
        <Typography.Text type="secondary">版本 ID <Typography.Text code copyable>{result.tool_version_id}</Typography.Text></Typography.Text>
        <Typography.Text type="secondary">{result.latency_ms} ms</Typography.Text>
      </Space>
      {result.error && <Alert className="tool-trial-result" showIcon type="error" message={result.error} />}
      {result.status === 'success' && <pre aria-label="工具输出" className="prompt-text tool-trial-output" tabIndex={0}>
        {result.output || '无输出'}
      </pre>}
    </div>}
  </section>
}

function CreateToolVersionModal({ baseVersion, saving, error, onSave, onClose }: {
  baseVersion: ToolVersion; saving: boolean; error?: string
  onSave: (input: CreateToolVersionInput) => Promise<void>; onClose: () => void
}) {
  const [form] = Form.useForm<CreateToolVersionInput>()
  return <Modal title={`新建版本 · 基于 v${baseVersion.version}`} open width={640} okText="保存草稿" cancelText="取消"
    confirmLoading={saving} cancelButtonProps={{ disabled: saving }} closable={!saving} maskClosable={false} keyboard={!saving}
    onOk={() => form.submit()} onCancel={onClose}>
    {error && <Alert showIcon type="error" message={error} style={{ marginBottom: 16 }} />}
    <Form form={form} layout="vertical" disabled={saving} onFinish={onSave} autoComplete="off"
      initialValues={{
        schema_json: baseVersion.schema_json,
        endpoint_config: baseVersion.endpoint_config,
        retry_policy: baseVersion.retry_policy,
        auth_config: '',
      }}>
      <Form.Item name="schema_json" label="参数 Schema" rules={[jsonObjectRule]}>
        <Input.TextArea rows={4} spellCheck={false} />
      </Form.Item>
      <Form.Item name="endpoint_config" label="端点配置" rules={[jsonObjectRule]}>
        <Input.TextArea rows={3} spellCheck={false} />
      </Form.Item>
      <Form.Item name="retry_policy" label="重试策略" rules={[jsonObjectRule]}>
        <Input.TextArea rows={2} spellCheck={false} />
      </Form.Item>
      <Form.Item name="auth_config" label="鉴权配置" rules={[jsonObjectRule]}
        extra="留空沿用最新版本的凭据，填写 {} 清空。">
        <Input.Password placeholder="填写单行 JSON 对象" autoComplete="off" />
      </Form.Item>
    </Form>
  </Modal>
}

function CreateToolModal({ saving, error, onSave, onClose }: {
  saving: boolean; error?: string; onSave: (input: CreateToolInput) => Promise<void>; onClose: () => void
}) {
  const [form] = Form.useForm<CreateToolInput>()
  const source = Form.useWatch('source_type', form) ?? 'rest_api'
  return <Modal title="注册工具" open width={640} okText="注册" cancelText="取消"
    confirmLoading={saving} cancelButtonProps={{ disabled: saving }} closable={!saving} maskClosable={false} keyboard={!saving}
    onOk={() => form.submit()} onCancel={onClose}>
    {error && <Alert showIcon type="error" message={error} style={{ marginBottom: 16 }} />}
    <Form form={form} layout="vertical" disabled={saving} onFinish={onSave} autoComplete="off"
      initialValues={{ source_type: 'rest_api', schema_json: '{\n  "type": "object"\n}', sensitive: false }}>
      <Form.Item name="name" label="工具名称" rules={[
        { required: true, transform: (value?: string) => value?.trim(), pattern: /^[A-Za-z0-9_-]{1,64}$/, message: '请填写 1–64 个英文字母、数字、下划线或连字符' },
      ]}><Input placeholder="get_order" /></Form.Item>
      <Form.Item name="source_type" label="来源类型" rules={[{ required: true }]}>
        <Select options={Object.entries(TOOL_SOURCES).map(([value, label]) => ({ value, label }))} />
      </Form.Item>
      <Form.Item name="description" label="工具描述"><Input.TextArea rows={2} /></Form.Item>
      <Form.Item name="schema_json" label="参数 Schema" rules={[{ required: true, whitespace: true }, jsonObjectRule]}>
        <Input.TextArea rows={3} spellCheck={false} />
      </Form.Item>
      <Form.Item name="endpoint_config" label="端点配置" rules={[jsonObjectRule]}>
        <Input.TextArea rows={2} spellCheck={false} placeholder={endpointExamples[source]} />
      </Form.Item>
      <Form.Item name="auth_config" label="鉴权配置" rules={[jsonObjectRule]}>
        <Input.Password placeholder="可选，填写单行 JSON 对象" autoComplete="off" />
      </Form.Item>
      <Form.Item name="sensitive" label="敏感工具" valuePropName="checked"><Switch /></Form.Item>
    </Form>
  </Modal>
}
