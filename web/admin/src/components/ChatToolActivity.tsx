import { Tag } from 'antd'
import type { ChatEvent } from '@/api/chat'

export type LiveToolCall = {
  id: string
  name: string
  arguments?: string
  status: 'running' | 'success' | 'error' | 'cancelled'
  output?: string
  error?: string
  latency?: number
}

const statusLabels = {
  running: { label: '执行中', color: 'processing' },
  success: { label: '已完成', color: 'success' },
  error: { label: '失败', color: 'error' },
  cancelled: { label: '已取消', color: 'default' },
}

export function applyToolEvent(calls: LiveToolCall[], event: ChatEvent): LiveToolCall[] {
  const data = event.data
  if (!data || typeof data.call_id !== 'string' || typeof data.name !== 'string') return calls

  const base = { id: data.call_id, name: data.name }
  if (event.type === 'tool_call') {
    if (calls.some(call => call.id === base.id)) return calls
    return [...calls, { ...base, status: 'running', arguments: typeof data.arguments === 'string' ? data.arguments : undefined }]
  }
  if (event.type !== 'tool_result' || (data.status !== 'success' && data.status !== 'error')) return calls

  const result: LiveToolCall = {
    ...base,
    status: data.status,
    output: typeof data.output === 'string' ? data.output : undefined,
    error: typeof data.error === 'string' ? data.error : undefined,
    latency: typeof data.latency_ms === 'number' ? data.latency_ms : undefined,
  }
  const existing = calls.find(call => call.id === base.id)
  if (!existing) return [...calls, result]
  return calls.map(call => call.id === base.id ? { ...call, ...result } : call)
}

export function formatToolJSON(value: string) {
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

export function ChatToolActivity({ calls }: { calls: LiveToolCall[] }) {
  if (calls.length === 0) return null

  return <section className="chat-tool-activity" aria-label="本轮工具">
    <h5>本轮工具</h5>
    {calls.map(call => <details key={call.id}>
      <summary>
        <span>{call.name}</span>
        <Tag color={statusLabels[call.status].color}>{statusLabels[call.status].label}</Tag>
        {call.latency !== undefined && <span className="chat-tool-latency">{call.latency} ms</span>}
      </summary>
      {call.arguments && <div>参数<pre>{formatToolJSON(call.arguments)}</pre></div>}
      {call.output !== undefined && <div>结果<pre>{formatToolJSON(call.output)}</pre></div>}
      {call.error && <div>{call.error}</div>}
    </details>)}
  </section>
}
