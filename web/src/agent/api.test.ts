import { beforeAll, expect, it, vi } from 'vitest'
import axios, { AxiosError, AxiosHeaders, type AxiosResponse } from 'axios'

beforeAll(() => {
  const values = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => values.get(key) || null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  })
})
it('401 刷新重放保留请求 UUID 和正文，并使用新的认证令牌', async () => {
  const { api, tokens } = await import('../api')
  const { agentApi } = await import('./api')
  const requestId = crypto.randomUUID(),
    seen: { id: string; body: unknown; auth: string }[] = []
  tokens.set('old')
  tokens.setRefresh('refresh')
  api.defaults.adapter = async (config) => {
    const response: AxiosResponse = {
      config,
      data: {},
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
    }
    if (config.url?.includes('/auth/refresh'))
      return { ...response, data: { access_token: 'new', refresh_token: 'next' } }
    seen.push({
      id: String(config.headers.get('X-Request-ID')),
      body: config.data,
      auth: String(config.headers.get('Authorization')),
    })
    if (seen.length === 1)
      throw new AxiosError('unauthorized', 'ERR_BAD_REQUEST', config, undefined, {
        ...response,
        status: 401,
      })
    return { ...response, data: { configuration: { id: 'new-version' } } }
  }
  await agentApi.save('prompts', 'test_prompt', { text: '正文', variables: [] }, null, requestId)
  expect(seen).toHaveLength(2)
  expect(seen[0].id).toBe(requestId)
  expect(seen[1].id).toBe(requestId)
  expect(seen[1].body).toBe(seen[0].body)
  expect(seen[1].auth).toBe('Bearer new')
  tokens.clear()
  api.defaults.adapter = axios.defaults.adapter
})
