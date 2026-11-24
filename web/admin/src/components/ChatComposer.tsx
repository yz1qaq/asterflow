import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react'
import { Alert, Button, Input, Select, Space, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { listAgentVersions } from '@/api/agents'
import { getChatError, streamChat } from '@/api/chat'
import type { Conversation } from '@/api/conversations'
import { listPromptVersions, parsePromptVariables } from '@/api/prompts'

export function readStringField(json: string | undefined, key: string) {
  if (!json) return ''
  try {
    const value = (JSON.parse(json) as Record<string, unknown> | null)?.[key]
    return typeof value === 'string' ? value : ''
  } catch {
    return ''
  }
}

export function ChatComposer({ workspaceID, userID, conversation, firstTurn, onDone, onDraftChange }: {
  workspaceID: string
  userID: string
  conversation: Conversation
  firstTurn: boolean
  onDone: () => Promise<void>
  onDraftChange: Dispatch<SetStateAction<string>>
}) {
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [useTemplate, setUseTemplate] = useState(false)
  const [variables, setVariables] = useState('{}')
  const [versionID, setVersionID] = useState('')
  const [firstTurnCommitted, setFirstTurnCommitted] = useState(false)
  const abortRef = useRef<AbortController | null>(null)
  useEffect(() => () => abortRef.current?.abort(), [])

  const defaultVersionID = readStringField(conversation.runtime_config_json, 'user_prompt_version_id')
  const templateAvailable = firstTurn && !firstTurnCommitted && Boolean(defaultVersionID)
  const templateMode = templateAvailable && useTemplate
  const agentVersion = useQuery({
    queryKey: ['conversation-agent-version', userID, workspaceID, conversation.agent_id, conversation.agent_version_id],
    enabled: templateMode, retry: false,
    queryFn: async ({ signal }) => {
      const versions = await listAgentVersions(workspaceID, conversation.agent_id, signal)
      const pinned = versions.find(version => version.id === conversation.agent_version_id)
      if (!pinned) throw new Error('无法读取会话绑定的 Agent 版本')
      return pinned
    },
  })
  const promptID = agentVersion.isSuccess ? readStringField(agentVersion.data.snapshot_json, 'user_prompt_id') : ''
  const promptVersions = useQuery({
    queryKey: ['conversation-prompt-versions', userID, workspaceID, promptID],
    enabled: templateMode && Boolean(promptID), retry: false, refetchOnMount: 'always',
    queryFn: ({ signal }) => listPromptVersions(workspaceID, promptID, signal),
  })
  const choices = promptVersions.isSuccess ? promptVersions.data : []
  const defaultLabel = choices.find(version => version.id === defaultVersionID)?.version
  const canSend = !versionID || !templateMode || choices.some(version => version.id === versionID)

  async function send() {
    const text = input.trim()
    if (abortRef.current || !canSend || (!text && !templateMode)) return
    setError('')
    let vars: Record<string, unknown> | undefined
    if (templateMode) {
      try {
        vars = parsePromptVariables(variables)
      } catch {
        setError('模板变量必须是 JSON 对象，例如 {"order_id":"ORD-1001"}')
        return
      }
    }

    const controller = new AbortController()
    abortRef.current = controller
    setBusy(true)
    onDraftChange('')
    try {
      let completed = false
      for await (const event of streamChat(
        workspaceID, conversation.id, text, vars, controller.signal, templateMode && versionID ? versionID : undefined,
      )) {
        if (event.type === 'model_delta' && typeof event.data?.content === 'string') {
          const content = event.data.content
          onDraftChange(prev => prev + content)
        }
        if (event.type === 'run_failed') {
          const reason = event.data?.message
          throw new Error(typeof reason === 'string' ? reason : '本轮生成失败')
        }
        if (event.type === 'run_cancelled') throw new Error('已取消')
        if (event.type === 'run_completed') completed = true
      }
      if (!completed) throw new Error('连接已断开，请刷新会话确认本轮是否保存')
      setFirstTurnCommitted(true)
      setInput('')
      await onDone()
      onDraftChange('')
    } catch (err) {
      onDraftChange('')
      setError(getChatError(err))
    } finally {
      setBusy(false)
      abortRef.current = null
    }
  }

  return <div className="chat-composer" aria-label="聊天输入">
    {error && <Alert className="user-create-error" role="alert" type="error" showIcon message={error} />}
    {templateAvailable && <Space direction="vertical" style={{ width: '100%', marginBottom: 12 }}>
      <Button disabled={busy} aria-expanded={templateMode} onClick={() => {
        setUseTemplate(!templateMode)
        setError('')
        setVersionID('')
      }}>{templateMode ? '使用普通消息' : '使用首轮模板'}</Button>
      {templateMode && <>
        {promptID && <Select aria-label="模板版本" style={{ minWidth: 220 }}
          value={versionID} disabled={busy} loading={promptVersions.isFetching} onChange={setVersionID}
          options={[
            { value: '', label: defaultLabel ? `会话默认版本（v${defaultLabel}）` : '会话默认版本' },
            ...choices.map(version => ({ value: version.id, label: `指定 v${version.version}` })),
          ]} />}
        {(agentVersion.isError || promptVersions.isError) &&
          <Typography.Text type="secondary">暂时无法读取可选版本，仍可使用会话默认版本。</Typography.Text>}
        <label htmlFor="first-turn-vars">模板变量（JSON）</label>
        <Input.TextArea id="first-turn-vars" rows={3} value={variables} disabled={busy}
          onChange={event => setVariables(event.target.value)} placeholder='{"order_id":"ORD-1001"}' />
      </>}
    </Space>}
    <Space.Compact style={{ width: '100%' }}>
      <Input.TextArea autoSize={{ minRows: 1, maxRows: 4 }} value={input}
        onChange={event => setInput(event.target.value)}
        placeholder={templateMode ? '可选补充说明' : '输入消息，Enter 发送'}
        disabled={busy} onPressEnter={event => {
          if (event.shiftKey || event.nativeEvent.isComposing) return
          event.preventDefault()
          void send()
        }} />
      {busy
        ? <Button danger onClick={() => abortRef.current?.abort()}>停止</Button>
        : <Button type="primary" disabled={!canSend || (!input.trim() && !templateMode)} onClick={() => void send()}>发送</Button>}
    </Space.Compact>
  </div>
}
