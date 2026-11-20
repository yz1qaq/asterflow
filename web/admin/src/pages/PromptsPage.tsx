import axios from 'axios'
import { useEffect, useRef, useState } from 'react'
import { Alert, App, Button, Empty, Form, Input, InputNumber, Modal, Popconfirm, Select, Skeleton, Space, Table, Tabs, Tag, Typography } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Redirect, useLocation } from 'wouter'
import { getWorkspaceContext, getWorkspaceSelectionError, isWorkspaceUnavailable } from '@/api/iam'
import {
  canWritePrompts, completeRollout, createPrompt, createPromptVersion, diffPromptVersions, getActiveExperiment,
  getPromptError, listEnvEvents, listPromptEnvs, listPrompts, listPromptVersions, listRolloutEvents, parsePromptVariables,
  promotePromptVersion, renderPrompt, rollbackPromptVersion, rollbackRollout, startExperiment, startRollout,
  updateRolloutTraffic, PROMPT_ENVS,
  type CreatePromptInput, type Prompt, type PromptExperiment, type PromptExperimentVariant, type PromptVersion,
} from '@/api/prompts'
import { isSessionValid, useAuthStore } from '@/store/authStore'

type Scope = { userID: string; token: string; workspaceID: string }
type PromptRequest = <T>(signal: AbortSignal, run: () => Promise<T>) => Promise<T>
const contextKey = ({ userID, workspaceID }: Scope) => ['workspace-context', userID, workspaceID]
const promptsKey = ({ userID, workspaceID }: Scope) => ['prompts', userID, workspaceID]
const versionsKey = ({ userID, workspaceID }: Scope, promptID: string) => ['prompt-versions', userID, workspaceID, promptID]
const envsKey = ({ userID, workspaceID }: Scope, promptID: string) => ['prompt-envs', userID, workspaceID, promptID]
const experimentKey = ({ userID, workspaceID }: Scope, promptID: string, env: string) => ['prompt-experiment', userID, workspaceID, promptID, env]
const eventsKey = ({ userID, workspaceID }: Scope, promptID: string, experimentID: string) => ['prompt-events', userID, workspaceID, promptID, experimentID]
const envEventsKey = ({ userID, workspaceID }: Scope, promptID: string, env: string) => ['prompt-env-events', userID, workspaceID, promptID, env]
const timeLabel = (value: string) => Number.isNaN(Date.parse(value)) ? '—' : new Date(value).toLocaleString('zh-CN', { hour12: false })
const diffLineClass = (line: string) => line.startsWith('+ ') ? 'diff-add'
  : line.startsWith('- ') ? 'diff-del' : line.startsWith('--- ') ? 'diff-file' : 'diff-same'

// Unlike queries, form requests are not canceled by React Query on unmount.
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

export function PromptsPage() {
  const session = useAuthStore()
  const [, navigate] = useLocation()
  if (!isSessionValid(session) || !session.user || !session.token) return <Redirect to="/login" />
  return <section aria-labelledby="prompts-heading">
    <div className="page-heading"><div><Typography.Title level={2} id="prompts-heading">提示词管理</Typography.Title>
      <Typography.Paragraph className="page-description">管理当前空间的提示词和不可变版本。指定版本预览仅渲染文本，不调用模型、不发布，也不更改环境绑定。</Typography.Paragraph></div></div>
    {session.workspaceId ? <WorkspacePrompts key={`${session.token}:${session.user.id}:${session.workspaceId}`}
      scope={{ userID: session.user.id, token: session.token, workspaceID: session.workspaceId }} />
      : <Empty description="请先选择工作空间"><Button type="primary" onClick={() => navigate('/workspaces')}>前往选择空间</Button></Empty>}
  </section>
}

function WorkspacePrompts({ scope }: { scope: Scope }) {
  const { message } = App.useApp()
  const { userID, token, workspaceID } = scope
  const [access, setAccess] = useState<'checking' | 'denied' | null>(null)
  const accessCheckInFlight = useRef(false)
  const context = useQuery({
    queryKey: contextKey(scope),
    queryFn: async ({ signal }) => {
      const result = await getWorkspaceContext(workspaceID, signal)
      if (result.user_id !== userID) throw new Error('当前身份已变化，请重新登录')
      return result
    },
    retry: false, refetchOnMount: 'always', refetchOnWindowFocus: true,
  })
  useEffect(() => {
    const current = useAuthStore.getState()
    if (context.isFetchedAfterMount && isWorkspaceUnavailable(context.error) && current.token === token
      && current.user?.id === userID && current.workspaceId === workspaceID) {
      current.setWorkspace(null)
      void message.warning(getWorkspaceSelectionError(context.error))
    }
  }, [context.error, context.isFetchedAfterMount, token, userID, workspaceID, message])
  async function recheckAccess(deniedRole?: string) {
    if (accessCheckInFlight.current) return
    accessCheckInFlight.current = true
    setAccess('checking')
    try {
      const result = await context.refetch()
      // If the role did not change, do not remount and repeat an automatically denied list request.
      setAccess(result.isSuccess && result.data.workspace_role === deniedRole ? 'denied' : null)
    } finally {
      accessCheckInFlight.current = false
    }
  }
  const request: PromptRequest = async (signal, run) => {
    try {
      return await run()
    } catch (error) {
      if (!signal.aborted && axios.isAxiosError(error) && error.response?.status === 403) {
        void recheckAccess(context.data?.workspace_role)
      }
      throw error
    }
  }
  if (access === 'checking' || !context.isFetchedAfterMount || context.isPending) return <div aria-label="正在确认空间权限"><Skeleton active /></div>
  if (access === 'denied' || context.isError || context.data?.user_id !== userID) return <Alert role="alert" type="error" showIcon
    message={context.isError ? getWorkspaceSelectionError(context.error) : '当前无法访问提示词，请重新确认空间权限'}
    action={<Button onClick={() => void recheckAccess()}>重新确认</Button>} />
  const role = context.data.workspace_role
  const writable = canWritePrompts(role)
  return <>
    <div className="workspace-current model-workspace" aria-label="提示词管理当前空间"><Space wrap>
      <Typography.Text strong>当前空间</Typography.Text><Typography.Text className="workspace-id" copyable>{workspaceID}</Typography.Text>
      <Tag color="blue">空间角色：{role}</Tag>
      {!writable && <Typography.Text>当前角色仅可查看提示词与版本</Typography.Text>}
    </Space></div>
    <PromptPanel key={role} scope={scope} writable={writable} request={request} />
  </>
}

type PanelProps = { scope: Scope; writable: boolean; request: PromptRequest }

function PromptPanel({ scope, writable, request }: PanelProps) {
  const { message } = App.useApp()
  const qc = useQueryClient()
  const [selected, setSelected] = useState<string | null>(null)
  const [mode, setMode] = useState<'create' | Prompt | null>(null)
  const [form] = Form.useForm<CreatePromptInput>()
  const [busy, setBusy] = useState(false)
  const [writeError, setWriteError] = useState('')
  const submitting = useRef(false)
  const formSignal = useFormRequest(scope)
  const { workspaceID } = scope
  const prompts = useQuery({ queryKey: promptsKey(scope), queryFn: ({ signal }) => request(signal, () => listPrompts(workspaceID, signal)),
    retry: false, refetchOnMount: 'always' })
  const current = prompts.isSuccess ? prompts.data.find(prompt => prompt.id === selected) : undefined

  function open(next: 'create' | Prompt) {
    form.resetFields()
    setWriteError('')
    setMode(next)
  }
  function close() {
    if (submitting.current) return
    form.resetFields()
    setWriteError('')
    setMode(null)
  }
  async function save(values: CreatePromptInput) {
    if (!mode || !writable || submitting.current || !prompts.isSuccess) return
    const signal = formSignal()
    if (!signal) return
    submitting.current = true
    setBusy(true)
    setWriteError('')
    try {
      const input = { template: values.template, variables_schema: values.variables_schema }
      const version = await request(signal, async () => mode === 'create'
        ? (await createPrompt(workspaceID, { ...input, name: values.name.trim(), category: values.category?.trim() }, signal)).version
        : createPromptVersion(workspaceID, mode.id, input, signal))
      if (signal.aborted) return
      setSelected(version.prompt_id)
      form.resetFields()
      setMode(null)
      void message.success(mode === 'create' ? '提示词与 v1 已创建，dev 初始指向 v1' : `v${version.version} 已保存，未发布`)
      if (mode === 'create') void qc.invalidateQueries({ queryKey: promptsKey(scope) })
      void qc.invalidateQueries({ queryKey: versionsKey(scope, version.prompt_id) })
    } catch (error) {
      if (signal.aborted) return
      setWriteError(getPromptError(error, 'save'))
    } finally {
      submitting.current = false
      if (!signal.aborted) setBusy(false)
    }
  }

  // After an access check remounts the panel, cached success is not a fresh list response.
  if (!prompts.isFetchedAfterMount) return <div aria-label="正在读取提示词"><Skeleton active /></div>
  return <>
    <div className="model-section-heading"><Typography.Title level={3}>提示词</Typography.Title>
      <Space wrap><Button icon={<ReloadOutlined />} loading={prompts.isFetching} onClick={() => void prompts.refetch()}>刷新提示词</Button>
        {writable && <Button type="primary" icon={<PlusOutlined />} disabled={!prompts.isSuccess} onClick={() => open('create')}>新建提示词</Button>}</Space></div>
    {prompts.isError ? <Alert role="alert" type="error" showIcon message={getPromptError(prompts.error, 'read')}
      action={<Button onClick={() => void prompts.refetch()}>重试</Button>} />
      : <Table<Prompt> rowKey="id" dataSource={prompts.data ?? []} loading={prompts.isFetching} scroll={{ x: 660 }}
        pagination={{ pageSize: 8, hideOnSinglePage: true }} locale={{ emptyText: <Empty description="当前空间暂无提示词" /> }}
        columns={[
          { title: '名称 / ID', render: (_, row) => <Space direction="vertical" size={4}><Typography.Text strong>{row.name}</Typography.Text>
            <Typography.Text className="workspace-id" copyable>{row.id}</Typography.Text></Space> },
          { title: '分类', dataIndex: 'category', width: 160, ellipsis: true, render: (value: string) => value || '—' },
          { title: '创建时间', dataIndex: 'created_at', width: 185, render: timeLabel },
          { title: '操作', width: 120, render: (_, row) => <Button size="small" type={selected === row.id ? 'primary' : 'default'}
            onClick={() => setSelected(row.id)} aria-label={`查看版本 ${row.name}`}>查看版本</Button> },
        ]} />}
    {current ? <PromptHistory key={current.id} scope={scope} prompt={current} writable={writable} request={request} onAppend={() => open(current)} />
      : prompts.isSuccess && prompts.data.length > 0 && <Empty description="选择上方提示词，查看版本历史" />}
    {writable && <Modal title={mode === 'create' ? '新建提示词' : `为“${mode?.name ?? ''}”追加版本`} open={mode !== null} forceRender
      onCancel={close} onOk={() => form.submit()} okText={mode === 'create' ? '创建提示词与 v1' : '保存新版本'} cancelText="取消"
      confirmLoading={busy} cancelButtonProps={{ disabled: busy }} closable={!busy} keyboard={!busy} maskClosable={!busy}>
      <Typography.Paragraph className="user-form-help">{mode === 'create' ? '创建时同时保存 v1，dev 初始指向 v1；不会修改 staging 或 prod。'
        : '保存为独立的新版本，不会发布或修改任何环境绑定。'}</Typography.Paragraph>
      {writeError && <Alert className="user-create-error" role="alert" type="error" showIcon message={writeError} />}
      <Form<CreatePromptInput> form={form} layout="vertical" onFinish={save} disabled={busy} preserve={false}>
        {mode === 'create' && <>
          <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true, message: '请输入提示词名称' }]}><Input /></Form.Item>
          <Form.Item name="category" label="分类（可选）"><Input /></Form.Item>
        </>}
        <Form.Item name="template" label="模板" rules={[{ required: true, message: '请输入模板' }]}
          extra={'支持 Go 模板语法，例如 {{.order_id}}。'}><Input.TextArea rows={5} /></Form.Item>
        <Form.Item name="variables_schema" label="变量 Schema（可选）"
          extra={'输入 JSON 原文，作为字符串保存；留空使用 {}。例如 {"required":["order_id"]}。'}><Input.TextArea rows={3} /></Form.Item>
      </Form>
    </Modal>}
  </>
}

function PromptHistory({ scope, prompt, writable, request, onAppend }: PanelProps & { prompt: Prompt; onAppend: () => void }) {
  const { message } = App.useApp()
  const qc = useQueryClient()
  const [selectedVersion, setSelectedVersion] = useState<string | null>(null)
  const [publishTarget, setPublishTarget] = useState<{ version?: PromptVersion; env?: string } | null>(null)
  const [publishError, setPublishError] = useState('')
  const [publishing, setPublishing] = useState(false)
  const [rollingBack, setRollingBack] = useState<string | null>(null)
  const [diffFrom, setDiffFrom] = useState<number | undefined>()
  const [diffTo, setDiffTo] = useState<number | undefined>()
  const [diffResult, setDiffResult] = useState<{ from: number; to: number; text: string } | null>(null)
  const [diffError, setDiffError] = useState('')
  const [diffBusy, setDiffBusy] = useState(false)
  const diffBusyRef = useRef(false)
  const [expEnv, setExpEnv] = useState<string>('dev')
  const [candidateID, setCandidateID] = useState<string | undefined>()
  const [startTraffic, setStartTraffic] = useState<number>(20)
  const [trafficDraft, setTrafficDraft] = useState<number | null>(null)
  const [experimentError, setExperimentError] = useState('')
  const [experimentBusy, setExperimentBusy] = useState<string | null>(null)
  const [lastExperiment, setLastExperiment] = useState<PromptExperiment | null>(null)
  const [variantOpen, setVariantOpen] = useState(false)
  const [variantRows, setVariantRows] = useState<{ versionID?: string; traffic: number }[]>([{ traffic: 50 }, { traffic: 50 }])
  const experimentBusyRef = useRef(false)
  const active = useQuery({ queryKey: experimentKey(scope, prompt.id, expEnv),
    queryFn: ({ signal }) => request(signal, () => getActiveExperiment(scope.workspaceID, prompt.id, expEnv, signal)),
    retry: false, refetchOnMount: 'always' })
  const [publishForm] = Form.useForm<{ env?: string; version_id?: string }>()
  const publishingRef = useRef(false)
  const formSignal = useFormRequest(scope)
  const history = useQuery({ queryKey: versionsKey(scope, prompt.id),
    queryFn: ({ signal }) => request(signal, () => listPromptVersions(scope.workspaceID, prompt.id, signal)), retry: false, refetchOnMount: 'always' })
  const envs = useQuery({ queryKey: envsKey(scope, prompt.id),
    queryFn: ({ signal }) => request(signal, () => listPromptEnvs(scope.workspaceID, prompt.id, signal)), retry: false, refetchOnMount: 'always' })
  if (!history.isFetchedAfterMount) return <div aria-label="正在读取版本"><Skeleton active /></div>
  const version = history.isSuccess ? history.data.find(row => row.id === selectedVersion) : undefined
  const versionNumber = (id: string) => history.data?.find(row => row.id === id)?.version

  function openPublish(target: PromptVersion) {
    publishForm.resetFields()
    setPublishError('')
    setPublishTarget({ version: target })
  }
  function openPublishEnv(env: string) {
    publishForm.resetFields()
    setPublishError('')
    setPublishTarget({ env })
  }
  function closePublish() {
    if (publishingRef.current) return
    publishForm.resetFields()
    setPublishError('')
    setPublishTarget(null)
  }
  async function publish(values: { env?: string; version_id?: string }) {
    if (!publishTarget || !writable || publishingRef.current) return
    const versionID = publishTarget.version?.id ?? values.version_id
    const env = publishTarget.env ?? values.env
    if (!versionID || !env) return
    const signal = formSignal()
    if (!signal) return
    publishingRef.current = true
    setPublishing(true)
    setPublishError('')
    try {
      await promotePromptVersion(scope.workspaceID, prompt.id, env, versionID, signal)
      if (signal.aborted) return
      const versionLabel = publishTarget.version?.version ?? versionNumber(versionID) ?? versionID.slice(0, 8)
      setPublishTarget(null)
      publishForm.resetFields()
      void message.success(`v${versionLabel} 已发布到 ${env}，其他环境不变`)
      void qc.invalidateQueries({ queryKey: envsKey(scope, prompt.id) })
      void qc.invalidateQueries({ queryKey: ['prompt-env-events', scope.userID, scope.workspaceID, prompt.id] })
    } catch (error) {
      if (signal.aborted) return
      setPublishError(getPromptError(error, 'save'))
    } finally {
      publishingRef.current = false
      if (!signal.aborted) setPublishing(false)
    }
  }
  const versions = history.data ?? []
  const fromVer = diffFrom ?? versions[0]?.version
  const toVer = diffTo ?? versions[versions.length - 1]?.version
  async function showDiff() {
    if (diffBusyRef.current || fromVer === undefined || toVer === undefined) return
    const signal = formSignal()
    if (!signal) return
    diffBusyRef.current = true
    setDiffBusy(true)
    setDiffError('')
    try {
      const text = await request(signal, () => diffPromptVersions(scope.workspaceID, prompt.id, fromVer, toVer, signal))
      if (!signal.aborted) setDiffResult({ from: fromVer, to: toVer, text })
    } catch (error) {
      if (signal.aborted) return
      setDiffResult(null)
      setDiffError(getPromptError(error, 'read'))
    } finally {
      diffBusyRef.current = false
      if (!signal.aborted) setDiffBusy(false)
    }
  }

  async function rollback(env: string, target: PromptVersion) {
    if (!writable || rollingBack) return
    const signal = formSignal()
    if (!signal) return
    setRollingBack(env)
    try {
      await request(signal, () => rollbackPromptVersion(scope.workspaceID, prompt.id, env, target.id, signal))
      if (signal.aborted) return
      void message.success(`${env} 已回滚到 v${target.version}`)
      void qc.invalidateQueries({ queryKey: envsKey(scope, prompt.id) })
      void qc.invalidateQueries({ queryKey: ['prompt-env-events', scope.userID, scope.workspaceID, prompt.id] })
    } catch (error) {
      if (signal.aborted) return
      void message.error(getPromptError(error, 'save'))
    } finally {
      if (!signal.aborted) setRollingBack(null)
    }
  }

  const envBindingID = (envs.data ?? []).find(row => row.env === expEnv)?.version_id
  async function runExperiment(action: string, run: (signal: AbortSignal) => Promise<unknown>): Promise<unknown | null> {
    if (experimentBusyRef.current) return null
    const signal = formSignal()
    if (!signal) return null
    experimentBusyRef.current = true
    setExperimentBusy(action)
    setExperimentError('')
    try {
      const result = await request(signal, () => run(signal))
      if (signal.aborted) return null
      void qc.invalidateQueries({ queryKey: experimentKey(scope, prompt.id, expEnv) })
      void qc.invalidateQueries({ queryKey: envsKey(scope, prompt.id) })
      void qc.invalidateQueries({ queryKey: ['prompt-events', scope.userID, scope.workspaceID, prompt.id] })
      void qc.invalidateQueries({ queryKey: ['prompt-env-events', scope.userID, scope.workspaceID, prompt.id] })
      return result
    } catch (error) {
      if (signal.aborted) return null
      setExperimentError(getPromptError(error, 'save'))
      return null
    } finally {
      experimentBusyRef.current = false
      if (!signal.aborted) setExperimentBusy(null)
    }
  }
  async function startGray() {
    if (!writable || !candidateID) return
    const traffic = startTraffic
    const started = await runExperiment('start', signal => startRollout(scope.workspaceID, prompt.id, expEnv, candidateID, traffic, signal))
    if (started) {
      setLastExperiment(started as PromptExperiment)
      setCandidateID(undefined)
      setTrafficDraft(null)
      void message.success(`${expEnv} 已启动灰度：候选 ${traffic}%`)
    }
  }
  async function saveTraffic(exp: PromptExperiment) {
    const traffic = trafficDraft ?? exp.traffic_percent
    const updated = await runExperiment('traffic', signal => updateRolloutTraffic(scope.workspaceID, prompt.id, expEnv, traffic, signal))
    if (updated) {
      setLastExperiment(updated as PromptExperiment)
      setTrafficDraft(null)
      void message.success(`灰度流量已调整到 ${traffic}%`)
    }
  }
  async function finishRollout(exp: PromptExperiment) {
    if (await runExperiment('complete', signal => completeRollout(scope.workspaceID, prompt.id, expEnv, signal))) {
      setLastExperiment({ ...exp, status: 'completed', traffic_percent: 100, completed_at: new Date().toISOString() })
      setTrafficDraft(null)
      void message.success(`${expEnv} 已完成上线，环境指针指向候选`)
    }
  }
  async function stopExperiment(exp: PromptExperiment) {
    if (await runExperiment('rollback', signal => rollbackRollout(scope.workspaceID, prompt.id, expEnv, signal))) {
      setLastExperiment({ ...exp, status: 'rolled_back', traffic_percent: 0, completed_at: new Date().toISOString() })
      setTrafficDraft(null)
      void message.success(`${expEnv} 实验已停止，环境指针保持不变`)
    }
  }
  function changeExpEnv(value: string) {
    setExpEnv(value)
    setCandidateID(undefined)
    setTrafficDraft(null)
    setLastExperiment(null)
    setVariantOpen(false)
    setExperimentError('')
  }
  function openVariantModal() {
    setVariantRows([
      { versionID: versions[0]?.id, traffic: 50 },
      { versionID: versions[1]?.id ?? versions[0]?.id, traffic: 50 },
    ])
    setExperimentError('')
    setVariantOpen(true)
  }
  function closeVariantModal() {
    if (experimentBusy === 'experiment') return
    setVariantOpen(false)
    setExperimentError('')
  }
  async function startVariantExperiment() {
    if (!writable || variantRows.length === 0) return
    if (variantRows.some(row => !row.versionID)) {
      setExperimentError('每个变体都要选择版本')
      return
    }
    const ids = variantRows.map(row => row.versionID as string)
    if (new Set(ids).size !== ids.length) {
      setExperimentError('同一个版本不能重复作为变体')
      return
    }
    const total = variantRows.reduce((sum, row) => sum + row.traffic, 0)
    if (total <= 0 || total > 100) {
      setExperimentError('变体流量合计必须在 1–100 之间')
      return
    }
    const variants: PromptExperimentVariant[] = variantRows.map(row => ({ version_id: row.versionID as string, traffic: row.traffic }))
    const started = await runExperiment('experiment', signal => startExperiment(scope.workspaceID, prompt.id, expEnv, variants, signal))
    if (started) {
      setLastExperiment(started as PromptExperiment)
      setVariantOpen(false)
      void message.success(`${expEnv} 已启动多变体实验`)
    }
  }

  function experimentCard(exp: PromptExperiment, live: boolean) {
    const candidateNumber = exp.candidate_version_id ? versionNumber(exp.candidate_version_id) : undefined
    const statusLabel = live ? '进行中' : exp.status === 'completed' ? '已完成' : '已停止'
    return <div className="prompt-env-item prompt-experiment-card" aria-label={live ? '当前实验' : '最近一次实验'}>
      <Space wrap><Tag color={live ? 'processing' : 'default'}>{statusLabel}</Tag>
        <Typography.Text strong>{exp.candidate_version_id
          ? `候选 v${candidateNumber ?? exp.candidate_version_id.slice(0, 8)} 占 ${exp.traffic_percent}%`
          : `多变体实验：${exp.variants.map(variant => `v${versionNumber(variant.version_id) ?? variant.version_id.slice(0, 8)} ${variant.traffic}%`).join('，')}`}</Typography.Text></Space>
      <Typography.Text type="secondary">其余流量走 {expEnv} 当前指针；开始于 {timeLabel(exp.started_at)}</Typography.Text>
      {live && writable && exp.candidate_version_id && <Space wrap align="end">
        <InputNumber aria-label="灰度流量" min={1} max={99} value={trafficDraft ?? exp.traffic_percent}
          onChange={value => setTrafficDraft(typeof value === 'number' ? value : null)} />
        <Button loading={experimentBusy === 'traffic'} disabled={experimentBusy !== null} onClick={() => void saveTraffic(exp)}>保存比例</Button>
        <Popconfirm title="完成上线？" description="候选成为该环境基线，全部用户切换。" okText="完成上线" cancelText="取消"
          onConfirm={() => void finishRollout(exp)}>
          <Button type="primary" loading={experimentBusy === 'complete'} disabled={experimentBusy !== null}>完成上线</Button>
        </Popconfirm>
      </Space>}
      {live && writable && <Popconfirm title="停止这个实验？" description="用户回到环境当前指针；指针不变。" okText="停止实验" cancelText="取消"
        onConfirm={() => void stopExperiment(exp)}>
        <Button danger loading={experimentBusy === 'rollback'} disabled={experimentBusy !== null}>停止实验</Button>
      </Popconfirm>}
      <PromptTimeline scope={scope} promptID={prompt.id} experimentID={exp.id} request={request} />
    </div>
  }

  return <section aria-labelledby="prompt-history-heading">
    <div className="model-section-heading"><div><Typography.Title level={3} id="prompt-history-heading">{prompt.name}</Typography.Title>
      <Typography.Text>版本不可修改；发布只移动环境指针；灰度按比例分流到候选版本。</Typography.Text></div>
      <Space wrap><Button icon={<ReloadOutlined />} loading={history.isFetching} onClick={() => void history.refetch()}>刷新</Button>
        {writable && <Button disabled={!history.isSuccess} onClick={onAppend}>追加版本</Button>}</Space></div>
    <Tabs defaultActiveKey="versions" items={[
      {
        key: 'versions', label: '版本',
        children: <>
          {history.isError ? <Alert role="alert" type="error" showIcon message={getPromptError(history.error, 'read')}
            action={<Button onClick={() => void history.refetch()}>重试版本</Button>} />
            : <Table<PromptVersion> rowKey="id" dataSource={history.data ?? []} loading={history.isFetching} scroll={{ x: 660 }}
              pagination={{ pageSize: 8, hideOnSinglePage: true }} locale={{ emptyText: <Empty description="此提示词暂无版本" /> }}
              columns={[
                { title: '版本 / ID', render: (_, row) => <Space direction="vertical" size={4}><Tag color="blue">v{row.version}</Tag>
                  <Typography.Text className="workspace-id" copyable>{row.id}</Typography.Text></Space> },
                { title: '创建时间', dataIndex: 'created_at', width: 185, render: timeLabel },
                { title: '操作', width: 140, render: (_, row) => <Button size="small" type={version?.id === row.id ? 'primary' : 'default'}
                  onClick={() => setSelectedVersion(row.id)} aria-label={`查看 v${row.version} 内容`}>查看内容</Button> },
              ]} />}
          {version && <section aria-label={`v${version.version} 版本内容`} className="prompt-detail">
            <div className="model-section-heading"><div><Typography.Title level={4}>v{version.version} · 版本内容</Typography.Title></div>
              {writable && <Button type="primary" onClick={() => openPublish(version)}>发布此版本</Button>}</div>
            <Typography.Text strong>模板</Typography.Text><pre className="prompt-text">{version.template || '（空文本）'}</pre>
            <Typography.Text strong>变量 Schema</Typography.Text><pre className="prompt-text">{version.variables_schema}</pre>
            {writable && <PromptPreview key={version.id} scope={scope} version={version} request={request} />}
          </section>}
          {versions.length >= 2 && <section aria-label="版本对比" className="prompt-detail">
            <div className="model-section-heading"><div><Typography.Title level={4}>版本对比</Typography.Title>
              <Typography.Text>比较两个版本的模板；变量 Schema 不同时一并显示。</Typography.Text></div></div>
            <Space wrap align="end">
              <Space direction="vertical" size={2}><Typography.Text>起始版本</Typography.Text>
                <Select aria-label="起始版本" style={{ minWidth: 110 }} value={fromVer}
                  onChange={value => { setDiffFrom(value); setDiffResult(null); setDiffError('') }}
                  options={versions.map(row => ({ value: row.version, label: `v${row.version}` }))} /></Space>
              <Space direction="vertical" size={2}><Typography.Text>目标版本</Typography.Text>
                <Select aria-label="目标版本" style={{ minWidth: 110 }} value={toVer}
                  onChange={value => { setDiffTo(value); setDiffResult(null); setDiffError('') }}
                  options={versions.map(row => ({ value: row.version, label: `v${row.version}` }))} /></Space>
              <Button type="primary" loading={diffBusy} onClick={() => void showDiff()}>查看差异</Button>
            </Space>
            {diffError && <Alert role="alert" type="error" showIcon message={diffError} />}
            {diffResult && <div role="status" aria-label={`v${diffResult.from} 到 v${diffResult.to} 差异`}>
              <pre className="prompt-diff">{diffResult.text.split('\n')
                .filter((line, index, lines) => line !== '' || index < lines.length - 1)
                .map((line, index) => <span key={index} className={diffLineClass(line)}>{line}{'\n'}</span>)}</pre>
            </div>}
          </section>}
        </>,
      },
      {
        key: 'envs', label: '环境发布',
        children: <section aria-label="环境指针">
          <Typography.Paragraph className="user-form-help">每个环境各自指向一个版本；发布和回滚只移动该环境指针，不影响版本内容和其他环境。</Typography.Paragraph>
          {!envs.isFetchedAfterMount ? <div aria-label="正在读取环境指针"><Skeleton active paragraph={{ rows: 1 }} /></div>
            : envs.isError ? <Alert role="alert" type="error" showIcon message={getPromptError(envs.error, 'read')}
              action={<Button onClick={() => void envs.refetch()}>重试环境</Button>} />
            : <Space wrap size="middle">
              {PROMPT_ENVS.map(env => {
                const binding = (envs.data ?? []).find(row => row.env === env)
                const boundVersion = binding ? versionNumber(binding.version_id) : undefined
                const previous = boundVersion === undefined ? undefined
                  : (history.data ?? []).filter(row => row.version < boundVersion)
                    .reduce<PromptVersion | undefined>((best, row) => !best || row.version > best.version ? row : best, undefined)
                return <div key={env} className="prompt-env-item" aria-label={`环境 ${env}`}>
                  <Space wrap><Tag color={binding ? 'green' : 'default'}>{env}</Tag>
                    <Typography.Text strong>{binding ? (boundVersion ? `v${boundVersion}` : binding.version_id.slice(0, 8)) : '未发布'}</Typography.Text></Space>
                  {binding && <Typography.Text type="secondary">上次发布 {timeLabel(binding.updated_at)}</Typography.Text>}
                  {writable && <Space wrap>
                    <Button size="small" onClick={() => openPublishEnv(env)}>发布到此环境</Button>
                    {previous && <Popconfirm title={`回滚 ${env} 到 v${previous.version}？`}
                      description="只移动环境指针，不修改版本内容，也不影响其他环境。"
                      okText="回滚" cancelText="取消" onConfirm={() => void rollback(env, previous)}>
                      <Button size="small" loading={rollingBack === env} disabled={rollingBack !== null}>回滚到 v{previous.version}</Button>
                    </Popconfirm>}
                  </Space>}
                </div>
              })}
            </Space>}
          <EnvEventTimeline scope={scope} promptID={prompt.id} request={request} versionNumber={versionNumber} />
        </section>,
      },
      {
        key: 'experiments', label: '实验灰度',
        children: <section aria-label="实验与灰度">
          <Typography.Paragraph className="user-form-help">灰度把候选版本分给一部分用户；只有完成上线才移动环境指针。同一环境同时只有一个进行中的实验。</Typography.Paragraph>
          <Space wrap><Typography.Text>环境</Typography.Text>
            <Select aria-label="实验环境" style={{ minWidth: 110 }} value={expEnv} onChange={changeExpEnv}
              options={PROMPT_ENVS.map(env => ({ value: env, label: env }))} /></Space>
          {!active.isFetchedAfterMount ? <div aria-label="正在读取实验"><Skeleton active paragraph={{ rows: 1 }} /></div>
            : active.isError ? <Alert role="alert" type="error" showIcon message={getPromptError(active.error, 'read')}
              action={<Button onClick={() => void active.refetch()}>重试实验</Button>} />
            : active.data ? experimentCard(active.data, true)
            : <>
              {lastExperiment && experimentCard(lastExperiment, false)}
              <div className="prompt-env-item prompt-experiment-card" aria-label="新实验">
              <Typography.Text>{lastExperiment ? `在 ${expEnv} 启动新的灰度：` : `当前 ${expEnv} 没有进行中的实验。`}</Typography.Text>
              {writable && versions.length >= 2 && <Space wrap align="end">
                <Space direction="vertical" size={2}><Typography.Text>候选版本</Typography.Text>
                  <Select aria-label="候选版本" style={{ minWidth: 130 }} placeholder="选择候选版本" value={candidateID}
                    onChange={setCandidateID}
                    options={versions.filter(row => row.id !== envBindingID).map(row => ({ value: row.id, label: `v${row.version}` }))} /></Space>
                <Space direction="vertical" size={2}><Typography.Text>流量 %</Typography.Text>
                  <InputNumber aria-label="启动流量" min={1} max={99} value={startTraffic}
                    onChange={value => setStartTraffic(typeof value === 'number' ? value : 20)} /></Space>
                <Button type="primary" loading={experimentBusy === 'start'} disabled={!candidateID || experimentBusy !== null}
                  onClick={() => void startGray()}>启动灰度</Button>
                <Button onClick={openVariantModal} disabled={experimentBusy !== null}>启动多变体实验</Button>
              </Space>}
              </div>
            </>}
          {experimentError && !variantOpen && <Alert className="user-create-error" role="alert" type="error" showIcon message={experimentError} />}
        </section>,
      },
    ]} />
    {writable && <Modal title={`在 ${expEnv} 启动多变体实验`} open={variantOpen} forceRender
      onCancel={closeVariantModal} onOk={() => void startVariantExperiment()} okText="启动实验" cancelText="取消"
      confirmLoading={experimentBusy === 'experiment'} cancelButtonProps={{ disabled: experimentBusy !== null }}
      closable={experimentBusy === null} keyboard={experimentBusy === null} maskClosable={experimentBusy === null}>
      <Typography.Paragraph className="user-form-help">
        多个版本按百分比分流；合计 1–100，未覆盖部分走当前环境指针。
      </Typography.Paragraph>
      {experimentError && <Alert className="user-create-error" role="alert" type="error" showIcon message={experimentError} />}
      <Space direction="vertical" size="small" style={{ width: '100%' }}>
        {variantRows.map((row, index) => <Space key={index} wrap>
          <Select aria-label={`变体 ${index + 1} 版本`} style={{ minWidth: 150 }} placeholder="选择版本" value={row.versionID}
            onChange={value => setVariantRows(rows => rows.map((item, i) => i === index ? { ...item, versionID: value } : item))}
            options={versions.map(item => ({ value: item.id, label: `v${item.version}` }))} />
          <InputNumber aria-label={`变体 ${index + 1} 流量`} min={0} max={100} value={row.traffic}
            onChange={value => setVariantRows(rows => rows.map((item, i) => i === index
              ? { ...item, traffic: typeof value === 'number' ? value : 0 } : item))} />
          <Button size="small" danger disabled={variantRows.length <= 1 || experimentBusy !== null}
            onClick={() => setVariantRows(rows => rows.filter((_, i) => i !== index))}>移除</Button>
        </Space>)}
        <Space wrap>
          <Button size="small" disabled={variantRows.length >= 5 || experimentBusy !== null}
            onClick={() => setVariantRows(rows => [...rows, { traffic: 20 }])}>添加变体</Button>
          <Typography.Text type="secondary">合计 {variantRows.reduce((sum, row) => sum + row.traffic, 0)}%</Typography.Text>
        </Space>
      </Space>
    </Modal>}
    {writable && <Modal title="发布版本" open={publishTarget !== null} forceRender
      onCancel={closePublish} onOk={() => publishForm.submit()} okText="发布" cancelText="取消"
      confirmLoading={publishing} cancelButtonProps={{ disabled: publishing }} closable={!publishing} keyboard={!publishing} maskClosable={!publishing}>
      <Typography.Paragraph className="user-form-help">发布后目标环境指针指向所选版本；版本内容不可变，其他环境不受影响。</Typography.Paragraph>
      {publishError && <Alert className="user-create-error" role="alert" type="error" showIcon message={publishError} />}
      <Form form={publishForm} layout="vertical"
        initialValues={{ env: publishTarget?.env ?? 'dev', version_id: publishTarget?.version?.id }}
        onFinish={publish} disabled={publishing} preserve={false}>
        {publishTarget?.version
          ? <Typography.Paragraph className="user-form-help">版本：v{publishTarget.version.version}</Typography.Paragraph>
          : <Form.Item name="version_id" label="版本" rules={[{ required: true, message: '请选择版本' }]}>
            <Select options={versions.map(item => ({ value: item.id, label: `v${item.version}` }))} />
          </Form.Item>}
        {publishTarget?.env
          ? <Typography.Paragraph className="user-form-help">目标环境：{publishTarget.env}</Typography.Paragraph>
          : <Form.Item name="env" label="目标环境" rules={[{ required: true, message: '请选择环境' }]}>
            <Select options={PROMPT_ENVS.map(env => ({ value: env, label: env }))} />
          </Form.Item>}
      </Form>
    </Modal>}
  </section>
}

const envActionLabel: Record<string, string> = { published: '发布', rolled_back: '回滚' }

function EnvEventTimeline({ scope, promptID, request, versionNumber }: {
  scope: Scope; promptID: string; request: PromptRequest; versionNumber: (id: string) => number | undefined
}) {
  const [env, setEnv] = useState('dev')
  const events = useQuery({ queryKey: envEventsKey(scope, promptID, env),
    queryFn: ({ signal }) => request(signal, () => listEnvEvents(scope.workspaceID, promptID, env, signal)),
    retry: false, refetchOnMount: 'always' })
  const versionLabel = (id?: string) => id ? `v${versionNumber(id) ?? id.slice(0, 8)}` : '首次发布'
  return <div className="prompt-timeline prompt-env-history" aria-label="发布记录">
    <Space wrap><Typography.Text strong>发布/回滚记录</Typography.Text>
      <Select aria-label="记录环境" style={{ minWidth: 100 }} value={env} onChange={setEnv}
        options={PROMPT_ENVS.map(item => ({ value: item, label: item }))} />
      <Button size="small" icon={<ReloadOutlined />} loading={events.isFetching} onClick={() => void events.refetch()}>刷新记录</Button></Space>
    {!events.isFetchedAfterMount ? <Skeleton active paragraph={{ rows: 1 }} />
      : events.isError ? <Alert role="alert" type="error" showIcon message={getPromptError(events.error, 'read')} />
      : (events.data ?? []).length === 0 ? <Typography.Text type="secondary">暂无发布记录</Typography.Text>
      : <ul>{(events.data ?? []).map(event => <li key={event.id}>
        <Tag>{envActionLabel[event.action] ?? event.action}</Tag>
        <Typography.Text>{versionLabel(event.from_version_id)} → {versionLabel(event.to_version_id)}</Typography.Text>
        {event.detail && <Typography.Text type="secondary">{event.detail}</Typography.Text>}
        <Typography.Text type="secondary">{timeLabel(event.created_at)} · {event.actor.slice(0, 8)}</Typography.Text>
      </li>)}</ul>}
  </div>
}

const rolloutActionLabel: Record<string, string> = {
  started: '启动实验', traffic_changed: '调整流量', completed: '完成上线', rolled_back: '停止实验',
}

function PromptTimeline({ scope, promptID, experimentID, request }: {
  scope: Scope; promptID: string; experimentID: string; request: PromptRequest
}) {
  const events = useQuery({ queryKey: eventsKey(scope, promptID, experimentID),
    queryFn: ({ signal }) => request(signal, () => listRolloutEvents(scope.workspaceID, promptID, experimentID, signal)),
    retry: false, refetchOnMount: 'always' })
  return <div className="prompt-timeline" aria-label="实验事件">
    <Typography.Text strong>事件时间线</Typography.Text>
    {!events.isFetchedAfterMount ? <Skeleton active paragraph={{ rows: 1 }} />
      : events.isError ? <Alert role="alert" type="error" showIcon message={getPromptError(events.error, 'read')} />
      : (events.data ?? []).length === 0 ? <Typography.Text type="secondary">暂无事件</Typography.Text>
      : <ul>{(events.data ?? []).map(event => <li key={event.id}>
        <Tag>{rolloutActionLabel[event.action] ?? event.action}</Tag>
        <Typography.Text>{event.from_traffic}% → {event.to_traffic}%</Typography.Text>
        {event.detail && <Typography.Text type="secondary">{event.detail}</Typography.Text>}
        <Typography.Text type="secondary">{timeLabel(event.created_at)} · {event.actor.slice(0, 8)}</Typography.Text>
      </li>)}</ul>}
  </div>
}

function PromptPreview({ scope, version, request }: { scope: Scope; version: PromptVersion; request: PromptRequest }) {
  const formSignal = useFormRequest(scope)
  const submitting = useRef(false)
  const [busy, setBusy] = useState(false)
  const [errorText, setErrorText] = useState('')
  const [rendered, setRendered] = useState<string | null>(null)

  function clearResult() { setErrorText(''); setRendered(null) }
  async function preview({ vars }: { vars: string }) {
    const signal = formSignal()
    if (submitting.current || !signal) return
    submitting.current = true
    setBusy(true)
    clearResult()
    try {
      const result = await request(signal, () => renderPrompt(scope.workspaceID, version.prompt_id, version.id, parsePromptVariables(vars), signal))
      if (!signal.aborted) setRendered(result)
    } catch (error) {
      if (signal.aborted) return
      setErrorText(getPromptError(error, 'preview'))
    } finally {
      submitting.current = false
      if (!signal.aborted) setBusy(false)
    }
  }

  return <Form<{ vars: string }> layout="vertical" initialValues={{ vars: '{}' }} onFinish={preview} onValuesChange={clearResult} disabled={busy}>
    <Form.Item name="vars" label="预览变量" extra={'输入 JSON 对象，例如 {"order_id":"ORD-1001"}。预览只渲染当前版本，不会发布。'}>
      <Input.TextArea rows={3} />
    </Form.Item>
    <Form.Item><Button htmlType="submit" loading={busy}>渲染预览 v{version.version}</Button></Form.Item>
    {errorText && <Alert role="alert" type="error" showIcon message={errorText} />}
    {rendered !== null && <div role="status"><Typography.Text strong>预览结果（纯文本）</Typography.Text>
      <pre className="prompt-text">{rendered || '（空文本）'}</pre></div>}
  </Form>
}
