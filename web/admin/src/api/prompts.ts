import axios from 'axios'
import { api } from './client'

export interface Prompt {
  id: string
  workspace_id: string
  name: string
  category: string
  created_at: string
}

export interface PromptVersion {
  id: string
  prompt_id: string
  version: number
  template: string
  variables_schema: string
  created_at: string
}

export interface PromptEnvBinding {
  prompt_id: string
  env: string
  version_id: string
  updated_at: string
}

export const PROMPT_ENVS = ['dev', 'staging', 'prod'] as const

export interface PromptExperimentVariant {
  version_id: string
  traffic: number
}

export interface PromptEnvEvent {
  id: string
  prompt_id: string
  env: string
  action: string
  from_version_id?: string
  to_version_id: string
  actor: string
  detail: string
  created_at: string
}

export interface PromptRolloutEvent {
  id: string
  experiment_id: string
  action: string
  from_traffic: number
  to_traffic: number
  actor: string
  detail: string
  created_at: string
}

export interface PromptExperiment {
  id: string
  prompt_id: string
  env: string
  baseline_version_id?: string
  candidate_version_id?: string
  traffic_percent: number
  variants: PromptExperimentVariant[]
  status: string
  started_at: string
  completed_at?: string
  created_by: string
  created_at: string
}

export type PromptVersionInput = Pick<PromptVersion, 'template'> & { variables_schema?: string }
export type CreatePromptInput = PromptVersionInput & { name: string; category?: string }

export function canWritePrompts(role: string) {
  return ['owner', 'admin', 'editor'].includes(role)
}

const scope = (workspaceID: string, signal?: AbortSignal) => ({ headers: { 'X-Workspace-ID': workspaceID }, signal })
const promptPath = (promptID: string) => `/prompts/${encodeURIComponent(promptID)}`
const versionBody = (input: PromptVersionInput) => ({ template: input.template, variables_schema: input.variables_schema })

function parsePrompt(value: unknown, workspaceID: string): Prompt {
  const row = value as Partial<Prompt> | null
  if (!row || row.workspace_id !== workspaceID || typeof row.id !== 'string' || !row.id
    || typeof row.name !== 'string' || typeof row.category !== 'string' || typeof row.created_at !== 'string') {
    throw new Error('提示词响应无效，请刷新列表确认结果')
  }
  return { id: row.id, workspace_id: workspaceID, name: row.name, category: row.category, created_at: row.created_at }
}

function parseVersion(value: unknown, promptID: string): PromptVersion {
  const row = value as Partial<PromptVersion> | null
  if (!row || row.prompt_id !== promptID || typeof row.id !== 'string' || !row.id
    || typeof row.version !== 'number' || !Number.isSafeInteger(row.version) || row.version < 1
    || typeof row.template !== 'string' || typeof row.variables_schema !== 'string' || typeof row.created_at !== 'string') {
    throw new Error('提示词版本响应无效，请刷新版本列表确认结果')
  }
  return { id: row.id, prompt_id: promptID, version: row.version, template: row.template,
    variables_schema: row.variables_schema, created_at: row.created_at }
}

export async function listPrompts(workspaceID: string, signal?: AbortSignal) {
  const { data } = await api.get<unknown>('/prompts', scope(workspaceID, signal))
  if (!Array.isArray(data)) throw new Error('提示词列表响应无效')
  return data.map(row => parsePrompt(row, workspaceID))
}

export async function createPrompt(workspaceID: string, input: CreatePromptInput, signal?: AbortSignal) {
  const { data } = await api.post<{ prompt: unknown; version: unknown }>('/prompts', {
    name: input.name, category: input.category, ...versionBody(input),
  }, scope(workspaceID, signal))
  const prompt = parsePrompt(data?.prompt, workspaceID)
  const version = parseVersion(data?.version, prompt.id)
  if (version.version !== 1) throw new Error('提示词初始版本响应无效，请刷新列表确认结果')
  return { prompt, version }
}

export async function listPromptVersions(workspaceID: string, promptID: string, signal?: AbortSignal) {
  const { data } = await api.get<unknown>(`${promptPath(promptID)}/versions`, scope(workspaceID, signal))
  if (!Array.isArray(data)) throw new Error('提示词版本列表响应无效')
  return data.map(row => parseVersion(row, promptID))
}

export async function archivePromptDraft(workspaceID: string, promptID: string, versionID: string, signal?: AbortSignal) {
  await api.delete(`/prompts/${encodeURIComponent(promptID)}/versions/${encodeURIComponent(versionID)}`,
    { headers: { 'X-Workspace-ID': workspaceID }, signal })
}

export async function createPromptVersion(workspaceID: string, promptID: string, input: PromptVersionInput, signal?: AbortSignal) {
  const { data } = await api.post<unknown>(`${promptPath(promptID)}/versions`, versionBody(input), scope(workspaceID, signal))
  return parseVersion(data, promptID)
}

function parseEnvBinding(value: unknown, promptID: string): PromptEnvBinding {
  const row = value as Partial<PromptEnvBinding> | null
  if (!row || row.prompt_id !== promptID || typeof row.env !== 'string' || !row.env
    || typeof row.version_id !== 'string' || !row.version_id || typeof row.updated_at !== 'string') {
    throw new Error('环境指针响应无效，请刷新后重试')
  }
  return { prompt_id: promptID, env: row.env, version_id: row.version_id, updated_at: row.updated_at }
}

export async function listPromptEnvs(workspaceID: string, promptID: string, signal?: AbortSignal) {
  const { data } = await api.get<unknown>(`${promptPath(promptID)}/envs`, scope(workspaceID, signal))
  if (!Array.isArray(data)) throw new Error('环境指针列表响应无效')
  return data.map(row => parseEnvBinding(row, promptID))
}

export async function rollbackPromptVersion(
  workspaceID: string, promptID: string, env: string, versionID: string, signal?: AbortSignal,
) {
  const { data } = await api.post<{ status?: unknown }>(`${promptPath(promptID)}/rollback`, {
    env, version_id: versionID,
  }, scope(workspaceID, signal))
  if (data?.status !== 'rolled_back') throw new Error('回滚响应无效，请刷新环境指针确认结果')
  return 'rolled_back' as const
}

export async function promotePromptVersion(
  workspaceID: string, promptID: string, env: string, versionID: string, signal?: AbortSignal,
) {
  const { data } = await api.post<{ status?: unknown }>(`${promptPath(promptID)}/promote`, {
    env, version_id: versionID,
  }, scope(workspaceID, signal))
  if (data?.status !== 'promoted') throw new Error('发布响应无效，请刷新环境指针确认结果')
  return 'promoted' as const
}

function parseExperiment(value: unknown, promptID: string): PromptExperiment {
  const row = value as Partial<PromptExperiment> | null
  const variants = row?.variants
  if (!row || row.prompt_id !== promptID || typeof row.id !== 'string' || !row.id
    || typeof row.env !== 'string' || !row.env || typeof row.status !== 'string' || !row.status
    || typeof row.traffic_percent !== 'number' || !Number.isFinite(row.traffic_percent)
    || typeof row.started_at !== 'string' || typeof row.created_at !== 'string'
    || !Array.isArray(variants)
    || variants.some(item => !item || typeof item.version_id !== 'string' || typeof item.traffic !== 'number')) {
    throw new Error('实验响应无效，请刷新后重试')
  }
  return {
    id: row.id,
    prompt_id: promptID,
    env: row.env,
    baseline_version_id: typeof row.baseline_version_id === 'string' ? row.baseline_version_id : undefined,
    candidate_version_id: typeof row.candidate_version_id === 'string' ? row.candidate_version_id : undefined,
    traffic_percent: row.traffic_percent,
    variants: variants.map(item => ({ version_id: item.version_id, traffic: item.traffic })),
    status: row.status,
    started_at: row.started_at,
    completed_at: typeof row.completed_at === 'string' ? row.completed_at : undefined,
    created_by: typeof row.created_by === 'string' ? row.created_by : '',
    created_at: row.created_at,
  }
}

export async function getActiveExperiment(workspaceID: string, promptID: string, env: string, signal?: AbortSignal) {
  const { data } = await api.get<unknown>(`${promptPath(promptID)}/experiments/active`, {
    ...scope(workspaceID, signal), params: { env },
  })
  if (data === null || data === undefined) return null
  return parseExperiment(data, promptID)
}

export async function startExperiment(
  workspaceID: string, promptID: string, env: string, variants: PromptExperimentVariant[], signal?: AbortSignal,
) {
  const { data } = await api.post<unknown>(`${promptPath(promptID)}/experiments`, {
    env, variants,
  }, scope(workspaceID, signal))
  return parseExperiment(data, promptID)
}

export async function startRollout(
  workspaceID: string, promptID: string, env: string, candidateVersionID: string, traffic: number, signal?: AbortSignal,
) {
  const { data } = await api.post<unknown>(`${promptPath(promptID)}/rollouts`, {
    env, candidate_version_id: candidateVersionID, traffic,
  }, scope(workspaceID, signal))
  return parseExperiment(data, promptID)
}

export async function updateRolloutTraffic(
  workspaceID: string, promptID: string, env: string, traffic: number, signal?: AbortSignal,
) {
  const { data } = await api.put<unknown>(`${promptPath(promptID)}/rollouts/traffic`, {
    env, traffic,
  }, scope(workspaceID, signal))
  return parseExperiment(data, promptID)
}

export async function completeRollout(workspaceID: string, promptID: string, env: string, signal?: AbortSignal) {
  const { data } = await api.post<{ status?: unknown }>(`${promptPath(promptID)}/rollouts/complete`, { env }, scope(workspaceID, signal))
  if (data?.status !== 'completed') throw new Error('完成上线响应无效，请刷新后确认结果')
  return 'completed' as const
}

export async function rollbackRollout(workspaceID: string, promptID: string, env: string, signal?: AbortSignal) {
  const { data } = await api.post<{ status?: unknown }>(`${promptPath(promptID)}/rollouts/rollback`, { env }, scope(workspaceID, signal))
  if (data?.status !== 'rolled_back') throw new Error('停止实验响应无效，请刷新后确认结果')
  return 'rolled_back' as const
}

function parseRolloutEvent(value: unknown, experimentID: string): PromptRolloutEvent {
  const row = value as Partial<PromptRolloutEvent> | null
  if (!row || row.experiment_id !== experimentID || typeof row.id !== 'string' || !row.id
    || typeof row.action !== 'string' || !row.action
    || typeof row.from_traffic !== 'number' || typeof row.to_traffic !== 'number'
    || typeof row.actor !== 'string' || typeof row.detail !== 'string' || typeof row.created_at !== 'string') {
    throw new Error('实验事件响应无效，请刷新后重试')
  }
  return {
    id: row.id, experiment_id: experimentID, action: row.action,
    from_traffic: row.from_traffic, to_traffic: row.to_traffic,
    actor: row.actor, detail: row.detail, created_at: row.created_at,
  }
}

export async function listRolloutEvents(
  workspaceID: string, promptID: string, experimentID: string, signal?: AbortSignal,
) {
  const { data } = await api.get<unknown>(
    `${promptPath(promptID)}/experiments/${encodeURIComponent(experimentID)}/events`, scope(workspaceID, signal))
  if (!Array.isArray(data)) throw new Error('实验事件列表响应无效')
  return data.map(row => parseRolloutEvent(row, experimentID))
}

function parseEnvEvent(value: unknown, promptID: string): PromptEnvEvent {
  const row = value as Partial<PromptEnvEvent> | null
  if (!row || row.prompt_id !== promptID || typeof row.id !== 'string' || !row.id
    || typeof row.env !== 'string' || !row.env || typeof row.action !== 'string' || !row.action
    || typeof row.to_version_id !== 'string' || !row.to_version_id
    || typeof row.actor !== 'string' || typeof row.detail !== 'string' || typeof row.created_at !== 'string') {
    throw new Error('环境事件响应无效，请刷新后重试')
  }
  return {
    id: row.id, prompt_id: promptID, env: row.env, action: row.action,
    from_version_id: typeof row.from_version_id === 'string' && row.from_version_id ? row.from_version_id : undefined,
    to_version_id: row.to_version_id, actor: row.actor, detail: row.detail, created_at: row.created_at,
  }
}

export async function listEnvEvents(
  workspaceID: string, promptID: string, env: string, signal?: AbortSignal,
) {
  const { data } = await api.get<unknown>(`${promptPath(promptID)}/env-events`, {
    ...scope(workspaceID, signal), params: { env },
  })
  if (!Array.isArray(data)) throw new Error('环境事件列表响应无效')
  return data.map(row => parseEnvEvent(row, promptID))
}

export async function diffPromptVersions(
  workspaceID: string, promptID: string, from: number, to: number, signal?: AbortSignal,
) {
  const { data } = await api.get<{ diff?: unknown }>(`${promptPath(promptID)}/diff`, {
    ...scope(workspaceID, signal), params: { from, to },
  })
  if (typeof data?.diff !== 'string') throw new Error('版本差异响应无效，请重试')
  return data.diff
}

export type PromptVariables = Record<string, unknown>

export function parsePromptVariables(text: string): PromptVariables {
  try {
    const value: unknown = JSON.parse(text.trim() || '{}')
    if (value !== null && typeof value === 'object' && !Array.isArray(value)) return value as PromptVariables
  } catch {
    // Keep browser-specific JSON parser details out of the form error.
  }
  throw new Error('预览变量必须是 JSON 对象，例如 {"order_id":"ORD-1001"}')
}

export async function renderPrompt(workspaceID: string, promptID: string, versionID: string, vars: PromptVariables, signal?: AbortSignal) {
  const { data } = await api.post<{ rendered: unknown }>(`${promptPath(promptID)}/render`, {
    version_id: versionID, vars,
  }, scope(workspaceID, signal))
  if (typeof data?.rendered !== 'string') throw new Error('预览响应无效，请重试')
  return data.rendered
}

export function getPromptError(error: unknown, action: 'read' | 'save' | 'preview') {
  if (axios.isAxiosError<{ error?: unknown }>(error)) {
    switch (error.response?.status) {
      case 401: return '登录已失效，请重新登录'
      case 403: return '当前空间权限不足，请重新确认空间权限'
      case 404: return '提示词或版本不在当前空间，请刷新后重新选择'
      case 400: {
        const detail = error.response.data?.error
        const prefix = 'invalid prompt argument: '
        if (typeof detail === 'string' && detail.startsWith(prefix)) {
          const reason = detail.slice(prefix.length).slice(0, 400)
          const missing = reason.match(/missing required prompt variable (.+)|map has no entry for key (.+)/)
          if (action === 'preview' && missing) return `缺少预览变量 ${missing[1] ?? missing[2]}`
          return reason
        }
        return action === 'preview' ? '请检查预览变量与所选版本' : '请检查模板、变量 Schema 或其他请求参数'
      }
    }
    if (action === 'save' && (!error.response || error.response.status >= 500)) {
      return '保存结果未确认，请刷新列表和版本历史后再操作'
    }
  } else if (error instanceof Error) {
    return error.message
  }
  return { read: '无法读取提示词，请稍后重试', save: '保存提示词失败，请稍后重试', preview: '预览失败，请稍后重试' }[action]
}
