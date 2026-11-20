import { AxiosError } from 'axios'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './client'
import {
  canWritePrompts, completeRollout, createPrompt, createPromptVersion, diffPromptVersions, getActiveExperiment,
  getPromptError, listEnvEvents, listPromptEnvs, listPrompts, listPromptVersions, listRolloutEvents, parsePromptVariables,
  promotePromptVersion, renderPrompt, rollbackPromptVersion, rollbackRollout, startExperiment, startRollout,
  updateRolloutTraffic,
} from './prompts'

const workspaceID = 'space-a'
const prompt = { id: 'prompt/id', workspace_id: workspaceID, name: '订单核验', category: '', created_at: '2026-11-19T00:00:00Z' }
const input = { template: '订单：{{.order_id}}\n', variables_schema: '{ "required": ["order_id"] }\n' }
const version = { id: 'version-1', prompt_id: prompt.id, version: 1, created_at: prompt.created_at, ...input }
const scope = (signal?: AbortSignal) => ({ headers: { 'X-Workspace-ID': workspaceID }, signal })
const httpError = (status: number, error: string) => Object.assign(new AxiosError('internal message'), { response: { status, data: { error } } })

afterEach(() => vi.restoreAllMocks())

describe('prompt API', () => {
  it('scopes both reads explicitly, encodes the prompt ID and forwards cancellation', async () => {
    const signal = new AbortController().signal
    const get = vi.spyOn(api, 'get').mockResolvedValueOnce({ data: [prompt] }).mockResolvedValueOnce({ data: [version] })
    await expect(listPrompts(workspaceID, signal)).resolves.toEqual([prompt])
    await expect(listPromptVersions(workspaceID, prompt.id, signal)).resolves.toEqual([version])
    expect(get).toHaveBeenNthCalledWith(1, '/prompts', scope(signal))
    expect(get).toHaveBeenNthCalledWith(2, '/prompts/prompt%2Fid/versions', scope(signal))
  })

  it('accepts empty lists but rejects rows from another workspace or prompt', async () => {
    vi.spyOn(api, 'get').mockResolvedValueOnce({ data: [] }).mockResolvedValueOnce({ data: [] })
      .mockResolvedValueOnce({ data: [{ ...prompt, workspace_id: 'space-b' }] })
      .mockResolvedValueOnce({ data: [{ ...version, prompt_id: 'other' }] })
      .mockResolvedValueOnce({ data: [{ ...version, variables_schema: {} }] })
    await expect(listPrompts(workspaceID)).resolves.toEqual([])
    await expect(listPromptVersions(workspaceID, prompt.id)).resolves.toEqual([])
    await expect(listPrompts(workspaceID)).rejects.toThrow('响应无效')
    await expect(listPromptVersions(workspaceID, prompt.id)).rejects.toThrow('响应无效')
    await expect(listPromptVersions(workspaceID, prompt.id)).rejects.toThrow('响应无效')
  })

  it('sends schema and template verbatim, with category only on create and no server-owned inputs', async () => {
    const signal = new AbortController().signal
    const post = vi.spyOn(api, 'post').mockResolvedValueOnce({ data: { prompt, version } })
      .mockResolvedValueOnce({ data: { ...version, version: 2 } })
    const draft = { ...input, name: prompt.name, category: '订单', workspace_id: 'space-b', created_by: 'actor', version: 42 }
    await expect(createPrompt(workspaceID, draft, signal)).resolves.toEqual({ prompt, version })
    await expect(createPromptVersion(workspaceID, prompt.id, draft, signal)).resolves.toMatchObject({ version: 2 })
    expect(post).toHaveBeenNthCalledWith(1, '/prompts', { name: prompt.name, category: '订单', ...input }, scope(signal))
    expect(post).toHaveBeenNthCalledWith(2, '/prompts/prompt%2Fid/versions', input, scope(signal))
  })

  it('checks the created prompt/version relationship and initial version number', async () => {
    vi.spyOn(api, 'post').mockResolvedValueOnce({ data: { prompt, version: { ...version, version: 2 } } })
      .mockResolvedValueOnce({ data: { prompt, version: { ...version, prompt_id: 'other' } } })
      .mockResolvedValueOnce({ data: { ...version, prompt_id: 'other', version: 2 } })
    const draft = { ...input, name: prompt.name }
    await expect(createPrompt(workspaceID, draft)).rejects.toThrow('初始版本响应无效')
    await expect(createPrompt(workspaceID, draft)).rejects.toThrow('版本响应无效')
    await expect(createPromptVersion(workspaceID, prompt.id, input)).rejects.toThrow('版本响应无效')
  })

  it('posts a version ID and JSON object for preview, returning the text unchanged', async () => {
    const signal = new AbortController().signal
    const rawOutput = '<strong>订单</strong>\n'
    const post = vi.spyOn(api, 'post').mockResolvedValueOnce({ data: { rendered: rawOutput } })
      .mockResolvedValueOnce({ data: { rendered: '' } }).mockResolvedValueOnce({ data: { rendered: 12 } })
    const vars = parsePromptVariables('{"order_id":"ORD-1001","quantity":0,"urgent":false}')
    await expect(renderPrompt(workspaceID, prompt.id, version.id, vars, signal)).resolves.toBe(rawOutput)
    expect(post).toHaveBeenNthCalledWith(1, '/prompts/prompt%2Fid/render', {
      version_id: version.id, vars: { order_id: 'ORD-1001', quantity: 0, urgent: false },
    }, scope(signal))
    await expect(renderPrompt(workspaceID, prompt.id, version.id, {})).resolves.toBe('')
    await expect(renderPrompt(workspaceID, prompt.id, version.id, {})).rejects.toThrow('预览响应无效')
  })

  it('scopes environment reads and rejects bindings of another prompt', async () => {
    const signal = new AbortController().signal
    const binding = { prompt_id: prompt.id, env: 'dev', version_id: version.id, updated_at: prompt.created_at }
    const get = vi.spyOn(api, 'get').mockResolvedValueOnce({ data: [binding] })
      .mockResolvedValueOnce({ data: [{ ...binding, prompt_id: 'other' }] })
      .mockResolvedValueOnce({ data: [{ ...binding, env: '' }] })
    await expect(listPromptEnvs(workspaceID, prompt.id, signal)).resolves.toEqual([binding])
    expect(get).toHaveBeenNthCalledWith(1, '/prompts/prompt%2Fid/envs', scope(signal))
    await expect(listPromptEnvs(workspaceID, prompt.id)).rejects.toThrow('环境指针响应无效')
    await expect(listPromptEnvs(workspaceID, prompt.id)).rejects.toThrow('环境指针响应无效')
  })

  it('publishes an explicit version to one environment and checks the acknowledgement', async () => {
    const signal = new AbortController().signal
    const post = vi.spyOn(api, 'post').mockResolvedValueOnce({ data: { status: 'promoted' } })
      .mockResolvedValueOnce({ data: { status: 'ok' } })
    await expect(promotePromptVersion(workspaceID, prompt.id, 'staging', version.id, signal)).resolves.toBe('promoted')
    expect(post).toHaveBeenNthCalledWith(1, '/prompts/prompt%2Fid/promote', {
      env: 'staging', version_id: version.id,
    }, scope(signal))
    await expect(promotePromptVersion(workspaceID, prompt.id, 'dev', version.id)).rejects.toThrow('发布响应无效')
  })

  it('rolls a single environment back to an explicit version and checks the acknowledgement', async () => {
    const signal = new AbortController().signal
    const post = vi.spyOn(api, 'post').mockResolvedValueOnce({ data: { status: 'rolled_back' } })
      .mockResolvedValueOnce({ data: { status: 'promoted' } })
    await expect(rollbackPromptVersion(workspaceID, prompt.id, 'dev', version.id, signal)).resolves.toBe('rolled_back')
    expect(post).toHaveBeenNthCalledWith(1, '/prompts/prompt%2Fid/rollback', {
      env: 'dev', version_id: version.id,
    }, scope(signal))
    await expect(rollbackPromptVersion(workspaceID, prompt.id, 'dev', version.id)).rejects.toThrow('回滚响应无效')
  })

  it('requests a scoped version diff and returns the text unchanged', async () => {
    const signal = new AbortController().signal
    const diff = '- old line\n+ new line\n'
    const get = vi.spyOn(api, 'get').mockResolvedValueOnce({ data: { diff } }).mockResolvedValueOnce({ data: { diff: 1 } })
    await expect(diffPromptVersions(workspaceID, prompt.id, 1, 2, signal)).resolves.toBe(diff)
    expect(get).toHaveBeenNthCalledWith(1, '/prompts/prompt%2Fid/diff', {
      headers: { 'X-Workspace-ID': workspaceID }, signal, params: { from: 1, to: 2 },
    })
    await expect(diffPromptVersions(workspaceID, prompt.id, 1, 2)).rejects.toThrow('版本差异响应无效')
  })

  it('reads the active experiment per environment and rejects foreign or malformed rows', async () => {
    const experiment = {
      id: 'exp-1', prompt_id: prompt.id, env: 'dev', baseline_version_id: version.id, candidate_version_id: 'v2',
      traffic_percent: 20, variants: [{ version_id: 'v2', traffic: 20 }], status: 'active',
      started_at: prompt.created_at, created_at: prompt.created_at,
    }
    const signal = new AbortController().signal
    const get = vi.spyOn(api, 'get').mockResolvedValueOnce({ data: null })
      .mockResolvedValueOnce({ data: experiment })
      .mockResolvedValueOnce({ data: { ...experiment, prompt_id: 'other' } })
      .mockResolvedValueOnce({ data: { ...experiment, variants: [{ version_id: 1 }] } })
    await expect(getActiveExperiment(workspaceID, prompt.id, 'dev', signal)).resolves.toBeNull()
    await expect(getActiveExperiment(workspaceID, prompt.id, 'dev', signal)).resolves.toMatchObject({ id: 'exp-1', traffic_percent: 20 })
    expect(get).toHaveBeenNthCalledWith(2, '/prompts/prompt%2Fid/experiments/active', {
      headers: { 'X-Workspace-ID': workspaceID }, signal, params: { env: 'dev' },
    })
    await expect(getActiveExperiment(workspaceID, prompt.id, 'dev')).rejects.toThrow('实验响应无效')
    await expect(getActiveExperiment(workspaceID, prompt.id, 'dev')).rejects.toThrow('实验响应无效')
  })

  it('sends rollout lifecycle requests and validates their acknowledgements', async () => {
    const experiment = {
      id: 'exp-1', prompt_id: prompt.id, env: 'dev', baseline_version_id: version.id, candidate_version_id: 'v2',
      traffic_percent: 20, variants: [{ version_id: 'v2', traffic: 20 }], status: 'active',
      started_at: prompt.created_at, created_at: prompt.created_at,
    }
    const signal = new AbortController().signal
    const post = vi.spyOn(api, 'post').mockResolvedValueOnce({ data: experiment })
      .mockResolvedValueOnce({ data: { status: 'completed' } })
      .mockResolvedValueOnce({ data: { status: 'rolled_back' } })
      .mockResolvedValueOnce({ data: { status: 'ok' } })
    const put = vi.spyOn(api, 'put').mockResolvedValueOnce({ data: { ...experiment, traffic_percent: 50 } })
    await expect(startRollout(workspaceID, prompt.id, 'dev', 'v2', 20, signal)).resolves.toMatchObject({ id: 'exp-1' })
    expect(post).toHaveBeenNthCalledWith(1, '/prompts/prompt%2Fid/rollouts', {
      env: 'dev', candidate_version_id: 'v2', traffic: 20,
    }, scope(signal))
    await expect(updateRolloutTraffic(workspaceID, prompt.id, 'dev', 50, signal)).resolves.toMatchObject({ traffic_percent: 50 })
    expect(put).toHaveBeenNthCalledWith(1, '/prompts/prompt%2Fid/rollouts/traffic', { env: 'dev', traffic: 50 }, scope(signal))
    await expect(completeRollout(workspaceID, prompt.id, 'dev', signal)).resolves.toBe('completed')
    await expect(rollbackRollout(workspaceID, prompt.id, 'dev', signal)).resolves.toBe('rolled_back')
    await expect(completeRollout(workspaceID, prompt.id, 'dev')).rejects.toThrow('完成上线响应无效')
  })

  it('starts a multi-variant experiment with ordered variants', async () => {
    const experiment = {
      id: 'exp-2', prompt_id: prompt.id, env: 'dev', traffic_percent: 0,
      variants: [{ version_id: version.id, traffic: 30 }, { version_id: 'v2', traffic: 30 }],
      status: 'active', started_at: prompt.created_at, created_at: prompt.created_at,
    }
    const signal = new AbortController().signal
    const post = vi.spyOn(api, 'post').mockResolvedValueOnce({ data: experiment })
    await expect(startExperiment(workspaceID, prompt.id, 'dev', experiment.variants, signal))
      .resolves.toMatchObject({ id: 'exp-2' })
    expect(post).toHaveBeenNthCalledWith(1, '/prompts/prompt%2Fid/experiments', {
      env: 'dev', variants: experiment.variants,
    }, scope(signal))
  })

  it('reads environment publish events and rejects foreign rows', async () => {
    const event = {
      id: 'env-ev-1', prompt_id: prompt.id, env: 'dev', action: 'published',
      from_version_id: version.id, to_version_id: 'v2', actor: 'user-1', detail: '', created_at: prompt.created_at,
    }
    const signal = new AbortController().signal
    const get = vi.spyOn(api, 'get').mockResolvedValueOnce({ data: [event] })
      .mockResolvedValueOnce({ data: [{ ...event, prompt_id: 'other' }] })
    await expect(listEnvEvents(workspaceID, prompt.id, 'dev', signal)).resolves.toEqual([event])
    expect(get).toHaveBeenNthCalledWith(1, '/prompts/prompt%2Fid/env-events', {
      headers: { 'X-Workspace-ID': workspaceID }, signal, params: { env: 'dev' },
    })
    await expect(listEnvEvents(workspaceID, prompt.id, 'dev')).rejects.toThrow('环境事件响应无效')
  })

  it('reads rollout events for one experiment and rejects foreign rows', async () => {
    const event = {
      id: 'ev-1', experiment_id: 'exp-1', action: 'started', from_traffic: 0, to_traffic: 20,
      actor: 'user-1', detail: '', created_at: prompt.created_at,
    }
    const signal = new AbortController().signal
    const get = vi.spyOn(api, 'get').mockResolvedValueOnce({ data: [event] })
      .mockResolvedValueOnce({ data: [{ ...event, experiment_id: 'other' }] })
    await expect(listRolloutEvents(workspaceID, prompt.id, 'exp-1', signal)).resolves.toEqual([event])
    expect(get).toHaveBeenNthCalledWith(1, '/prompts/prompt%2Fid/experiments/exp-1/events', scope(signal))
    await expect(listRolloutEvents(workspaceID, prompt.id, 'exp-1')).rejects.toThrow('实验事件响应无效')
  })

  it('rejects non-object JSON and treats an empty vars field as an empty object', () => {
    expect(parsePromptVariables('  ')).toEqual({})
    for (const value of ['[]', 'null', '1', 'false', '"literal"', '{']) {
      expect(() => parsePromptVariables(value)).toThrow('必须是 JSON 对象')
    }
  })

  it.each([
    ['owner', true], ['admin', true], ['editor', true], ['member', false], ['viewer', false], ['unknown', false],
  ])('maps %s to write permission %s', (role, allowed) => {
    expect(canWritePrompts(role)).toBe(allowed)
  })

  it('reports missing variables and actionable errors without showing raw 5xx details', () => {
    for (const error of ['missing required prompt variable "order_id"', 'render prompt: map has no entry for key "order_id"']) {
      expect(getPromptError(httpError(400, `invalid prompt argument: ${error}`), 'preview')).toBe('缺少预览变量 "order_id"')
    }
    expect(getPromptError(httpError(400, 'invalid prompt argument: parse prompt template: unexpected EOF'), 'save')).toContain('unexpected EOF')
    expect(getPromptError(httpError(403, 'forbidden'), 'save')).toContain('权限不足')
    expect(getPromptError(httpError(404, 'not found'), 'read')).toContain('不在当前空间')
    expect(getPromptError(new AxiosError('timeout', 'ECONNABORTED'), 'save')).toContain('结果未确认')
    expect(getPromptError(httpError(500, 'private detail'), 'preview')).not.toContain('private detail')
  })
})
