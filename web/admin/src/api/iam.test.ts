import { afterEach, describe, expect, it, vi } from 'vitest'
import { AxiosError } from 'axios'
import { api } from './client'
import { createWorkspace, getWorkspaceError, listWorkspaces, getMe, getWorkspaceContext, listWorkspaceMembers, removeWorkspaceMember, upsertWorkspaceMember } from './iam'

afterEach(() => vi.restoreAllMocks())
describe('workspace API', () => {
  it('uses paged list requests and forwards cancellation', async () => {
    const signal = new AbortController().signal
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: [] })
    await expect(listWorkspaces(11, 10, signal)).resolves.toEqual([])
    expect(get).toHaveBeenCalledWith('/workspaces', { params: { limit: 11, offset: 10 }, signal })
  })
  it('rejects malformed list responses', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: '<html>fallback</html>' })
    await expect(listWorkspaces()).rejects.toThrow('空间列表响应无效')
  })
  it('creates a space without supplying an actor identity', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({ data: { id: 'space-1', name: 'Sales' } })
    await expect(createWorkspace({ name: 'Sales', description: 'demo' })).resolves.toMatchObject({ id: 'space-1' })
    expect(post).toHaveBeenCalledWith('/workspaces', { name: 'Sales', description: 'demo' })
  })
  it('asks users to verify the list after an uncertain creation timeout', () => {
    expect(getWorkspaceError(new AxiosError('timeout', 'ECONNABORTED'), 'create')).toContain('先刷新列表')
  })
})

describe('workspace members API', () => {
  it('rejects member results from a different workspace', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: [{ workspace_id: 'other', user_id: 'user-1', role: 'owner' }] })
    await expect(listWorkspaceMembers('space-1')).rejects.toThrow('成员列表响应无效')
  })
  it('accepts canonical UUIDs in member update responses', async () => {
    const id = '12345678-1234-1234-1234-123456789abc'
    const put = vi.spyOn(api, 'put').mockResolvedValue({ data: { workspace_id: 'space-1', user_id: id, role: 'editor' } })
    await upsertWorkspaceMember('space-1', id.replace(/-/g, ''), 'editor')
    expect(put).toHaveBeenCalledWith('/workspaces/space-1/members/12345678123412341234123456789abc', { role: 'editor' })
  })
  it('requires a confirmed 204 response for member removal', async () => {
    vi.spyOn(api, 'delete').mockResolvedValue({ status: 200 })
    await expect(removeWorkspaceMember('space-1', 'user-1')).rejects.toThrow('移除响应无效')
  })
  it('loads the current global role from the server', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: { user_id: 'user-1', global_role: 'member' } })
    await expect(getMe()).resolves.toEqual({ user_id: 'user-1', global_role: 'member' })
  })
})

describe('current workspace verification', () => {
  it('checks the requested workspace explicitly and forwards cancellation', async () => {
    const signal = new AbortController().signal
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: {
      user_id: 'user-1', global_role: 'member', workspace_id: 'space-b', workspace_role: 'owner',
    } })
    await expect(getWorkspaceContext('space-b', signal)).resolves.toMatchObject({ workspace_role: 'owner' })
    expect(get).toHaveBeenCalledWith('/context', { headers: { 'X-Workspace-ID': 'space-b' }, signal })
  })
  it('does not accept a role response for another workspace', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: {
      user_id: 'user-1', global_role: 'member', workspace_id: 'space-a', workspace_role: 'owner',
    } })
    await expect(getWorkspaceContext('space-b')).rejects.toThrow('空间权限响应无效')
  })
})
