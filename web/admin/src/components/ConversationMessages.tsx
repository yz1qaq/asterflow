import { Space, Tag, Typography } from 'antd'
import type { ConversationMessage } from '@/api/conversations'

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
    <div className="chat-message-content">{message.content || '（空内容）'}</div>
    {message.tool_calls?.length ? <pre className="prompt-text">{JSON.stringify(message.tool_calls, null, 2)}</pre> : null}
  </article>)}</>
}
