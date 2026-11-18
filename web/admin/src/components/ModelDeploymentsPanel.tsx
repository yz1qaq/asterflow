import axios from 'axios'
import { useEffect, useRef, useState } from 'react'
import { Alert, App, Button, Empty, Form, Input, InputNumber, Modal, Select, Space, Table, Tag, Typography } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  canManageModelAccounts, createModelDeployment, getModelAccountError, getModelDeploymentError,
  listModelAccounts, listModelDeployments, updateModelDeploymentPricing,
  type CreateModelDeploymentRequest, type DeploymentPricing, type ModelDeployment,
} from '@/api/model'
import { isSessionValid, useAuthStore } from '@/store/authStore'

const priceFields = [
  { key: 'input_price_per_million', label: '输入价 / 百万 token' },
  { key: 'output_price_per_million', label: '输出价 / 百万 token' },
  { key: 'cached_input_price_per_million', label: '缓存输入价 / 百万 token' },
] as const

export function ModelDeploymentsPanel({ userID, token, workspaceID, role, onChooseAccounts }: {
  userID: string; token: string; workspaceID: string; role: string; onChooseAccounts: () => void
}) {
  const { message } = App.useApp()
  const qc = useQueryClient()
  const [form] = Form.useForm<CreateModelDeploymentRequest>()
  const [mode, setMode] = useState<'create' | ModelDeployment | null>(null)
  const [busy, setBusy] = useState(false)
  const [writeError, setWriteError] = useState('')
  const submitting = useRef(false)
  const mounted = useRef(false)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  const writable = canManageModelAccounts(role)
  function sameScope() {
    const state = useAuthStore.getState()
    return mounted.current && isSessionValid(state) && state.token === token
      && state.user?.id === userID && state.workspaceId === workspaceID
  }
  const queryKey = ['model-deployments', userID, workspaceID]
  const query = useQuery({
    queryKey, queryFn: ({ signal }) => listModelDeployments(workspaceID, signal),
    retry: false, refetchOnMount: 'always',
  })
  const accounts = useQuery({
    queryKey: ['model-accounts', userID, workspaceID],
    queryFn: ({ signal }) => listModelAccounts(workspaceID, signal),
    retry: false, refetchOnMount: 'always',
  })
  const accountNames = new Map(accounts.data?.map(account => [account.id, account.name]))
  const canCreate = writable && accounts.isSuccess && !accounts.isFetching && accounts.data.length > 0 && !query.isError
  function openForm(next: 'create' | ModelDeployment) {
    form.resetFields()
    if (next !== 'create') form.setFieldsValue({
      input_price_per_million: next.input_price_per_million,
      output_price_per_million: next.output_price_per_million,
      cached_input_price_per_million: next.cached_input_price_per_million,
    })
    setWriteError('')
    setMode(next)
  }
  function closeForm() {
    if (submitting.current) return
    form.resetFields()
    setWriteError('')
    setMode(null)
  }
  async function submit(values: CreateModelDeploymentRequest) {
    if (submitting.current || !mode || !writable || !sameScope()) return
    if (mode === 'create' && (!accounts.isSuccess || !accounts.data.some(account => account.id === values.provider_account_id))) {
      setWriteError('请重新加载并选择当前空间的供应商账户')
      return
    }
    const prices: DeploymentPricing = {
      input_price_per_million: values.input_price_per_million,
      output_price_per_million: values.output_price_per_million,
      cached_input_price_per_million: values.cached_input_price_per_million,
    }
    submitting.current = true
    setBusy(true)
    setWriteError('')
    try {
      if (mode === 'create') {
        await createModelDeployment(workspaceID, {
          ...prices, provider_account_id: values.provider_account_id,
          name: values.name.trim(), model_name: values.model_name.trim(),
          region: values.region?.trim() ?? '', timeout_ms: values.timeout_ms, max_retries: values.max_retries,
        })
      } else {
        await updateModelDeploymentPricing(workspaceID, mode.id, prices)
      }
      if (!sameScope()) return
      form.resetFields()
      setMode(null)
      void message.success(mode === 'create' ? '模型部署已创建' : '部署价格已更新')
      void qc.invalidateQueries({ queryKey })
    } catch (error) {
      if (!sameScope()) return
      setWriteError(getModelDeploymentError(error, true))
      if (axios.isAxiosError(error) && error.response?.status === 403) {
        void qc.invalidateQueries({ queryKey: ['workspace-context', userID, workspaceID] })
      }
    } finally {
      submitting.current = false
      if (sameScope()) setBusy(false)
    }
  }
  const timeLabel = (value: string) => Number.isNaN(Date.parse(value)) ? '—' : new Date(value).toLocaleString('zh-CN', { hour12: false })
  return <section aria-labelledby="deployments-heading">
    <div className="model-section-heading"><div><Typography.Title level={3} id="deployments-heading">模型部署</Typography.Title>
      <Typography.Text>将供应商账户关联到具体模型，并配置调用参数和每百万 token 的单价。</Typography.Text></div>
      <Space><Button icon={<ReloadOutlined />} loading={query.isFetching || accounts.isFetching} onClick={() => { void query.refetch(); void accounts.refetch() }}>刷新部署</Button>
        {writable && <Button type="primary" icon={<PlusOutlined />} disabled={!canCreate} onClick={() => openForm('create')}>新建部署</Button>}</Space></div>
    {accounts.isError && <Alert className="user-create-error" type="error" role="alert" showIcon message={getModelAccountError(accounts.error)}
      action={<Button onClick={() => void accounts.refetch()}>重新加载账户</Button>} />}
    {accounts.isSuccess && accounts.data.length === 0 && <Alert className="user-create-error" type="info" showIcon
      message={writable ? '请先创建当前空间的供应商账户，再创建模型部署' : '当前空间尚未配置供应商账户'}
      action={writable ? <Button onClick={onChooseAccounts}>前往账户配置</Button> : undefined} />}
    {query.isError ? <Alert role="alert" type="error" showIcon message={getModelDeploymentError(query.error)}
      action={<Button onClick={() => void query.refetch()}>重试</Button>} />
      : <Table<ModelDeployment> rowKey="id" dataSource={query.data ?? []} loading={query.isPending || query.isFetching}
        pagination={{ pageSize: 10, hideOnSinglePage: true }} scroll={{ x: 1180 }}
        locale={{ emptyText: <Empty description="当前空间暂无模型部署" /> }}
        columns={[
          { title: '部署名称 / ID', width: 230, render: (_, row) => <Space direction="vertical" size={4}><Typography.Text strong>{row.name}</Typography.Text><Typography.Text className="workspace-id" copyable>{row.id}</Typography.Text></Space> },
          { title: '模型 / 地域', width: 155, render: (_, row) => <Space direction="vertical" size={4}><Typography.Text>{row.model_name}</Typography.Text><Typography.Text>{row.region || '未指定地域'}</Typography.Text></Space> },
          { title: '供应商账户', width: 150, render: (_, row) => <Typography.Text className="workspace-id" title={row.provider_account_id}>{accountNames.get(row.provider_account_id) ?? row.provider_account_id}</Typography.Text> },
          { title: '超时 / 重试', width: 130, render: (_, row) => <Space direction="vertical" size={4}><span>{row.timeout_ms} ms</span><span>{row.max_retries} 次重试</span></Space> },
          { title: '单价 / 百万 token', width: 170, render: (_, row) => <div className="deployment-prices"><span>输入：{row.input_price_per_million}</span><span>输出：{row.output_price_per_million}</span><span>缓存输入：{row.cached_input_price_per_million}</span></div> },
          { title: '状态', width: 80, dataIndex: 'status', render: (status: string) => <Tag color={status === 'active' ? 'success' : 'default'}>{status === 'active' ? '启用' : status}</Tag> },
          { title: '更新时间', dataIndex: 'updated_at', width: 170, render: timeLabel },
          { title: '操作', width: 110, render: (_, row) => writable ? <Button size="small" aria-label={`编辑价格 ${row.name}`} onClick={() => openForm(row)}>编辑价格</Button> : '只读' },
        ]} />}
    <Modal title={mode === 'create' ? '新建模型部署' : '编辑部署价格'} width={720} open={mode !== null} forceRender
      onCancel={closeForm} onOk={() => form.submit()} okText={mode === 'create' ? '创建部署' : '保存价格'} cancelText="取消"
      confirmLoading={busy} cancelButtonProps={{ disabled: busy }} closable={!busy} keyboard={!busy} maskClosable={!busy}>
      <Typography.Paragraph className="user-form-help">{mode === 'create'
        ? '选择供应商账户，并填写供应商实际支持的模型标识。保存部署配置不会发起模型调用。'
        : `修改“${typeof mode === 'object' && mode ? mode.name : ''}”的三项单价；0 是有效价格。`}</Typography.Paragraph>
      {writeError && <Alert role="alert" className="user-create-error" type="error" showIcon message={writeError} />}
      <Form<CreateModelDeploymentRequest> form={form} layout="vertical" onFinish={submit} disabled={busy} preserve={false} requiredMark={false}
        initialValues={{ region: '', timeout_ms: 120000, max_retries: 0, input_price_per_million: 0, output_price_per_million: 0, cached_input_price_per_million: 0 }}>
        {mode === 'create' && <>
          <Form.Item name="provider_account_id" label="供应商账户" rules={[{ required: true, message: '请选择当前空间的账户' }]}>
            <Select showSearch optionFilterProp="label" loading={accounts.isFetching} options={accounts.data?.map(account => ({ value: account.id, label: account.name }))} />
          </Form.Item>
          <div className="deployment-form-grid">
            <Form.Item name="name" label="部署名称" rules={[{ required: true, whitespace: true, message: '请输入部署名称' }]}><Input autoComplete="off" placeholder="例如：客服主模型" /></Form.Item>
            <Form.Item name="model_name" label="模型标识" rules={[{ required: true, whitespace: true, message: '请输入供应商支持的模型标识' }]}><Input autoComplete="off" /></Form.Item>
          </div>
          <Form.Item name="region" label="地域（可选）"><Input autoComplete="off" /></Form.Item>
          <div className="deployment-form-grid">
            <Form.Item name="timeout_ms" label="超时（毫秒）" rules={[{ required: true, type: 'number', min: 1, max: 2147483647, message: '请输入有效的正整数超时' }]}><InputNumber min={1} max={2147483647} precision={0} /></Form.Item>
            <Form.Item name="max_retries" label="最大重试次数" rules={[{ required: true, type: 'number', min: 0, max: 2147483647, message: '请输入非负整数' }]}><InputNumber min={0} max={2147483647} precision={0} /></Form.Item>
          </div>
        </>}
        <Typography.Paragraph className="user-form-help">以下三项均为每百万 token 的单价。</Typography.Paragraph>
        <div className="deployment-price-grid">{priceFields.map(field => <Form.Item key={field.key} name={field.key} label={field.label}
          rules={[{ required: true, type: 'number', min: 0, message: '请输入非负价格' }, {
            validator: (_, value: number) => Number.isFinite(value) ? Promise.resolve() : Promise.reject(new Error('价格必须是有限数字')),
          }]}><InputNumber min={0} step={0.01} /></Form.Item>)}</div>
      </Form>
    </Modal>
  </section>
}
