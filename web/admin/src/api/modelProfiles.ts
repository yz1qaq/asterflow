import axios from 'axios'
import { api } from './client'

export interface ModelProfile {
  id: string; workspace_id: string; name: string; description: string
  created_by: string; created_at: string; updated_at: string
}
export interface ModelProfileVersion {
  id: string; profile_id: string; version: number; primary_deployment_id: string
  fallback_deployment_ids: string[]; classification_max: string; created_by: string; created_at: string
}
export type ProfileVersionInput = Pick<ModelProfileVersion, 'primary_deployment_id' | 'fallback_deployment_ids' | 'classification_max'>
export type ProfileInput = ProfileVersionInput & { name: string; description: string }
function profileRow(value: unknown, workspace: string): ModelProfile {
  const r = value as ModelProfile | null
  if (!r || r.workspace_id !== workspace || !r.id ||
    ['id', 'name', 'description', 'created_by', 'created_at', 'updated_at'].some(k => typeof r[k as keyof ModelProfile] !== 'string')) {
    throw new Error('模型方案响应无效，请刷新列表确认结果')
  }
  return { id: r.id, workspace_id: workspace, name: r.name, description: r.description,
    created_by: r.created_by, created_at: r.created_at, updated_at: r.updated_at }
}
export function parseModelProfileVersion(value: unknown, profileID?: string): ModelProfileVersion {
  const r = value as ModelProfileVersion | null
  if (!r || typeof r.profile_id !== 'string' || !r.profile_id || (profileID !== undefined && r.profile_id !== profileID) || !r.id || !r.primary_deployment_id ||
    ['id', 'primary_deployment_id', 'classification_max', 'created_by', 'created_at'].some(k => typeof r[k as keyof ModelProfileVersion] !== 'string')
    || !Number.isInteger(r.version) || r.version < 1 || r.version > 2147483647
    || !Array.isArray(r.fallback_deployment_ids) || r.fallback_deployment_ids.some(id => typeof id !== 'string' || !id)) {
    throw new Error('模型版本响应无效，请刷新版本列表确认结果')
  }
  return { id: r.id, profile_id: r.profile_id, version: r.version, primary_deployment_id: r.primary_deployment_id,
    fallback_deployment_ids: [...r.fallback_deployment_ids], classification_max: r.classification_max,
    created_by: r.created_by, created_at: r.created_at }
}
const body = (r: ProfileVersionInput) => ({
  primary_deployment_id: r.primary_deployment_id, fallback_deployment_ids: r.fallback_deployment_ids,
  classification_max: r.classification_max,
})
export async function listModelProfiles(workspaceID: string, signal?: AbortSignal) {
  const { data } = await api.get<unknown>('/model-profiles', { headers: { 'X-Workspace-ID': workspaceID }, signal })
  if (!Array.isArray(data)) throw new Error('模型方案列表响应无效')
  return data.map(r => profileRow(r, workspaceID))
}
export async function createModelProfile(workspaceID: string, req: ProfileInput) {
  const { data } = await api.post('/model-profiles', { name: req.name, description: req.description, ...body(req) },
    { headers: { 'X-Workspace-ID': workspaceID } })
  const profile = profileRow(data?.profile, workspaceID)
  const version = parseModelProfileVersion(data?.version, profile.id)
  if (version.version !== 1) throw new Error('初始版本响应无效，请刷新列表确认结果')
  return { profile, version }
}
export async function listModelProfileVersions(workspaceID: string, profileID: string, signal?: AbortSignal) {
  const { data } = await api.get<unknown>(`/model-profiles/${encodeURIComponent(profileID)}/versions`,
    { headers: { 'X-Workspace-ID': workspaceID }, signal })
  if (!Array.isArray(data)) throw new Error('模型版本列表响应无效')
  return data.map(r => parseModelProfileVersion(r, profileID))
}
export async function createModelProfileVersion(workspaceID: string, profileID: string, req: ProfileVersionInput) {
  const { data } = await api.post(`/model-profiles/${encodeURIComponent(profileID)}/versions`, body(req),
    { headers: { 'X-Workspace-ID': workspaceID } })
  return parseModelProfileVersion(data, profileID)
}
export function getModelProfileError(error: unknown, writing = false) {
  if (axios.isAxiosError(error)) {
    switch (error.response?.status) {
      case 401: return '登录已失效，请重新登录'
      case 403: return '当前空间权限不足，请重新确认空间权限'
      case 404: return '方案或部署不在当前空间，请刷新后重新选择'
      case 409: return '当前空间已有同名方案，请修改名称'
      case 400: return '请检查方案名称和主备部署配置'
    }
    if (!error.response || error.code === 'ECONNABORTED') return writing
      ? '操作结果未确认，请刷新方案及版本列表核对后再操作' : '无法读取模型方案，请检查连接后重试'
    return '模型方案操作失败，请稍后重试'
  }
  return error instanceof Error ? error.message : '模型方案操作失败'
}
