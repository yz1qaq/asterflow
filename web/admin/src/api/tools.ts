import { api } from './client'

export const TOOL_SOURCES = {
  rest_api: 'REST API',
  mcp_server: 'MCP',
  internal_sdk: '内部 SDK',
  code_execution: '代码执行',
  a2a: 'A2A',
}

export type ToolSource = keyof typeof TOOL_SOURCES

export interface Tool {
  id: string
  name: string
  source_type: ToolSource
  description: string
  sensitive: boolean
}

export interface ToolVersion {
  id: string
  version: number
  status: string
  has_auth: boolean
  schema_json: string
  endpoint_config: string
  endpoint_config_redacted?: boolean
  retry_policy: string
  created_at: string
}

export interface CreateToolInput {
  name: string
  source_type: ToolSource
  description?: string
  schema_json: string
  endpoint_config?: string
  auth_config?: string
  sensitive: boolean
}

export interface CreateToolVersionInput {
  schema_json?: string
  endpoint_config?: string
  auth_config?: string
  retry_policy?: string
}

export interface ToolTestRun {
  id: string
  tool_version_id: string
  status: 'success' | 'error'
  latency_ms: number
  output: string
  error?: string
}

const toolPath = (toolID: string) => `/tools/${encodeURIComponent(toolID)}`
const toolVersionsPath = (toolID: string) => `${toolPath(toolID)}/versions`
const scope = (workspaceID: string, signal: AbortSignal) => ({ headers: { 'X-Workspace-ID': workspaceID }, signal })
const hasSource = (source: string): source is ToolSource => Object.prototype.hasOwnProperty.call(TOOL_SOURCES, source)

function parseTool(value: unknown, workspaceID: string): Tool {
  const row = value as (Partial<Tool> & { workspace_id?: string }) | null
  if (!row || row.workspace_id !== workspaceID || typeof row.id !== 'string' || !row.id ||
    typeof row.name !== 'string' || typeof row.source_type !== 'string' || !hasSource(row.source_type) ||
    typeof row.description !== 'string' || typeof row.sensitive !== 'boolean') {
    throw new Error('工具响应无效，请刷新列表确认结果')
  }
  return { id: row.id, name: row.name, source_type: row.source_type, description: row.description, sensitive: row.sensitive }
}

function parseVersion(value: unknown, toolID: string): ToolVersion {
  const row = value as (Partial<ToolVersion> & { tool_id?: string }) | null
  if (!row || row.tool_id !== toolID || typeof row.id !== 'string' || !row.id ||
    typeof row.version !== 'number' || !Number.isSafeInteger(row.version) || row.version < 1 ||
    typeof row.status !== 'string' || typeof row.has_auth !== 'boolean' ||
    typeof row.schema_json !== 'string' || typeof row.endpoint_config !== 'string' ||
    typeof row.retry_policy !== 'string' || typeof row.created_at !== 'string') {
    throw new Error('工具版本响应无效，请刷新版本列表')
  }

  // 版本界面只接收鉴权状态，不接收凭据配置字段。
  return {
    id: row.id, version: row.version, status: row.status, has_auth: row.has_auth,
    schema_json: row.schema_json, endpoint_config: row.endpoint_config, retry_policy: row.retry_policy,
    endpoint_config_redacted: row.endpoint_config_redacted === true,
    created_at: row.created_at,
  }
}

export async function listTools(workspaceID: string, signal: AbortSignal) {
  const { data } = await api.get<unknown>('/tools', scope(workspaceID, signal))
  if (!Array.isArray(data)) throw new Error('工具列表响应无效')
  return data.map(row => parseTool(row, workspaceID))
}

export async function listToolVersions(workspaceID: string, toolID: string, signal: AbortSignal) {
  const { data } = await api.get<unknown>(
    toolVersionsPath(toolID), scope(workspaceID, signal),
  )
  if (!Array.isArray(data)) throw new Error('工具版本列表响应无效')
  return data.map(row => parseVersion(row, toolID))
}

export async function createTool(workspaceID: string, input: CreateToolInput, signal: AbortSignal) {
  const { data } = await api.post<unknown>('/tools', {
    name: input.name.trim(),
    source_type: input.source_type,
    description: input.description?.trim() ?? '',
    schema_json: input.schema_json.trim(),
    endpoint_config: input.endpoint_config?.trim() ?? '',
    auth_config: input.auth_config?.trim() ?? '',
    sensitive: input.sensitive,
  }, scope(workspaceID, signal))
  return parseTool(data, workspaceID)
}

export async function createToolVersion(workspaceID: string, toolID: string, input: CreateToolVersionInput, signal: AbortSignal) {
  const { data } = await api.post<unknown>(toolVersionsPath(toolID), {
    schema_json: input.schema_json?.trim() ?? '',
    endpoint_config: input.endpoint_config?.trim() ?? '',
    auth_config: input.auth_config?.trim() ?? '',
    retry_policy: input.retry_policy?.trim() ?? '',
  }, scope(workspaceID, signal))
  return parseVersion(data, toolID)
}

export async function testTool(workspaceID: string, toolID: string, input: string, signal: AbortSignal): Promise<ToolTestRun> {
  try {
    JSON.parse(input)
  } catch {
    throw new Error('请输入有效的 JSON')
  }
  const request = scope(workspaceID, signal)
  // 校验语法后保留原始 JSON，避免重新序列化改变大整数。
  const { data } = await api.post<unknown>(`${toolPath(toolID)}/test`, `{"input":${input}}`, {
    ...request, headers: { ...request.headers, 'Content-Type': 'application/json' }, timeout: 45_000,
  })
  const row = data as (Partial<ToolTestRun> & { tool_id?: string }) | null
  if (!row || row.tool_id !== toolID || typeof row.id !== 'string' || !row.id ||
    typeof row.tool_version_id !== 'string' || !row.tool_version_id ||
    (row.status !== 'success' && row.status !== 'error') ||
    typeof row.latency_ms !== 'number' || !Number.isSafeInteger(row.latency_ms) || row.latency_ms < 0 ||
    typeof row.output !== 'string' || (row.status === 'error' && typeof row.error !== 'string')) {
    throw new Error('工具试跑响应无效')
  }
  return {
    id: row.id, tool_version_id: row.tool_version_id, status: row.status, latency_ms: row.latency_ms, output: row.output,
    error: row.status === 'error' ? row.error : undefined,
  }
}

export async function publishToolVersion(workspaceID: string, toolID: string, versionID: string, signal: AbortSignal) {
  const { data } = await api.post<{ status?: unknown }>(
    `${toolVersionsPath(toolID)}/${encodeURIComponent(versionID)}/publish`,
    undefined, scope(workspaceID, signal),
  )
  if (data?.status !== 'published') throw new Error('无法确认版本是否已发布，请刷新列表')
}
