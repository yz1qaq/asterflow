import axios from 'axios'
import { api } from './client'

export interface ModelAccount {
  id: string
  workspace_id: string
  name: string
  kind: string
  base_url: string
  status: string
  has_api_key: boolean
  created_by: string
  created_at: string
  updated_at: string
}

export interface CreateModelAccountRequest {
  name: string
  kind: string
  base_url: string
  api_key: string
}

function accountFromResponse(value: unknown, workspaceID: string): ModelAccount {
  const row = value as Partial<ModelAccount> | null
  if (!row || typeof row.id !== 'string' || !row.id || row.workspace_id !== workspaceID
    || typeof row.name !== 'string' || typeof row.kind !== 'string'
    || typeof row.base_url !== 'string' || typeof row.status !== 'string'
    || typeof row.has_api_key !== 'boolean' || typeof row.created_by !== 'string'
    || typeof row.created_at !== 'string' || typeof row.updated_at !== 'string') {
    throw new Error('模型账户响应无效，请刷新列表确认结果')
  }
  return {
    id: row.id, workspace_id: workspaceID, name: row.name, kind: row.kind,
    base_url: row.base_url, status: row.status, has_api_key: row.has_api_key,
    created_by: row.created_by, created_at: row.created_at, updated_at: row.updated_at,
  }
}

export async function listModelAccounts(workspaceID: string, signal?: AbortSignal): Promise<ModelAccount[]> {
  const { data } = await api.get<unknown>('/model-accounts', {
    headers: { 'X-Workspace-ID': workspaceID }, signal,
  })
  if (!Array.isArray(data)) throw new Error('模型账户列表响应无效，请稍后重试')
  return data.map(row => accountFromResponse(row, workspaceID))
}

export async function createModelAccount(workspaceID: string, request: CreateModelAccountRequest): Promise<ModelAccount> {
  const { data } = await api.post<unknown>('/model-accounts', {
    name: request.name, kind: request.kind, base_url: request.base_url, api_key: request.api_key,
  }, { headers: { 'X-Workspace-ID': workspaceID } })
  return accountFromResponse(data, workspaceID)
}

export async function rotateModelAccountAPIKey(workspaceID: string, accountID: string, apiKey: string): Promise<void> {
  const response = await api.put(`/model-accounts/${encodeURIComponent(accountID)}/api-key`,
    { api_key: apiKey }, { headers: { 'X-Workspace-ID': workspaceID } })
  if (response.status !== 204) throw new Error('密钥更新结果未确认，请刷新列表核对更新时间')
}

export function canManageModelAccounts(role: string) {
  return ['owner', 'admin', 'editor'].includes(role)
}

export function getModelAccountError(error: unknown, writing = false) {
  if (axios.isAxiosError(error)) {
    if (error.response?.status === 401) return '登录已失效，请重新登录'
    if (error.response?.status === 403) return '当前空间权限不足，请重新确认空间权限'
    if (error.response?.status === 404) return '账户不存在或不属于当前空间，请刷新列表'
    if (error.response?.status === 409) return '当前空间已有同名账户，请修改名称'
    if (error.response?.status === 400) return '请检查必填项、HTTP(S)地址和 API Key 后重试'
    if (error.code === 'ECONNABORTED' || !error.response) {
      return writing ? '操作结果未确认，请检查连接并刷新列表核对后再操作' : '无法读取账户，请检查连接后重试'
    }
    return writing ? '操作失败，请稍后重试' : '无法加载账户列表，请稍后重试'
  }
  return error instanceof Error ? error.message : '操作失败，请稍后重试'
}

export interface ModelDeployment {
  id: string
  workspace_id: string
  provider_account_id: string
  name: string
  model_name: string
  region: string
  timeout_ms: number
  max_retries: number
  input_price_per_million: number
  output_price_per_million: number
  cached_input_price_per_million: number
  status: string
  created_by: string
  created_at: string
  updated_at: string
}

export type DeploymentPricing = Pick<ModelDeployment,
  'input_price_per_million' | 'output_price_per_million' | 'cached_input_price_per_million'>

export type CreateModelDeploymentRequest = DeploymentPricing & {
  provider_account_id: string
  name: string
  model_name: string
  region: string
  timeout_ms: number
  max_retries: number
}

function deploymentFromResponse(value: unknown, workspaceID: string): ModelDeployment {
  const row = value as Partial<ModelDeployment> | null
  if (!row || row.workspace_id !== workspaceID
    || ['id', 'provider_account_id', 'name', 'model_name', 'status', 'created_by', 'created_at', 'updated_at', 'region'].some(key => typeof row[key as keyof ModelDeployment] !== 'string')
    || !row.id || !row.provider_account_id
    || !Number.isInteger(row.timeout_ms) || row.timeout_ms! <= 0 || row.timeout_ms! > 2_147_483_647
    || !Number.isInteger(row.max_retries) || row.max_retries! < 0 || row.max_retries! > 2_147_483_647
    || [row.input_price_per_million, row.output_price_per_million, row.cached_input_price_per_million]
      .some(price => typeof price !== 'number' || !Number.isFinite(price) || price < 0)) {
    throw new Error('模型部署响应无效，请刷新列表确认结果')
  }
  return {
    id: row.id, workspace_id: workspaceID, provider_account_id: row.provider_account_id,
    name: row.name!, model_name: row.model_name!, region: row.region!,
    timeout_ms: row.timeout_ms!, max_retries: row.max_retries!,
    input_price_per_million: row.input_price_per_million!,
    output_price_per_million: row.output_price_per_million!,
    cached_input_price_per_million: row.cached_input_price_per_million!,
    status: row.status!, created_by: row.created_by!, created_at: row.created_at!, updated_at: row.updated_at!,
  }
}

export async function listModelDeployments(workspaceID: string, signal?: AbortSignal): Promise<ModelDeployment[]> {
  const { data } = await api.get<unknown>('/model-deployments', {
    headers: { 'X-Workspace-ID': workspaceID }, signal,
  })
  if (!Array.isArray(data)) throw new Error('模型部署列表响应无效，请稍后重试')
  return data.map(row => deploymentFromResponse(row, workspaceID))
}

export async function createModelDeployment(workspaceID: string, request: CreateModelDeploymentRequest): Promise<ModelDeployment> {
  const { data } = await api.post<unknown>('/model-deployments', {
    provider_account_id: request.provider_account_id, name: request.name, model_name: request.model_name,
    region: request.region, timeout_ms: request.timeout_ms, max_retries: request.max_retries,
    input_price_per_million: request.input_price_per_million,
    output_price_per_million: request.output_price_per_million,
    cached_input_price_per_million: request.cached_input_price_per_million,
  }, { headers: { 'X-Workspace-ID': workspaceID } })
  return deploymentFromResponse(data, workspaceID)
}

export async function updateModelDeploymentPricing(workspaceID: string, deploymentID: string, prices: DeploymentPricing): Promise<ModelDeployment> {
  const { data } = await api.put<unknown>(`/model-deployments/${encodeURIComponent(deploymentID)}/pricing`, {
    input_price_per_million: prices.input_price_per_million,
    output_price_per_million: prices.output_price_per_million,
    cached_input_price_per_million: prices.cached_input_price_per_million,
  }, { headers: { 'X-Workspace-ID': workspaceID } })
  const deployment = deploymentFromResponse(data, workspaceID)
  if (deployment.id !== deploymentID) throw new Error('价格更新响应无效，请刷新列表确认结果')
  return deployment
}

export function getModelDeploymentError(error: unknown, writing = false) {
  if (axios.isAxiosError(error)) {
    if (error.response?.status === 401) return '登录已失效，请重新登录'
    if (error.response?.status === 403) return '当前空间权限不足，请重新确认空间权限'
    if (error.response?.status === 404) return '部署或供应商账户不存在于当前空间，请刷新后重新选择'
    if (error.response?.status === 409) {
      if (error.response.data?.code === 'deployment_price_required') {
        return '有环境已启用月预算并引用此部署，输入价和输出价不能同时为 0'
      }
      return '当前空间已有同名部署，请修改名称'
    }
    if (error.response?.status === 400) return '请检查模型名称、账户、超时、重试次数和非负价格'
    if (error.code === 'ECONNABORTED' || !error.response) return writing
      ? '操作结果未确认，请检查连接并刷新部署列表核对后再操作'
      : '无法读取部署，请检查连接后重试'
    return writing ? '保存部署配置失败，请稍后重试' : '无法加载部署列表，请稍后重试'
  }
  return error instanceof Error ? error.message : '操作失败，请稍后重试'
}
