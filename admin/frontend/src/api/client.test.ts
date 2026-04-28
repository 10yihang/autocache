import { describe, it, expect, vi, beforeEach } from 'vitest'
import { apiFetch } from './client.ts'

const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

function makeResponse(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: String(status),
    json: () => Promise.resolve(body),
  } as unknown as Response
}

beforeEach(() => {
  mockFetch.mockReset()
})

describe('apiFetch', () => {
  it('returns parsed JSON on 200', async () => {
    mockFetch.mockResolvedValueOnce(makeResponse(200, { key: 'value' }))
    const result = await apiFetch<{ key: string }>('/api/v1/test')
    expect(result).toEqual({ key: 'value' })
  })

  it('throws ApiError with code from error envelope on 4xx', async () => {
    mockFetch.mockResolvedValueOnce(
      makeResponse(404, { error: { code: 'ERR_NOT_FOUND', message: 'key not found' } }),
    )
    await expect(apiFetch('/api/v1/test')).rejects.toMatchObject({
      name: 'ApiError',
      code: 'ERR_NOT_FOUND',
      message: 'key not found',
      status: 404,
    })
  })

  it('throws ApiError with default code when no envelope', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 503,
      statusText: 'Service Unavailable',
      json: () => Promise.reject(new SyntaxError('not json')),
    } as unknown as Response)
    await expect(apiFetch('/api/v1/test')).rejects.toMatchObject({
      name: 'ApiError',
      code: 'ERR_HTTP_503',
      status: 503,
    })
  })

  it('throws ApiError with fallback when envelope missing fields', async () => {
    mockFetch.mockResolvedValueOnce(makeResponse(400, { error: {} }))
    await expect(apiFetch('/api/v1/test')).rejects.toMatchObject({
      name: 'ApiError',
      code: 'ERR_HTTP_400',
      status: 400,
    })
  })

  it('passes through RequestInit options', async () => {
    mockFetch.mockResolvedValueOnce(makeResponse(200, {}))
    await apiFetch('/api/v1/test', { method: 'DELETE' })
    expect(mockFetch).toHaveBeenCalledWith(
      '/api/v1/test',
      expect.objectContaining({ method: 'DELETE' }),
    )
  })
})
