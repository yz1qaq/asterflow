import axios from 'axios'
import { useEffect, useRef, useState } from 'react'
import { Alert, App, Button, Descriptions, Empty, Form, InputNumber, Modal, Segmented, Select, Skeleton, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { canManageModelAccounts } from '@/api/model'
import { getModelProfileError, listModelProfiles } from '@/api/modelProfiles'
import { getModelBindingError, getProjectBinding, listWorkspaceProfileVersions, maxBindingRate, saveProjectBinding,
  type ModelBindingInput } from '@/api/modelBindings'
import { isSessionValid, useAuthStore } from '@/store/authStore'

const environments = ['dev', 'staging', 'prod'] as const
type Environment = typeof environments[number]
type BindingFields = Omit<ModelBindingInput, 'env'>
type BindingScope = { userID: string; token: string; workspaceID: string; role: string; onChooseProfiles: () => void }

export function ModelBindingsPanel(props: BindingScope) {
  const [env, setEnv] = useState<Environment>('dev')
  const [saving, setSaving] = useState(false)
  return <section aria-labelledby="bindings-heading">
    <div className="model-section-heading">
      <div>
        <Typography.Title level={3} id="bindings-heading">环境绑定</Typography.Title>
        <Typography.Text>为每个环境选择模型方案版本，保存月预算及请求和 Token 限额。</Typography.Text>
      </div>
      <Segmented<Environment> aria-label="绑定环境" options={[...environments]} value={env} onChange={setEnv} disabled={saving} />
    </div>
    <EnvironmentBinding key={env} {...props} env={env} setSaving={setSaving} />
  </section>
}

function EnvironmentBinding({ userID, token, workspaceID, role, onChooseProfiles, env, setSaving }:
  BindingScope & { env: Environment; setSaving: (saving: boolean) => void }) {
  const { message } = App.useApp()
  const qc = useQueryClient()
  const writable = canManageModelAccounts(role)
  const [form] = Form.useForm<BindingFields>()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [writeError, setWriteError] = useState('')
  const mounted = useRef(false)
  const submitting = useRef(false)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; setSaving(false) } }, [setSaving])

  const key = ['model-binding', userID, workspaceID, env]
  const binding = useQuery({ queryKey: key, queryFn: ({ signal }) => getProjectBinding(workspaceID, env, signal),
    retry: false, refetchOnMount: 'always' })
  const profiles = useQuery({ queryKey: ['model-profiles', userID, workspaceID],
    queryFn: ({ signal }) => listModelProfiles(workspaceID, signal), retry: false, refetchOnMount: 'always' })
  const versions = useQuery({ queryKey: ['workspace-model-profile-versions', userID, workspaceID],
    queryFn: ({ signal }) => listWorkspaceProfileVersions(workspaceID, signal), retry: false, refetchOnMount: 'always' })
  const profileNames = new Map(profiles.data?.map(profile => [profile.id, profile.name]))
  const options = (versions.data ?? []).map(version => ({
    value: version.id, label: `${profileNames.get(version.profile_id) ?? version.profile_id} · v${version.version}`,
  }))
  const current = binding.data
  const canEdit = writable && binding.isSuccess && profiles.isSuccess && versions.isSuccess && options.length > 0

  function sameScope() {
    const state = useAuthStore.getState()
    return mounted.current && isSessionValid(state) && state.token === token &&
      state.user?.id === userID && state.workspaceId === workspaceID
  }

  function refresh() {
    void binding.refetch()
    void profiles.refetch()
    void versions.refetch()
  }

  function edit() {
    form.resetFields()
    form.setFieldsValue({
      model_profile_version_id: current?.model_profile_version_id,
      monthly_budget: current?.monthly_budget ?? 0, rpm_limit: current?.rpm_limit ?? 0, tpm_limit: current?.tpm_limit ?? 0,
    })
    setWriteError('')
    setOpen(true)
  }

  async function submit(values: BindingFields) {
    if (!open || !writable || submitting.current || !sameScope()) return
    if (!versions.isSuccess || !options.some(option => option.value === values.model_profile_version_id)) {
      setWriteError('请选择当前空间可用的模型方案版本')
      return
    }
    submitting.current = true
    setBusy(true)
    setSaving(true)
    setWriteError('')
    try {
      const saved = await saveProjectBinding(workspaceID, { ...values, env })
      if (!sameScope()) return
      await qc.cancelQueries({ queryKey: key, exact: true })
      if (!sameScope()) return
      qc.setQueryData(key, saved)
      setOpen(false)
      void message.success(`已保存 ${env} 环境绑定`)
    } catch (error) {
      if (!sameScope()) return
      setWriteError(getModelBindingError(error, true))
      if (axios.isAxiosError(error) && error.response?.status === 403) {
        void qc.invalidateQueries({ queryKey: ['workspace-context', userID, workspaceID] })
      }
    } finally {
      submitting.current = false
      if (sameScope()) { setBusy(false); setSaving(false) }
    }
  }

  const versionLabel = options.find(option => option.value === current?.model_profile_version_id)?.label
  const labelLimit = (value: number) => value === 0 ? '不限（0）' : value.toLocaleString('zh-CN')
  const rateRules = [{ required: true, type: 'integer' as const, min: 0, max: maxBindingRate, message: `请输入 0 至 ${maxBindingRate} 的整数` }]
  return <>
    <div className="model-binding-card">
      <div className="model-section-heading">
        <Typography.Title level={3}>{env} 环境</Typography.Title>
        <div className="model-binding-actions">
          <Button icon={<ReloadOutlined />} loading={binding.isFetching || profiles.isFetching || versions.isFetching} onClick={refresh}>刷新绑定</Button>
          {writable && <Button type="primary" disabled={!canEdit || binding.isFetching || profiles.isFetching || versions.isFetching}
            onClick={edit}>{current ? '编辑绑定' : '配置绑定'}</Button>}
        </div>
      </div>
      {binding.isError ? <Alert role="alert" type="error" showIcon message={getModelBindingError(binding.error)} />
        : binding.isPending ? <Skeleton active />
          : current ? <Descriptions column={2} items={[
            { key: 'version', label: '模型方案版本', children: versionLabel ?? current.model_profile_version_id },
            { key: 'budget', label: '月预算', children: labelLimit(current.monthly_budget) },
            { key: 'rpm', label: 'RPM（每分钟请求数）', children: labelLimit(current.rpm_limit) },
            { key: 'tpm', label: 'TPM（每分钟 Token 数）', children: labelLimit(current.tpm_limit) },
            { key: 'time', label: '更新时间', children: new Date(current.updated_at).toLocaleString('zh-CN', { hour12: false }) },
          ]} /> : <Empty description={`${env} 环境尚未绑定模型方案版本`} />}
      {(profiles.isError || versions.isError) && <Alert role="alert" type="error" showIcon
        message={getModelProfileError(profiles.error ?? versions.error)} />}
      {writable && versions.isSuccess && options.length === 0 && <Alert type="info" showIcon
        message="请先创建模型方案和版本，再配置环境绑定" action={<Button onClick={onChooseProfiles}>前往模型方案</Button>} />}
      <Typography.Text type="secondary">数值为 0 表示不设对应限额。</Typography.Text>
    </div>
    {writable && <Modal title={`配置 ${env} 环境绑定`} open={open} forceRender
      onCancel={() => { if (!submitting.current) setOpen(false) }} onOk={() => form.submit()}
      okText="保存绑定" cancelText="取消" confirmLoading={busy} okButtonProps={{ disabled: !canEdit }}
      cancelButtonProps={{ disabled: busy }} closable={!busy} keyboard={!busy} maskClosable={!busy}>
      {writeError && <Alert className="user-create-error" role="alert" type="error" showIcon message={writeError} />}
      <Form<BindingFields> form={form} layout="vertical" onFinish={submit} disabled={busy} requiredMark={false}>
        <Form.Item name="model_profile_version_id" label="模型方案版本" rules={[{ required: true, message: '请选择模型方案版本' }]}>
          <Select options={options} showSearch optionFilterProp="label" />
        </Form.Item>
        <div className="deployment-price-grid">
          <Form.Item name="monthly_budget" label="月预算" rules={[{ required: true, type: 'number', min: 0, message: '请输入非负预算' }]}>
            <InputNumber min={0} />
          </Form.Item>
          <Form.Item name="rpm_limit" label="RPM" rules={rateRules}><InputNumber min={0} max={maxBindingRate} step={1} /></Form.Item>
          <Form.Item name="tpm_limit" label="TPM" rules={rateRules}><InputNumber min={0} max={maxBindingRate} step={1} /></Form.Item>
        </div>
        <Typography.Text type="secondary">启用月预算时，主部署和各备用部署都需要配置价格。</Typography.Text>
      </Form>
    </Modal>}
  </>
}
