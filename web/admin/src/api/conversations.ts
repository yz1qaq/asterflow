import { api } from './client'

export interface Conversation {
  id: string
  workspace_id: string
  agent_id: string
  agent_version_id: string
  user_id: string
  status: string
  classification: string
  started_at: string
  updated_at: string
  runtime_config_json?: string
}

export interface ConversationMessage {
  id: string
  conversation_id: string
  role: string
  content: string
  tool_calls?: { id: string; tool_id?: string; args?: string; result?: string; error?: string; latency_ms?: number }[]
  tool_call_id?: string
  created_at: string
}

const scope = (workspaceID: string, signal?: AbortSignal) => ({ headers: { 'X-Workspace-ID': workspaceID }, signal })
const path = (id: string) => `/conversations/${encodeURIComponent(id)}`

function parseConversation(value: unknown): Conversation {
  const row = value as Partial<Conversation> | null
  if (!row || typeof row.id !== 'string' || !row.id || typeof row.workspace_id !== 'string'
    || typeof row.agent_id !== 'string' || typeof row.agent_version_id !== 'string'
    || typeof row.user_id !== 'string' || typeof row.status !== 'string'
    || typeof row.started_at !== 'string') {
    throw new Error('会话响应无效，请刷新列表确认结果')
  }
  return {
    id: row.id, workspace_id: row.workspace_id, agent_id: row.agent_id,
    agent_version_id: row.agent_version_id, user_id: row.user_id, status: row.status,
    classification: typeof row.classification === 'string' ? row.classification : 'internal',
    runtime_config_json: typeof row.runtime_config_json === 'string' ? row.runtime_config_json : undefined,
    started_at: row.started_at, updated_at: typeof row.updated_at === 'string' ? row.updated_at : row.started_at,
  }
}

function parseMessage(value: unknown): ConversationMessage {
  const row = value as Partial<ConversationMessage> | null
  if (!row || typeof row.id !== 'string' || !row.id || typeof row.conversation_id !== 'string'
    || typeof row.role !== 'string' || typeof row.content !== 'string' || typeof row.created_at !== 'string') {
    throw new Error('会话消息响应无效，请刷新后重试')
  }
  return { ...row as ConversationMessage }
}

export async function listConversations(workspaceID: string, signal?: AbortSignal, page?: { limit: number; offset: number }) {
  const { data } = await api.get<unknown>('/conversations', { ...scope(workspaceID, signal), params: page })
  if (!Array.isArray(data)) throw new Error('会话列表响应无效')
  return data.map(parseConversation)
}

export async function createConversation(workspaceID: string, agentID: string, versionID: string, signal?: AbortSignal) {
  const { data } = await api.post<unknown>('/conversations', { agent_id: agentID, agent_version_id: versionID }, scope(workspaceID, signal))
  const conversation = parseConversation(data)
  if (conversation.workspace_id !== workspaceID || conversation.agent_id !== agentID || conversation.agent_version_id !== versionID) {
    throw new Error('会话和所选 Agent 版本不匹配，请刷新后重试')
  }
  return conversation
}

export async function deleteConversation(workspaceID: string, conversationID: string, signal?: AbortSignal) {
  await api.delete(path(conversationID), scope(workspaceID, signal))
}

export async function getConversation(workspaceID: string, conversationID: string, signal?: AbortSignal) {
  const { data } = await api.get<{ conversation?: unknown; messages?: unknown }>(path(conversationID), scope(workspaceID, signal))
  const conversation = parseConversation(data?.conversation)
  if (!Array.isArray(data?.messages)) throw new Error('会话消息响应无效')
  return { conversation, messages: data.messages.map(parseMessage) }
}
