import { useAuthStore } from '@/store/authStore'

export interface ChatEvent {
  seq: number
  type: string
  data?: Record<string, unknown>
}

// streamChat sends one turn and yields ordered SSE events. The caller owns the
// AbortController: aborting cancels the request and releases the server lease.
export async function* streamChat(
  workspaceID: string,
  conversationID: string,
  input: string,
  vars: Record<string, unknown> | undefined,
  signal: AbortSignal,
  userPromptVersionID?: string,
): AsyncGenerator<ChatEvent, void, unknown> {
  const token = useAuthStore.getState().token
  if (!token) throw new Error('登录已失效，请重新登录')

  const response = await fetch(`/api/v1/conversations/${encodeURIComponent(conversationID)}/chat`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
      'X-Workspace-ID': workspaceID,
    },
    body: JSON.stringify({ input, vars, user_prompt_version_id: userPromptVersionID }),
    signal,
  })
  if (!response.ok || !response.body) {
    let detail = ''
    try {
      const problem: unknown = await response.json()
      if (problem && typeof problem === 'object' && 'error' in problem && typeof problem.error === 'string') {
        detail = problem.error
      }
    } catch { /* use the HTTP status */ }
    throw new Error(detail || `聊天请求失败（${response.status}）`)
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      let newline: number
      while ((newline = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0, newline).trim()
        buffer = buffer.slice(newline + 1)
        if (!line.startsWith('data: ')) continue
        const payload = line.slice('data: '.length).trim()
        if (!payload) continue
        let event: ChatEvent
        try {
          event = JSON.parse(payload)
        } catch {
          continue
        }
        yield event
      }
    }
  } finally {
    reader.releaseLock()
  }
}

export function getChatError(error: unknown): string {
  if (error instanceof Error && error.name === 'AbortError') return '已取消'
  if (error instanceof Error) return error.message
  return '聊天失败，请重试'
}
