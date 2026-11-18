import axios from 'axios'
import { api } from './client'
import { parseModelProfileVersion } from './modelProfiles'

export interface ModelBindingInput {
  env: string
  model_profile_version_id: string
  monthly_budget: number
  rpm_limit: number
  tpm_limit: number
}

export interface ModelBinding extends ModelBindingInput {
  workspace_id: string
  updated_at: string
}

export const maxBindingRate = 2_147_483_647

function bindingRow(value: unknown, workspaceID: string, env: string): ModelBinding {
  const row = value as ModelBinding | null
  const validRate = (rate: number) => Number.isInteger(rate) && rate >= 0 && rate <= maxBindingRate
  if (!row || row.workspace_id !== workspaceID || row.env !== env ||
    typeof row.model_profile_version_id !== 'string' || !row.model_profile_version_id ||
    typeof row.updated_at !== 'string' || !Number.isFinite(Date.parse(row.updated_at)) ||
    !Number.isFinite(row.monthly_budget) || row.monthly_budget < 0 ||
    !validRate(row.rpm_limit) || !validRate(row.tpm_limit)) {
    throw new Error('环境绑定响应无效，请刷新后重试')
  }
  return {
    workspace_id: workspaceID, env, model_profile_version_id: row.model_profile_version_id,
    monthly_budget: row.monthly_budget, rpm_limit: row.rpm_limit, tpm_limit: row.tpm_limit,
    updated_at: row.updated_at,
  }
}

export async function listWorkspaceProfileVersions(workspaceID: string, signal?: AbortSignal) {
  const { data } = await api.get<unknown>('/model-profile-versions', { headers: { 'X-Workspace-ID': workspaceID }, signal })
  if (!Array.isArray(data)) throw new Error('模型版本列表响应无效')
  return data.map(row => parseModelProfileVersion(row))
}

export async function getProjectBinding(workspaceID: string, env: string, signal?: AbortSignal) {
  try {
    const { data } = await api.get<unknown>('/model-bindings', {
      headers: { 'X-Workspace-ID': workspaceID }, params: { env }, signal,
    })
    return bindingRow(data, workspaceID, env)
  } catch (error) {
    if (axios.isAxiosError(error) && error.response?.status === 404 &&
      error.response.data?.code === 'model_binding_not_found') return null
    throw error
  }
}

export async function saveProjectBinding(workspaceID: string, req: ModelBindingInput) {
  const { env, model_profile_version_id, monthly_budget, rpm_limit, tpm_limit } = req
  const { data } = await api.put<unknown>('/model-bindings', { env, model_profile_version_id, monthly_budget, rpm_limit, tpm_limit },
    { headers: { 'X-Workspace-ID': workspaceID } })
  try {
    const saved = bindingRow(data, workspaceID, env)
    if (saved.model_profile_version_id === model_profile_version_id) return saved
  } catch {
    // The write might have succeeded even if the response can't confirm it.
  }
  throw new Error('保存结果未确认，请刷新当前环境后核对')
}

export function getModelBindingError(error: unknown, writing = false) {
  if (axios.isAxiosError(error)) {
    switch (error.response?.status) {
      case 401: return '登录已失效，请重新登录'
      case 403: return '当前空间权限不足，请重新确认空间权限'
      case 404: return writing ? '所选版本或部署已不可用，请刷新后重新选择' : '无法读取当前空间的模型绑定，请确认空间后重试'
      case 400: return writing
        ? '请检查预算和限额。月预算大于 0 时，主部署和每个备用部署都必须配置输入价格或输出价格。'
        : '无法读取当前环境，请检查空间和环境后重试'
    }
    if (!error.response) return writing ? '保存结果未确认，请刷新当前环境后核对' : '无法读取环境绑定，请检查连接后重试'
    return '环境绑定操作失败，请稍后重试'
  }
  return error instanceof Error ? error.message : '环境绑定操作失败'
}
