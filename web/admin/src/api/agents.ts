import { api } from './client'

export interface Agent {
  id: string
  workspace_id: string
  name: string
  description: string
  template: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface AgentVersion {
  id: string
  agent_id: string
  version: number
  snapshot_json: string
  created_by: string
  created_at: string
}

export interface AgentEnvBinding {
  agent_id: string
  env: string
  version_id: string
  updated_at: string
}

export interface AgentVersionInput {
  system_prompt?: string
  system_prompt_version_id?: string
  user_prompt_id?: string
  user_prompt_version_id?: string
  prompt_env?: string
  model_profile_version_id?: string
  tool_ids?: string[]
  generation_config?: Record<string, unknown>
}

export function canWriteAgents(role: string) {
  return ['owner', 'admin', 'editor'].includes(role)
}

export function canDeleteAgents(role: string) {
  return role === 'owner' || role === 'admin'
}

const scope = (workspaceID: string, signal?: AbortSignal) => ({ headers: { 'X-Workspace-ID': workspaceID }, signal })
const agentPath = (id: string) => `/agents/${encodeURIComponent(id)}`

function parseAgent(value: unknown, workspaceID: string): Agent {
  const row = value as Partial<Agent> | null
  if (!row || row.workspace_id !== workspaceID || typeof row.id !== 'string' || !row.id
    || typeof row.name !== 'string' || typeof row.description !== 'string'
    || typeof row.template !== 'string' || typeof row.created_at !== 'string') {
    throw new Error('Agent 响应无效，请刷新列表确认结果')
  }
  return {
    id: row.id, workspace_id: workspaceID, name: row.name, description: row.description,
    template: row.template, created_by: typeof row.created_by === 'string' ? row.created_by : '',
    created_at: row.created_at, updated_at: typeof row.updated_at === 'string' ? row.updated_at : row.created_at,
  }
}

function parseVersion(value: unknown, agentID: string): AgentVersion {
  const row = value as Partial<AgentVersion> | null
  if (!row || row.agent_id !== agentID || typeof row.id !== 'string' || !row.id
    || typeof row.version !== 'number' || !Number.isSafeInteger(row.version) || row.version < 1
    || typeof row.snapshot_json !== 'string' || typeof row.created_at !== 'string') {
    throw new Error('Agent 版本响应无效，请刷新版本列表确认结果')
  }
  return {
    id: row.id, agent_id: agentID, version: row.version, snapshot_json: row.snapshot_json,
    created_by: typeof row.created_by === 'string' ? row.created_by : '', created_at: row.created_at,
  }
}

function parseEnv(value: unknown, agentID: string): AgentEnvBinding {
  const row = value as Partial<AgentEnvBinding> | null
  if (!row || row.agent_id !== agentID || typeof row.env !== 'string' || !row.env
    || typeof row.version_id !== 'string' || !row.version_id || typeof row.updated_at !== 'string') {
    throw new Error('Agent 环境响应无效，请刷新后重试')
  }
  return { agent_id: agentID, env: row.env, version_id: row.version_id, updated_at: row.updated_at }
}

function versionBody(input: AgentVersionInput) {
  return {
    system_prompt: input.system_prompt ?? '',
    system_prompt_version_id: input.system_prompt_version_id ?? '',
    user_prompt_id: input.user_prompt_id ?? '',
    user_prompt_version_id: input.user_prompt_version_id ?? '',
    prompt_env: input.prompt_env ?? 'dev',
    model_profile_version_id: input.model_profile_version_id ?? '',
    ...(input.tool_ids?.length ? { tool_ids: input.tool_ids } : {}),
    generation_config: input.generation_config ?? {},
  }
}

export async function listAgents(workspaceID: string, signal?: AbortSignal) {
  const { data } = await api.get<unknown>('/agents', scope(workspaceID, signal))
  if (!Array.isArray(data)) throw new Error('Agent 列表响应无效')
  return data.map(row => parseAgent(row, workspaceID))
}

export async function createAgent(
  workspaceID: string,
  input: AgentVersionInput & { name: string; description?: string; template?: string },
  signal?: AbortSignal,
) {
  const { data } = await api.post<{ agent: unknown; version: unknown }>('/agents', {
    name: input.name, description: input.description ?? '', template: input.template ?? '',
    ...versionBody(input),
  }, scope(workspaceID, signal))
  const agent = parseAgent(data?.agent, workspaceID)
  const version = parseVersion(data?.version, agent.id)
  if (version.version !== 1) throw new Error('Agent 初始版本响应无效，请刷新列表确认结果')
  return { agent, version }
}

export async function getAgent(workspaceID: string, agentID: string, signal?: AbortSignal) {
  const { data } = await api.get<unknown>(agentPath(agentID), scope(workspaceID, signal))
  return parseAgent(data, workspaceID)
}

export async function deleteAgent(workspaceID: string, agentID: string, signal?: AbortSignal) {
  await api.delete(agentPath(agentID), scope(workspaceID, signal))
}

export async function listAgentVersions(workspaceID: string, agentID: string, signal?: AbortSignal) {
  const { data } = await api.get<unknown>(`${agentPath(agentID)}/versions`, scope(workspaceID, signal))
  if (!Array.isArray(data)) throw new Error('Agent 版本列表响应无效')
  return data.map(row => parseVersion(row, agentID))
}

export async function createAgentVersion(
  workspaceID: string, agentID: string, input: AgentVersionInput, signal?: AbortSignal,
) {
  const { data } = await api.post<unknown>(`${agentPath(agentID)}/versions`, versionBody(input), scope(workspaceID, signal))
  return parseVersion(data, agentID)
}

export async function listAgentEnvs(workspaceID: string, agentID: string, signal?: AbortSignal) {
  const { data } = await api.get<unknown>(`${agentPath(agentID)}/envs`, scope(workspaceID, signal))
  if (!Array.isArray(data)) throw new Error('Agent 环境列表响应无效')
  return data.map(row => parseEnv(row, agentID))
}

export async function promoteAgentVersion(
  workspaceID: string, agentID: string, env: string, versionID: string, signal?: AbortSignal,
) {
  const { data } = await api.post<{ status?: unknown }>(`${agentPath(agentID)}/promote`, {
    env, version_id: versionID,
  }, scope(workspaceID, signal))
  if (data?.status !== 'promoted') throw new Error('发布响应无效，请刷新环境指针确认结果')
  return 'promoted' as const
}
