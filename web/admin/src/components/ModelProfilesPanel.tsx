import axios from 'axios'
import { useEffect, useRef, useState } from 'react'
import { Alert, App, Button, Empty, Form, Input, Modal, Select, Space, Table, Tag, Typography } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { canManageModelAccounts, getModelDeploymentError, listModelDeployments } from '@/api/model'
import { createModelProfile, createModelProfileVersion, getModelProfileError, listModelProfiles, listModelProfileVersions,
  type ModelProfile, type ModelProfileVersion, type ProfileInput } from '@/api/modelProfiles'
import { isSessionValid, useAuthStore } from '@/store/authStore'

export function ModelProfilesPanel({ userID, token, workspaceID, role, onChooseDeployments }: {
  userID: string; token: string; workspaceID: string; role: string; onChooseDeployments: () => void
}) {
  const { message } = App.useApp()
  const qc = useQueryClient()
  const [selected, setSelected] = useState<string | null>(null)
  const [mode, setMode] = useState<'create' | ModelProfile | null>(null)
  const [busy, setBusy] = useState(false)
  const [writeError, setWriteError] = useState('')
  const [form] = Form.useForm<ProfileInput>()
  const mounted = useRef(false)
  const submitting = useRef(false)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  const writable = canManageModelAccounts(role)
  function sameScope() {
    const state = useAuthStore.getState()
    return mounted.current && isSessionValid(state) && state.token === token
      && state.user?.id === userID && state.workspaceId === workspaceID
  }
  const profileKey = ['model-profiles', userID, workspaceID]
  const profiles = useQuery({ queryKey: profileKey, queryFn: ({ signal }) => listModelProfiles(workspaceID, signal), retry: false, refetchOnMount: 'always' })
  const deployments = useQuery({ queryKey: ['model-deployments', userID, workspaceID],
    queryFn: ({ signal }) => listModelDeployments(workspaceID, signal), retry: false, refetchOnMount: 'always' })
  const current = profiles.data?.find(p => p.id === selected) ?? null
  const versionsKey = ['model-profile-versions', userID, workspaceID, selected]
  const versions = useQuery({ queryKey: versionsKey,
    queryFn: ({ signal }) => listModelProfileVersions(workspaceID, selected!, signal),
    enabled: Boolean(current) && profiles.isSuccess, retry: false, refetchOnMount: 'always' })
  const ready = deployments.isSuccess && !deployments.isFetching && deployments.data.length > 0 && profiles.isSuccess
  const names = new Map(deployments.data?.map(d => [d.id, d.name]))
  const displayDeployment = (id: string) => names.get(id) ?? id
  function open(next: 'create' | ModelProfile) {
    form.resetFields()
    form.setFieldsValue({ classification_max: 'internal', fallback_deployment_ids: [] })
    setWriteError('')
    setMode(next)
  }
  function close() {
    if (submitting.current) return
    form.resetFields(); setMode(null); setWriteError('')
  }
  async function submit(values: ProfileInput) {
    if (!mode || !writable || submitting.current || !sameScope()) return
    const ids = [values.primary_deployment_id, ...(values.fallback_deployment_ids ?? [])]
    if (!deployments.isSuccess || !ids.every(id => deployments.data.some(d => d.id === id))) {
      setWriteError('请刷新并选择当前空间的模型部署'); return
    }
    submitting.current = true; setBusy(true); setWriteError('')
    try {
      const input = { primary_deployment_id: values.primary_deployment_id,
        fallback_deployment_ids: values.fallback_deployment_ids ?? [], classification_max: values.classification_max }
      let profileID: string
      if (mode === 'create') {
        const result = await createModelProfile(workspaceID, { ...input, name: values.name.trim(), description: values.description?.trim() ?? '' })
        profileID = result.profile.id
      } else {
        await createModelProfileVersion(workspaceID, mode.id, input)
        profileID = mode.id
      }
      if (!sameScope()) return
      setSelected(profileID); form.resetFields(); setMode(null)
      void message.success(mode === 'create' ? '方案与 v1 已创建' : '新版本已创建')
      void qc.invalidateQueries({ queryKey: profileKey })
      void qc.invalidateQueries({ queryKey: ['model-profile-versions', userID, workspaceID, profileID] })
    } catch (error) {
      if (!sameScope()) return
      setWriteError(getModelProfileError(error, true))
      if (axios.isAxiosError(error) && error.response?.status === 403) void qc.invalidateQueries({ queryKey: ['workspace-context', userID, workspaceID] })
    } finally {
      submitting.current = false
      if (sameScope()) setBusy(false)
    }
  }
  const options = deployments.data?.map(d => ({ value: d.id, label: `${d.name} · ${d.model_name}` })) ?? []
  const timeLabel = (s: string) => Number.isNaN(Date.parse(s)) ? '—' : new Date(s).toLocaleString('zh-CN', { hour12: false })
  return <section aria-labelledby="profiles-heading">
    <div className="model-section-heading"><div><Typography.Title level={3} id="profiles-heading">模型方案</Typography.Title>
      <Typography.Text>以版本固定主部署、备选部署与数据分级上限。创建方案时同时生成 v1。</Typography.Text></div>
      <Space><Button icon={<ReloadOutlined />} loading={profiles.isFetching} onClick={() => { void profiles.refetch(); void deployments.refetch() }}>刷新方案</Button>
        {writable && <Button type="primary" icon={<PlusOutlined />} disabled={!ready} onClick={() => open('create')}>新建方案</Button>}</Space>
    </div>
    {deployments.isError && <Alert role="alert" type="error" showIcon message={getModelDeploymentError(deployments.error)}
      action={<Button onClick={() => void deployments.refetch()}>重试部署</Button>} />}
    {writable && deployments.isSuccess && deployments.data.length === 0 && <Alert type="info" showIcon
      message="请先创建模型部署，再配置模型方案" action={<Button onClick={onChooseDeployments}>前往模型部署</Button>} />}
    {profiles.isError ? <Alert role="alert" type="error" showIcon message={getModelProfileError(profiles.error)}
      action={<Button onClick={() => void profiles.refetch()}>重试</Button>} /> :
      <Table<ModelProfile> rowKey="id" dataSource={profiles.data ?? []} loading={profiles.isPending || profiles.isFetching}
        pagination={{ pageSize: 8, hideOnSinglePage: true }} scroll={{ x: 850 }}
        locale={{ emptyText: <Empty description="当前空间暂无模型方案" /> }}
        columns={[
          { title: '方案名称 / ID', width: 290, render: (_, p) => <Space direction="vertical" size={4}><Typography.Text strong>{p.name}</Typography.Text><Typography.Text className="workspace-id" copyable>{p.id}</Typography.Text></Space> },
          { title: '描述', dataIndex: 'description', ellipsis: true, render: (v: string) => v || '—' },
          { title: '创建时间', dataIndex: 'created_at', width: 185, render: timeLabel },
          { title: '操作', width: 120, render: (_, p) => <Button size="small" type={selected === p.id ? 'primary' : 'default'} onClick={() => setSelected(p.id)} aria-label={`查看版本 ${p.name}`}>查看版本</Button> },
        ]} />}
    <div className="model-section-heading"><div><Typography.Title level={3}>{current ? `${current.name} · 版本` : '方案版本'}</Typography.Title>
      <Typography.Text>已有版本保留原配置，调整部署时创建新版本。</Typography.Text></div>
      {current && <Space><Button icon={<ReloadOutlined />} loading={versions.isFetching} onClick={() => void versions.refetch()}>刷新版本</Button>
        {writable && <Button disabled={!ready || versions.isError || versions.isPending || profiles.isFetching} onClick={() => open(current)}>追加版本</Button>}</Space>}
    </div>
    {!current ? <Empty description="选择上方方案，查看版本与部署配置" /> :
      versions.isError ? <Alert role="alert" type="error" showIcon message={getModelProfileError(versions.error)}
        action={<Button onClick={() => void versions.refetch()}>重试版本</Button>} /> :
        <Table<ModelProfileVersion> rowKey="id" dataSource={versions.data ?? []} loading={versions.isPending || versions.isFetching}
          pagination={{ pageSize: 8, hideOnSinglePage: true }} scroll={{ x: 1050 }}
          columns={[
            { title: '版本 / ID', width: 285, render: (_, v) => <Space direction="vertical" size={4}><Tag color="blue">v{v.version}</Tag><Typography.Text className="workspace-id" copyable>{v.id}</Typography.Text></Space> },
            { title: '主部署', dataIndex: 'primary_deployment_id', width: 160, render: displayDeployment },
            { title: '备选部署（按顺序）', width: 240, render: (_, v) => v.fallback_deployment_ids.length ? v.fallback_deployment_ids.map(displayDeployment).join(' → ') : '无' },
            { title: '数据分级上限', dataIndex: 'classification_max', width: 150 },
            { title: '创建时间', dataIndex: 'created_at', width: 190, render: timeLabel },
          ]} />}
    <Modal title={mode === 'create' ? '新建模型方案' : `为“${mode?.name ?? ''}”追加版本`} open={mode !== null} forceRender
      onCancel={close} onOk={() => form.submit()} okText={mode === 'create' ? '创建方案与 v1' : '创建新版本'} cancelText="取消"
      confirmLoading={busy} cancelButtonProps={{ disabled: busy }} closable={!busy} keyboard={!busy} maskClosable={!busy}>
      <Typography.Paragraph className="user-form-help">选择当前空间的部署。保存配置不会发起模型调用。</Typography.Paragraph>
      {writeError && <Alert className="user-create-error" role="alert" type="error" showIcon message={writeError} />}
      <Form<ProfileInput> form={form} layout="vertical" onFinish={submit} disabled={busy} requiredMark={false} preserve={false}>
        {mode === 'create' && <>
          <Form.Item name="name" label="方案名称" rules={[{ required: true, whitespace: true, message: '请输入方案名称' }]}><Input autoComplete="off" /></Form.Item>
          <Form.Item name="description" label="描述"><Input.TextArea rows={2} /></Form.Item>
        </>}
        <Form.Item name="primary_deployment_id" label="主部署" rules={[{ required: true, message: '请选择主部署' }]}><Select options={options} showSearch optionFilterProp="label" /></Form.Item>
        <Form.Item name="fallback_deployment_ids" label="备选部署" extra="按选择顺序尝试，可不选。"><Select mode="multiple" options={options} optionFilterProp="label" /></Form.Item>
        <Form.Item name="classification_max" label="数据分级上限" rules={[{ required: true, message: '请选择数据分级上限' }]}><Select options={['public', 'internal', 'confidential', 'secret'].map(v => ({ value: v, label: v }))} /></Form.Item>
      </Form>
    </Modal>
  </section>
}
