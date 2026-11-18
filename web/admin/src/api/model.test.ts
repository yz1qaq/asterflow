import { getProjectBinding, listWorkspaceProfileVersions, saveProjectBinding } from './modelBindings'
import { listModelProfiles, createModelProfile, listModelProfileVersions, createModelProfileVersion, getModelProfileError } from './modelProfiles'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AxiosError } from 'axios'
import { api } from './client'
import { canManageModelAccounts, createModelDeployment, listModelDeployments, updateModelDeploymentPricing, getModelDeploymentError, createModelAccount, getModelAccountError, listModelAccounts, rotateModelAccountAPIKey } from './model'

afterEach(() => vi.restoreAllMocks())
const account = { id: 'account-1', workspace_id: 'space-a', name: 'Primary', kind: 'openai-compatible', base_url: 'https://example.invalid/v1', status: 'active', has_api_key: true, created_by: 'actor', created_at: '2026-11-17T00:00:00Z', updated_at: '2026-11-17T00:00:00Z' }

describe('model accounts', () => {
  it('scopes reads explicitly and forwards cancellation', async () => {
    const signal = new AbortController().signal
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: [account] })
    await expect(listModelAccounts('space-a', signal)).resolves.toEqual([account])
    expect(get).toHaveBeenCalledWith('/model-accounts', { headers: { 'X-Workspace-ID': 'space-a' }, signal })
  })
  it('rejects HTML and cross-workspace responses', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValueOnce({ data: '<html>' })
      .mockResolvedValueOnce({ data: [{ ...account, workspace_id: 'space-b' }] })
    await expect(listModelAccounts('space-a')).rejects.toThrow('响应无效')
    await expect(listModelAccounts('space-a')).rejects.toThrow('响应无效')
    expect(get).toHaveBeenCalledTimes(2)
  })
  it('rejects malformed credential-state responses', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: [{ ...account, has_api_key: 'yes' }] })
    await expect(listModelAccounts('space-a')).rejects.toThrow('响应无效')
  })
  it('whitelists create inputs and keeps server-owned fields out of the body', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({ data: account })
    const request = { name: 'Primary', kind: 'openai-compatible', base_url: account.base_url, api_key: 'test-only-key', created_by: 'injected', workspace_id: 'injected' }
    await createModelAccount('space-a', request)
    expect(post).toHaveBeenCalledWith('/model-accounts', {
      name: request.name, kind: request.kind, base_url: request.base_url, api_key: request.api_key,
    }, { headers: { 'X-Workspace-ID': 'space-a' } })
  })
  it('retains only public account fields from responses', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: [{ ...account, api_key: 'unexpected', api_key_ciphertext: 'unexpected' }] })
    await expect(listModelAccounts('space-a')).resolves.toEqual([account])
  })
  it('encodes IDs and requires confirmed 204 rotation', async () => {
    const put = vi.spyOn(api, 'put').mockResolvedValueOnce({ status: 204 }).mockResolvedValueOnce({ status: 200 })
    await rotateModelAccountAPIKey('space-a', 'account/id', 'test-only-key')
    expect(put).toHaveBeenCalledWith('/model-accounts/account%2Fid/api-key', { api_key: 'test-only-key' }, { headers: { 'X-Workspace-ID': 'space-a' } })
    await expect(rotateModelAccountAPIKey('space-a', 'account/id', 'test-only-key')).rejects.toThrow('结果未确认')
  })
  it('mirrors the server write roles', () => {
    for (const role of ['owner', 'admin', 'editor']) expect(canManageModelAccounts(role)).toBe(true)
    for (const role of ['member', 'viewer', '', 'unknown']) expect(canManageModelAccounts(role)).toBe(false)
  })
  it('does not expose raw error data and explains uncertain writes', () => {
    const error = Object.assign(new AxiosError('raw credential must stay private'), { response: { status: 409, data: { api_key: 'raw' } } })
    expect(getModelAccountError(error, true)).toContain('同名')
    expect(getModelAccountError(new AxiosError('raw', 'ECONNABORTED'), true)).toContain('结果未确认')
    expect(getModelAccountError(error, true)).not.toContain('raw')
  })
})

describe('model deployments', () => {
  const deployment = {
    id: 'deployment-1', workspace_id: 'space-a', provider_account_id: 'account-1',
    name: 'Primary', model_name: 'example-model', region: '', timeout_ms: 120000, max_retries: 0,
    input_price_per_million: 1.25, output_price_per_million: 2.5, cached_input_price_per_million: 0.1,
    status: 'active', created_by: 'actor', created_at: '2026-11-17T00:00:00Z', updated_at: '2026-11-17T00:00:00Z',
  }
  it('scopes deployment reads and forwards cancellation', async () => {
    const signal = new AbortController().signal
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: [deployment] })
    await expect(listModelDeployments('space-a', signal)).resolves.toEqual([deployment])
    expect(get).toHaveBeenCalledWith('/model-deployments', { headers: { 'X-Workspace-ID': 'space-a' }, signal })
  })
  it('rejects HTML and cross-workspace responses', async () => {
    vi.spyOn(api, 'get').mockResolvedValueOnce({ data: '<html>' }).mockResolvedValueOnce({ data: [{ ...deployment, workspace_id: 'space-b' }] })
    await expect(listModelDeployments('space-a')).rejects.toThrow('响应无效')
    await expect(listModelDeployments('space-a')).rejects.toThrow('响应无效')
  })
  it('rejects malformed prices and integer fields', async () => {
    const get = vi.spyOn(api, 'get')
    for (const invalid of [
      { input_price_per_million: NaN }, { output_price_per_million: Infinity },
      { cached_input_price_per_million: -1 }, { timeout_ms: 0 }, { max_retries: -1 },
    ]) {
      get.mockResolvedValueOnce({ data: [{ ...deployment, ...invalid }] })
      await expect(listModelDeployments('space-a')).rejects.toThrow('响应无效')
    }
  })
  it('creates only configuration fields, omitting actor and workspace body fields', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({ data: deployment })
    const request = {
      provider_account_id: 'account-1', name: 'Primary', model_name: 'example-model', region: '',
      timeout_ms: 120000, max_retries: 0, input_price_per_million: 1.25,
      output_price_per_million: 2.5, cached_input_price_per_million: 0.1,
    }
    await createModelDeployment('space-a', { ...request, created_by: 'injected', workspace_id: 'injected' } as typeof request)
    expect(post).toHaveBeenCalledWith('/model-deployments', request, { headers: { 'X-Workspace-ID': 'space-a' } })
  })
  it('updates all three prices including zero and returns persisted timestamps', async () => {
    const updated = { ...deployment, input_price_per_million: 0, output_price_per_million: 3.5, cached_input_price_per_million: 0 }
    const put = vi.spyOn(api, 'put').mockResolvedValue({ data: updated })
    const prices = { input_price_per_million: 0, output_price_per_million: 3.5, cached_input_price_per_million: 0 }
    await expect(updateModelDeploymentPricing('space-a', 'deployment-1', prices)).resolves.toEqual(updated)
    expect(put).toHaveBeenCalledWith('/model-deployments/deployment-1/pricing', prices, { headers: { 'X-Workspace-ID': 'space-a' } })
  })
  it('rejects pricing results for a different deployment', async () => {
    vi.spyOn(api, 'put').mockResolvedValue({ data: { ...deployment, id: 'another-deployment' } })
    await expect(updateModelDeploymentPricing('space-a', 'deployment-1', {
      input_price_per_million: 0, output_price_per_million: 0, cached_input_price_per_million: 0,
    })).rejects.toThrow('响应无效')
  })
  it('keeps only the public deployment fields', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: [{ ...deployment, private_extra: 'ignored' }] })
    await expect(listModelDeployments('space-a')).resolves.toEqual([deployment])
  })
  it('explains a price conflict separately from a duplicate deployment name', () => {
    const error = Object.assign(new AxiosError('private'), {
      response: { status: 409, data: { code: 'deployment_price_required' } },
    })
    expect(getModelDeploymentError(error, true)).toContain('输入价和输出价不能同时为 0')
  })
  it('explains name conflicts and uncertain writes without exposing backend data', () => {
    const error = Object.assign(new AxiosError('raw internal details'), { response: { status: 409, data: 'private' } })
    expect(getModelDeploymentError(error, true)).toContain('同名部署')
    expect(getModelDeploymentError(error, true)).not.toContain('raw')
    expect(getModelDeploymentError(new AxiosError('raw', 'ECONNABORTED'), true)).toContain('结果未确认')
  })
})

describe('model profiles', () => {
 const p = { id:'p1', workspace_id:'a', name:'Support', description:'', created_by:'actor', created_at:'2026-11-18T00:00:00Z', updated_at:'2026-11-18T00:00:00Z' }
 const v = { id:'v1', profile_id:'p1', version:1, primary_deployment_id:'d1', fallback_deployment_ids:[], classification_max:'internal', created_by:'actor', created_at:p.created_at }
 const input = { name:p.name, description:'', primary_deployment_id:'d1', fallback_deployment_ids:[], classification_max:'internal', created_by:'inject', workspace_id:'inject', version:99 }
 it('scopes profile reads and forwards cancellation',async()=>{
  const signal=new AbortController().signal
  const get=vi.spyOn(api,'get').mockResolvedValue({data:[p]})
  expect(await listModelProfiles('a',signal)).toEqual([p])
  expect(get).toHaveBeenCalledWith('/model-profiles',{headers:{'X-Workspace-ID':'a'},signal})
 })
 it('rejects invalid or foreign profiles',async()=>{
  const get=vi.spyOn(api,'get')
  for(const data of ['<html>',[{...p,workspace_id:'b'}],[{...p,id:''}]]){
   get.mockResolvedValueOnce({data});await expect(listModelProfiles('a')).rejects.toThrow()
  }
 })
 it('whitelists creation fields and validates bundle relation',async()=>{
  const post=vi.spyOn(api,'post').mockResolvedValueOnce({data:{profile:p,version:v}}).mockResolvedValueOnce({data:{profile:p,version:{...v,profile_id:'p2'}}})
  expect(await createModelProfile('a',input)).toEqual({profile:p,version:v})
  expect(post).toHaveBeenCalledWith('/model-profiles',{name:p.name,description:'',primary_deployment_id:'d1',fallback_deployment_ids:[],classification_max:'internal'},{headers:{'X-Workspace-ID':'a'}})
  await expect(createModelProfile('a',input)).rejects.toThrow()
 })
 it('requires the first version when creating a profile',async()=>{
  vi.spyOn(api,'post').mockResolvedValue({data:{profile:p,version:{...v,version:2}}})
  await expect(createModelProfile('a',input)).rejects.toThrow()
 })
 it('uses encoded URL identity and only configuration when appending',async()=>{
  const post=vi.spyOn(api,'post').mockResolvedValue({data:{...v,profile_id:'p/id',version:2}})
  await createModelProfileVersion('a','p/id',input)
  expect(post).toHaveBeenCalledWith('/model-profiles/p%2Fid/versions',{primary_deployment_id:'d1',fallback_deployment_ids:[],classification_max:'internal'},{headers:{'X-Workspace-ID':'a'}})
 })
 it('scopes version reads and rejects invalid history',async()=>{
  const signal=new AbortController().signal
  const get=vi.spyOn(api,'get').mockResolvedValue({data:[v]})
  expect(await listModelProfileVersions('a','p1',signal)).toEqual([v])
  expect(get).toHaveBeenCalledWith('/model-profiles/p1/versions',{headers:{'X-Workspace-ID':'a'},signal})
  for(const patch of [{profile_id:'p2'},{version:0},{version:1.5},{fallback_deployment_ids:null},{fallback_deployment_ids:[1]}]){
   get.mockResolvedValueOnce({data:[{...v,...patch}]});await expect(listModelProfileVersions('a','p1')).rejects.toThrow()
  }
 })
 it('reports uncertain writes without leaking raw messages',()=>{
  expect(getModelProfileError(new AxiosError('secret','ECONNABORTED'),true)).not.toContain('secret')
  expect(getModelProfileError(new AxiosError('secret','ECONNABORTED'),true)).toContain('结果未确认')
 })
})

describe('model bindings', () => {
  const body = { env: 'dev', model_profile_version_id: 'version-1', monthly_budget: 125.5, rpm_limit: 20, tpm_limit: 3000 }
  const saved = { ...body, workspace_id: 'space-a', updated_at: '2026-11-18T00:00:00Z' }
  const failure = (status: number, code: string) => Object.assign(new AxiosError('private'), { response: { status, data: { code } } })

  it('treats only the explicit missing binding as an empty environment', async () => {
    const signal = new AbortController().signal
    const get = vi.spyOn(api, 'get').mockRejectedValueOnce(failure(404, 'model_binding_not_found'))
      .mockRejectedValueOnce(failure(404, 'workspace_not_found')).mockRejectedValueOnce(failure(403, 'model_binding_not_found'))
    await expect(getProjectBinding('space-a', 'prod', signal)).resolves.toBeNull()
    await expect(getProjectBinding('space-a', 'prod', signal)).rejects.toThrow()
    await expect(getProjectBinding('space-a', 'prod', signal)).rejects.toThrow()
    expect(get).toHaveBeenCalledWith('/model-bindings', { headers: { 'X-Workspace-ID': 'space-a' }, params: { env: 'prod' }, signal })
  })
  it('rejects bindings for another environment or workspace', async () => {
    vi.spyOn(api, 'get').mockResolvedValueOnce({ data: { ...saved, env: 'prod' } })
      .mockResolvedValueOnce({ data: { ...saved, workspace_id: 'space-b' } })
    await expect(getProjectBinding('space-a', 'dev')).rejects.toThrow('响应无效')
    await expect(getProjectBinding('space-a', 'dev')).rejects.toThrow('响应无效')
  })
  it('saves only writable fields, and checks the returned version', async () => {
    const put = vi.spyOn(api, 'put').mockResolvedValueOnce({ data: saved })
      .mockResolvedValueOnce({ data: { ...saved, model_profile_version_id: 'other' } })
    const draft = { ...body, workspace_id: 'space-b', updated_at: 'injected' }
    await expect(saveProjectBinding('space-a', draft)).resolves.toEqual(saved)
    expect(put).toHaveBeenCalledWith('/model-bindings', body, { headers: { 'X-Workspace-ID': 'space-a' } })
    await expect(saveProjectBinding('space-a', body)).rejects.toThrow('结果未确认')
  })
  it('loads versions across profiles with the workspace header and rejects malformed rows', async () => {
    const row = { id: 'v1', profile_id: 'p1', version: 1, primary_deployment_id: 'd1',
      fallback_deployment_ids: [], classification_max: 'internal', created_by: 'actor', created_at: saved.updated_at }
    const another = { ...row, profile_id: 'p2', id: 'v2' }
    const get = vi.spyOn(api, 'get').mockResolvedValueOnce({ data: [row, another] })
      .mockResolvedValueOnce({ data: [{ ...row, profile_id: '' }] })
    const signal = new AbortController().signal
    await expect(listWorkspaceProfileVersions('space-a', signal)).resolves.toEqual([row, another])
    expect(get).toHaveBeenCalledWith('/model-profile-versions', { headers: { 'X-Workspace-ID': 'space-a' }, signal })
    await expect(listWorkspaceProfileVersions('space-a')).rejects.toThrow('响应无效')
  })
})
