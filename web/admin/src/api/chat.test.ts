import { afterEach, describe, expect, it, vi } from 'vitest'
import { getChatError, streamChat } from './chat'
import { useAuthStore } from '@/store/authStore'

function sseResponse(lines: string[], status = 200) {
  const encoder = new TextEncoder()
  const body = new ReadableStream({
    start(controller) {
      for (const line of lines) controller.enqueue(encoder.encode(`data: ${line}\n\n`))
      controller.close()
    },
  })
  return new Response(body, { status, headers: { 'Content-Type': 'text/event-stream' } })
}

describe('streamChat', () => {
  afterEach(() => vi.restoreAllMocks())

  it('yields ordered SSE events and sends scoped headers', async () => {
    vi.spyOn(useAuthStore, 'getState').mockReturnValue({ token: 'test-token' } as never)
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(sseResponse([
      '{"seq":1,"type":"run_started"}',
      '{"seq":2,"type":"model_delta","data":{"content":"he"}}',
      '{"seq":3,"type":"model_delta","data":{"content":"llo"}}',
      '{"seq":4,"type":"run_completed","data":{"assistant_message_id":"m1"}}',
    ]))

    const controller = new AbortController()
    const events = []
    for await (const event of streamChat('space-a', 'conv-1', 'hi', undefined, controller.signal)) {
      events.push(event)
    }

    expect(events.map(event => event.type)).toEqual(['run_started', 'model_delta', 'model_delta', 'run_completed'])
    expect(events[1].data).toEqual({ content: 'he' })
    expect(fetchMock).toHaveBeenCalledOnce()
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/v1/conversations/conv-1/chat')
    expect(init?.method).toBe('POST')
    expect((init?.headers as Record<string, string>)['X-Workspace-ID']).toBe('space-a')
    expect(JSON.parse(init?.body as string)).toEqual({ input: 'hi' })
  })

  it('preserves an explicit empty template variable object', async () => {
    vi.spyOn(useAuthStore, 'getState').mockReturnValue({ token: 'test-token' } as never)
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(sseResponse([]))
    const stream = streamChat('space-a', 'conv-1', '', {}, new AbortController().signal)

    expect(await stream.next()).toEqual({ done: true, value: undefined })
    expect(JSON.parse(fetchMock.mock.calls[0][1]?.body as string)).toEqual({ input: '', vars: {} })
  })

  it('sends an explicitly selected template version', async () => {
    vi.spyOn(useAuthStore, 'getState').mockReturnValue({ token: 'test-token' } as never)
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(sseResponse([]))
    const stream = streamChat('space-a', 'conv-1', '', { order_id: 'ORD-42' }, new AbortController().signal, 'v2-id')

    await stream.next()
    expect(JSON.parse(fetchMock.mock.calls[0][1]?.body as string)).toEqual({
      input: '', vars: { order_id: 'ORD-42' }, user_prompt_version_id: 'v2-id',
    })
  })

  it('throws a readable error on a non-2xx response', async () => {
    vi.spyOn(useAuthStore, 'getState').mockReturnValue({ token: 'test-token' } as never)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(sseResponse(['oops'], 404))

    const controller = new AbortController()
    await expect(async () => {
      for await (const _event of streamChat('space-a', 'conv-1', 'hi', undefined, controller.signal)) {
        // consume
      }
    }).rejects.toThrow()
  })

  it('reports abort as a readable message', async () => {
    vi.spyOn(useAuthStore, 'getState').mockReturnValue({ token: 'test-token' } as never)
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new DOMException('aborted', 'AbortError'))
    expect(getChatError(new DOMException('aborted', 'AbortError'))).toBe('已取消')
  })
})
