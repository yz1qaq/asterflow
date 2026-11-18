import axios from 'axios'
import { useEffect, useRef, useState } from 'react'
import { Alert, App, Button, Empty, Form, Input, Modal, Select, Skeleton, Space, Table, Tabs, Tag, Typography } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Redirect, useLocation } from 'wouter'
import { getWorkspaceContext, getWorkspaceSelectionError, isWorkspaceUnavailable } from '@/api/iam'
import { canManageModelAccounts, createModelAccount, getModelAccountError, listModelAccounts, rotateModelAccountAPIKey, type CreateModelAccountRequest, type ModelAccount } from '@/api/model'
import { ModelBindingsPanel } from '@/components/ModelBindingsPanel'
import { ModelProfilesPanel } from '@/components/ModelProfilesPanel'
import { ModelDeploymentsPanel } from '@/components/ModelDeploymentsPanel'
import { isSessionValid, useAuthStore } from '@/store/authStore'

export function ModelsPage() {
  const session = useAuthStore()
  const [, navigate] = useLocation()
  if (!isSessionValid(session) || !session.user || !session.token) return <Redirect to="/login" />
  if (!session.workspaceId) return <section aria-labelledby="models-heading">
    <div className="page-heading"><div><Typography.Title level={2} id="models-heading">模型管理</Typography.Title>
      <Typography.Paragraph className="page-description">管理当前空间调用模型所需的供应商账户。</Typography.Paragraph></div></div>
    <Empty description="请先选择工作空间"><Button type="primary" onClick={() => navigate('/workspaces')}>前往选择空间</Button></Empty>
  </section>
  return <WorkspaceModels key={session.token + ':' + session.workspaceId}
    userID={session.user.id} token={session.token} workspaceID={session.workspaceId} />
}

function WorkspaceModels({ userID, token, workspaceID }: { userID: string; token: string; workspaceID: string }) {
  const { message } = App.useApp()
  const [activeTab, setActiveTab] = useState('accounts')
  const context = useQuery({
    queryKey: ['workspace-context', userID, workspaceID],
    queryFn: async ({ signal }) => {
      const result = await getWorkspaceContext(workspaceID, signal)
      if (result.user_id !== userID) throw new Error('当前身份已变化，请重新登录')
      return result
    },
    retry: false, refetchOnMount: 'always', refetchOnWindowFocus: true,
  })
  useEffect(() => {
    const current = useAuthStore.getState()
    if (isWorkspaceUnavailable(context.error) && current.token === token && current.workspaceId === workspaceID) {
      current.setWorkspace(null)
      void message.warning(getWorkspaceSelectionError(context.error))
    }
  }, [context.error, token, workspaceID, message])
  if (context.isPending) return <div aria-label="正在确认空间权限"><Skeleton active /></div>
  if (context.isError || !context.data) return <Alert role="alert" type="error" showIcon message={getWorkspaceSelectionError(context.error)}
    action={<Button onClick={() => void context.refetch()}>重新确认</Button>} />
  return <section aria-labelledby="models-heading">
    <div className="page-heading"><div><Typography.Title level={2} id="models-heading">模型管理</Typography.Title>
      <Typography.Paragraph className="page-description">管理当前空间的供应商账户、模型部署、计价、方案版本与环境绑定。</Typography.Paragraph></div></div>
    <div className="workspace-current model-workspace" aria-label="模型管理当前空间"><Space wrap>
      <Typography.Text strong>当前空间</Typography.Text><Typography.Text className="workspace-id" copyable>{workspaceID}</Typography.Text>
      <Tag color="blue">空间角色：{context.data.workspace_role}</Tag>
      {!canManageModelAccounts(context.data.workspace_role) && <Typography.Text>当前角色仅可查看模型配置</Typography.Text>}
    </Space></div>
    <Tabs className="model-config-tabs" activeKey={activeTab} onChange={setActiveTab} destroyOnHidden items={[
      { key: 'accounts', label: '供应商账户', children: <AccountsPanel userID={userID} token={token} workspaceID={workspaceID} role={context.data.workspace_role} /> },
      { key: 'deployments', label: '模型部署', children: <ModelDeploymentsPanel userID={userID} token={token} workspaceID={workspaceID} role={context.data.workspace_role} onChooseAccounts={() => setActiveTab('accounts')} /> },
      { key: 'profiles', label: '模型方案', children: <ModelProfilesPanel userID={userID} token={token} workspaceID={workspaceID} role={context.data.workspace_role} onChooseDeployments={() => setActiveTab('deployments')} /> },
      { key: 'bindings', label: '环境绑定', children: <ModelBindingsPanel userID={userID} token={token} workspaceID={workspaceID} role={context.data.workspace_role} onChooseProfiles={() => setActiveTab('profiles')} /> },
    ]} />
  </section>
}

function AccountsPanel({ userID, token, workspaceID, role }: { userID: string; token: string; workspaceID: string; role: string }) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [form] = Form.useForm<CreateModelAccountRequest>()
  const [mode, setMode] = useState<'create' | ModelAccount | null>(null)
  const [busy, setBusy] = useState(false)
  const [writeError, setWriteError] = useState('')
  const submitting = useRef(false)
  const mounted = useRef(false)
  const writable = canManageModelAccounts(role)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  function sameScope() {
    const current = useAuthStore.getState()
    return mounted.current && isSessionValid(current) && current.token === token
      && current.user?.id === userID && current.workspaceId === workspaceID
  }
  const queryKey = ['model-accounts', userID, workspaceID]
  const query = useQuery({
    queryKey, queryFn: ({ signal }) => listModelAccounts(workspaceID, signal),
    retry: false, refetchOnMount: 'always',
  })
  function openForm(next: 'create' | ModelAccount) {
    form.resetFields()
    setWriteError('')
    setMode(next)
  }
  function closeForm() {
    if (submitting.current) return
    form.resetFields()
    setWriteError('')
    setMode(null)
  }
  async function submit(values: CreateModelAccountRequest) {
    if (submitting.current || !mode || !writable || !sameScope()) return
    submitting.current = true
    setBusy(true)
    setWriteError('')
    try {
      if (mode === 'create') {
        await createModelAccount(workspaceID, {
          name: values.name.trim(), kind: values.kind, base_url: values.base_url.trim(), api_key: values.api_key,
        })
      } else {
        await rotateModelAccountAPIKey(workspaceID, mode.id, values.api_key)
      }
      if (!sameScope()) return
      form.resetFields()
      setMode(null)
      void message.success(mode === 'create' ? '供应商账户已创建' : 'API Key 已更新')
      void queryClient.invalidateQueries({ queryKey })
    } catch (error) {
      if (!sameScope()) return
      setWriteError(getModelAccountError(error, true))
      if (axios.isAxiosError(error) && error.response?.status === 403) {
        void queryClient.invalidateQueries({ queryKey: ['workspace-context', userID, workspaceID] })
      }
    } finally {
      submitting.current = false
      if (sameScope()) setBusy(false)
    }
  }
  const timeLabel = (value: string) => Number.isNaN(Date.parse(value)) ? '—' : new Date(value).toLocaleString('zh-CN', { hour12: false })

  return <section aria-labelledby="accounts-heading">
    <div className="model-section-heading"><div><Typography.Title level={3} id="accounts-heading">供应商账户</Typography.Title>
      <Typography.Text>API Key 加密保存，仅显示是否已配置；一个账户可供多个模型部署使用。</Typography.Text></div>
      <Space><Button icon={<ReloadOutlined />} loading={query.isFetching} onClick={() => void query.refetch()}>刷新账户</Button>
        {writable && <Button type="primary" icon={<PlusOutlined />} disabled={query.isError} onClick={() => openForm('create')}>新建账户</Button>}</Space></div>
    {query.isError ? <Alert role="alert" type="error" showIcon message={getModelAccountError(query.error)}
      action={<Button onClick={() => void query.refetch()}>重试</Button>} />
      : <Table<ModelAccount> rowKey="id" dataSource={query.data ?? []} loading={query.isPending || query.isFetching}
        pagination={{ pageSize: 10, hideOnSinglePage: true }} scroll={{ x: 1050 }}
        locale={{ emptyText: <Empty description={writable ? '当前空间暂无供应商账户，可以新建账户开始配置' : '当前空间暂无供应商账户'} /> }}
        columns={[
          { title: '账户名称 / ID', width: 250, render: (_, row) => <Space direction="vertical" size={4}><Typography.Text strong>{row.name}</Typography.Text><Typography.Text className="workspace-id" copyable>{row.id}</Typography.Text></Space> },
          { title: '供应商类型', dataIndex: 'kind', width: 150 },
          { title: 'Base URL', dataIndex: 'base_url', width: 220, ellipsis: true },
          { title: 'API Key', dataIndex: 'has_api_key', width: 100, render: (value: boolean) => <Tag color={value ? 'success' : 'default'}>{value ? '已配置' : '未配置'}</Tag> },
          { title: '状态', dataIndex: 'status', width: 80, render: (value: string) => value === 'active' ? <Tag color="success">启用</Tag> : <Tag>{value}</Tag> },
          { title: '更新时间', dataIndex: 'updated_at', width: 175, render: timeLabel },
          { title: '操作', width: 140, render: (_, row) => writable ? <Button size="small" onClick={() => openForm(row)} aria-label={`轮换密钥 ${row.name}`}>轮换密钥</Button> : '只读' },
        ]} />}
    <Modal title={mode === 'create' ? '新建供应商账户' : '轮换 API Key'} open={mode !== null} forceRender
      onCancel={closeForm} onOk={() => form.submit()} okText={mode === 'create' ? '创建账户' : '保存新密钥'} cancelText="取消"
      confirmLoading={busy} cancelButtonProps={{ disabled: busy }} closable={!busy} keyboard={!busy} maskClosable={!busy}>
      <Typography.Paragraph className="user-form-help">{mode === 'create'
        ? '填写供应商接口地址与调用密钥。保存配置不会发起模型调用。'
        : `为“${typeof mode === 'object' && mode ? mode.name : ''}”替换调用密钥，保存后引用此账户的部署将使用新密钥。`}</Typography.Paragraph>
      {writeError && <Alert className="user-create-error" role="alert" type="error" showIcon message={writeError} />}
      <Form<CreateModelAccountRequest> form={form} layout="vertical" onFinish={submit} disabled={busy} requiredMark={false} preserve={false}
        initialValues={{ kind: 'openai-compatible' }}>
        {mode === 'create' && <>
          <Form.Item name="name" label="账户名称" rules={[{ required: true, whitespace: true, message: '请输入账户名称' }]}><Input autoComplete="off" /></Form.Item>
          <Form.Item name="kind" label="供应商类型" rules={[{ required: true, message: '请选择供应商类型' }]}><Select options={['openai-compatible', 'openai', 'deepseek', 'ollama'].map(value => ({ value, label: value }))} /></Form.Item>
          <Form.Item name="base_url" label="Base URL" rules={[{ required: true, whitespace: true, message: '请输入接口地址' }, {
            validator: (_, value: string) => { try { const address = value.trim(); const url = new URL(address); if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || address.includes('?') || url.hash) throw new Error(); return Promise.resolve() } catch { return Promise.reject(new Error('请输入不含账号密码、查询参数或片段的完整 HTTP(S) 地址')) } },
          }]}><Input placeholder="https://api.example.com/v1" autoComplete="off" /></Form.Item>
        </>}
        <Form.Item name="api_key" label={mode === 'create' ? 'API Key' : '新 API Key'} rules={[{ required: true, whitespace: true, message: '请输入 API Key' }]}>
          <Input.Password autoComplete="new-password" />
        </Form.Item>
      </Form>
    </Modal>
  </section>
}
