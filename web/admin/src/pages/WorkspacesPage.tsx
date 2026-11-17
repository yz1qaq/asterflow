import { useEffect, useRef, useState } from 'react'
import { Alert, App, Button, Empty, Form, Input, Modal, Space, Table, Tag, Typography } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Redirect } from 'wouter'
import { createWorkspace, getWorkspaceError, listWorkspaces, getWorkspaceContext, getWorkspaceSelectionError, isWorkspaceUnavailable, type CreateWorkspaceRequest, type Workspace } from '@/api/iam'
import { WorkspaceMembersDrawer } from '@/components/WorkspaceMembersDrawer'
import { isSessionValid, useAuthStore } from '@/store/authStore'

const PAGE_SIZE = 10

export function WorkspacesPage() {
  const session = useAuthStore()
  if (!isSessionValid(session) || !session.user || !session.token) return <Redirect to="/login" />
  return <WorkspacesContent key={session.token} userID={session.user.id} token={session.token} />
}

function WorkspacesContent({ userID, token }: { userID: string; token: string }) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [offset, setOffset] = useState(0)
  const [open, setOpen] = useState(false)
  const [membersWorkspace, setMembersWorkspace] = useState<Workspace | null>(null)
  const [createError, setCreateError] = useState('')
  const [form] = Form.useForm<CreateWorkspaceRequest>()
  const submitting = useRef(false)
  const mounted = useRef(false)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  function sameSession() {
    const session = useAuthStore.getState()
    return mounted.current && isSessionValid(session) && session.user?.id === userID && session.token === token
  }

  const workspaceId = useAuthStore(state => state.workspaceId)
  const setWorkspace = useAuthStore(state => state.setWorkspace)
  const [selectionError, setSelectionError] = useState('')
  const selecting = useRef(false)
  const selectionRequest = useRef<AbortController | null>(null)
  useEffect(() => () => selectionRequest.current?.abort(), [])

  async function loadContext(id: string, signal?: AbortSignal) {
    const result = await getWorkspaceContext(id, signal)
    if (result.user_id !== userID) throw new Error('当前身份已变化，请重新登录')
    return result
  }
  const currentContext = useQuery({
    queryKey: ['workspace-context', userID, workspaceId],
    queryFn: ({ signal }) => loadContext(workspaceId!, signal),
    enabled: Boolean(workspaceId),
    retry: false,
    refetchOnMount: 'always',
  })
  useEffect(() => {
    if (workspaceId && isWorkspaceUnavailable(currentContext.error) && sameSession()
      && useAuthStore.getState().workspaceId === workspaceId) {
      setSelectionError(getWorkspaceSelectionError(currentContext.error))
      setWorkspace(null)
    }
  }, [currentContext.error, workspaceId, setWorkspace])

  const selection = useMutation({
    mutationFn: async (workspace: Workspace) => {
      const controller = new AbortController()
      selectionRequest.current = controller
      return loadContext(workspace.id, controller.signal)
    },
    onSuccess: result => {
      if (!sameSession()) return
      queryClient.setQueryData(['workspace-context', userID, result.workspace_id], result)
      setWorkspace(result.workspace_id)
      setSelectionError('')
      setMembersWorkspace(null)
      void message.success('已切换当前空间')
    },
    onError: error => { if (sameSession()) setSelectionError(getWorkspaceSelectionError(error)) },
  })
  async function choose(workspace: Workspace) {
    if (selecting.current || !sameSession()) return
    selecting.current = true
    setSelectionError('')
    try { await selection.mutateAsync(workspace) } catch {
      // 只有验证成功才切换；失败保留原来的选择。
    } finally { selecting.current = false; selectionRequest.current = null }
  }

  const query = useQuery({
    queryKey: ['workspaces', userID, offset],
    queryFn: ({ signal }) => listWorkspaces(PAGE_SIZE + 1, offset, signal),
    retry: false,
    refetchOnMount: 'always',
  })
  const workspaces = query.data?.slice(0, PAGE_SIZE) ?? []
  const hasNext = (query.data?.length ?? 0) > PAGE_SIZE
  const create = useMutation({
    mutationFn: createWorkspace,
    onSuccess: async workspace => {
      if (!sameSession()) return
      void message.success('工作空间已创建，你已成为该空间的 owner')
      setOpen(false)
      form.resetFields()
      setOffset(0)
      void queryClient.invalidateQueries({ queryKey: ['workspaces', userID] })
      await choose(workspace)
    },
    onError: error => { if (sameSession()) setCreateError(getWorkspaceError(error, 'create')) },
  })
  async function onFinish(values: CreateWorkspaceRequest) {
    if (submitting.current || !sameSession()) return
    submitting.current = true
    setCreateError('')
    try {
      await create.mutateAsync({ name: values.name.trim(), description: values.description?.trim() ?? '' })
    } catch {
      // 保留输入，onError 显示失败原因。
    } finally {
      submitting.current = false
    }
  }
  function closeForm() {
    if (submitting.current) return
    setOpen(false)
    setCreateError('')
    form.resetFields()
    create.reset()
  }
  const createdAt = (value: string) => value && !Number.isNaN(Date.parse(value))
    ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—'

  return <section aria-labelledby="workspaces-heading">
    <div className="page-heading">
      <div>
        <Typography.Title level={2} id="workspaces-heading">工作空间</Typography.Title>
        <Typography.Paragraph className="page-description">查看有权访问的空间。创建空间后，你将自动成为该空间的 owner。</Typography.Paragraph>
      </div>
      <Space wrap>
        <Button icon={<ReloadOutlined />} loading={query.isFetching} onClick={() => void query.refetch()}>刷新列表</Button>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => { setCreateError(''); setOpen(true) }}>新建空间</Button>
      </Space>
    </div>
    <div className="workspace-current" aria-label="当前空间">
      <Space direction="vertical" size={8}>
        <Typography.Text strong>当前空间</Typography.Text>
        {workspaceId ? <>
          <Typography.Text className="workspace-id" copyable>{workspaceId}</Typography.Text>
          {currentContext.isFetching ? <Typography.Text role="status">正在确认空间权限…</Typography.Text>
            : currentContext.isError ? <Alert type="error" showIcon message={getWorkspaceSelectionError(currentContext.error)}
              action={<Button size="small" onClick={() => void currentContext.refetch()}>重新确认</Button>} />
            : currentContext.data && <Space><Tag color="blue">空间角色：{currentContext.data.workspace_role}</Tag>
              <Typography.Text>全局角色：{currentContext.data.global_role}</Typography.Text></Space>}
          <Button size="small" disabled={selection.isPending} onClick={() => { setWorkspace(null); setSelectionError('') }}>清除当前空间</Button>
        </> : <Typography.Text>尚未选择当前空间，请在下方列表中选择。</Typography.Text>}
      </Space>
    </div>
    {selectionError && <Alert className="user-create-error" role="alert" type="error" showIcon message={selectionError} />}
    {query.isError ? <Alert role="alert" type="error" showIcon message={getWorkspaceError(query.error, 'list')}
      action={<Button size="small" loading={query.isFetching} onClick={() => void query.refetch()}>重试</Button>} />
      : <Table<Workspace> rowKey="id" dataSource={workspaces} loading={query.isPending || query.isFetching} pagination={false}
        locale={{ emptyText: <Empty description="当前页暂无工作空间，可以新建一个空间开始使用" /> }} columns={[
          { title: '空间名称 / ID', dataIndex: 'name', render: (value: string, workspace: Workspace) => <Space direction="vertical" size={4}>
            <Typography.Text strong>{value}</Typography.Text><Typography.Text className="workspace-id" copyable>{workspace.id}</Typography.Text>
          </Space> },
          { title: '描述', dataIndex: 'description', render: (value: string) => value || '—' },
          { title: '创建时间', dataIndex: 'created_at', render: createdAt },
          { title: '操作', render: (_, workspace: Workspace) => <Space wrap>
            {workspaceId === workspace.id ? <Tag color="blue">当前空间</Tag>
              : <Button size="small" disabled={selection.isPending || create.isPending} loading={selection.isPending && selection.variables?.id === workspace.id}
                onClick={() => void choose(workspace)} aria-label={`选择空间 ${workspace.name}`}>选择空间</Button>}
            <Button size="small" onClick={() => setMembersWorkspace(workspace)} aria-label={`查看成员 ${workspace.name}`}>查看成员</Button>
          </Space> },
        ]} />}
    {!query.isError && <div className="users-pagination" aria-label="工作空间列表分页">
      <Typography.Text>第 {Math.floor(offset / PAGE_SIZE) + 1} 页 · 本页 {workspaces.length} 个空间</Typography.Text>
      <Space>
        <Button disabled={offset === 0 || query.isFetching} onClick={() => setOffset(value => Math.max(0, value - PAGE_SIZE))}>上一页</Button>
        <Button disabled={!hasNext || query.isFetching} onClick={() => setOffset(value => value + PAGE_SIZE)}>下一页</Button>
      </Space>
    </div>}
    {membersWorkspace && <WorkspaceMembersDrawer key={membersWorkspace.id} workspace={membersWorkspace} userID={userID} onClose={() => setMembersWorkspace(null)} />}
    <Modal className="user-create-modal" title="新建工作空间" open={open} onCancel={closeForm} onOk={() => form.submit()}
      okText="创建空间" cancelText="取消" confirmLoading={create.isPending} cancelButtonProps={{ disabled: create.isPending }}
      closable={!create.isPending} keyboard={!create.isPending} maskClosable={!create.isPending}>
      <Typography.Paragraph className="user-form-help">当前登录用户会自动成为空间 owner，可在成员管理中添加已有用户。</Typography.Paragraph>
      {createError && <Alert className="user-create-error" role="alert" type="error" showIcon message={createError} />}
      <Form<CreateWorkspaceRequest> form={form} name="create-workspace" layout="vertical" onFinish={onFinish} disabled={create.isPending} requiredMark={false}>
        <Form.Item name="name" label="空间名称" rules={[{ required: true, whitespace: true, message: '请输入空间名称' }]}><Input autoComplete="off" /></Form.Item>
        <Form.Item name="description" label="描述"><Input.TextArea autoSize={{ minRows: 3, maxRows: 6 }} /></Form.Item>
      </Form>
    </Modal>
  </section>
}
