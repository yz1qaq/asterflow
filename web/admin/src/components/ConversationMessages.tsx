import { Space, Tag, Typography } from 'antd'
import type { ConversationMessage } from '@/api/conversations'
import { formatToolJSON } from '@/components/ChatToolActivity'

export const timeLabel = (value: string) =>
  Number.isNaN(Date.parse(value)) ? '—' : new Date(value).toLocaleString('zh-CN', { hour12: false })

const roles: Record<string, string> = { user: '用户', assistant: '助手', tool: '工具' }

export function ConversationMessages({ messages }: { messages: ConversationMessage[] }) {
  return <>{messages.map(message => <article key={message.id} className={`chat-message chat-message--${message.role}`}>
    <Space wrap size="small">
      <Typography.Text strong>{roles[message.role] ?? message.role}</Typography.Text>
      <Typography.Text type="secondary" className="chat-message-time">{timeLabel(message.created_at)}</Typography.Text>
      {message.tool_call_id && <Tag>Tool call {message.tool_call_id.slice(0, 8)}</Tag>}
    </Space>
    {message.role === 'tool'
      ? <details>
          <summary>查看工具结果</summary>
          <pre className="prompt-text">{formatToolJSON(message.content)}</pre>
        </details>
      : message.content || !message.tool_calls?.length
        ? <div className="chat-message-content">{message.content || '（空内容）'}</div>
        : null}
    {message.tool_calls?.map(call => <details key={call.id}>
      <summary><span>{call.name ?? '工具调用'}</span> <Tag color={call.error ? 'error' : 'success'}>
        {call.error ? '失败' : '已完成'}
      </Tag></summary>
      {call.args !== undefined && <pre className="prompt-text">{formatToolJSON(call.args)}</pre>}
      {call.error && <Typography.Text type="danger">{call.error}</Typography.Text>}
    </details>)}
  </article>)}</>
}
