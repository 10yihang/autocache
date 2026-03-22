import { afterEach, describe, expect, it, vi } from 'vitest'
import { readPaste } from './clipboard'

describe('readPaste', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('deduplicates in-flight reads for the same paste code', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => ({
        paste: {
          code: 'abc123',
          content: 'hello',
          metadata: {
            ttl: '1h',
            created_at: '2026-03-22T00:00:00Z',
            expires_at: '2026-03-22T01:00:00Z',
          },
        },
      }),
    }))

    vi.stubGlobal('fetch', fetchMock)

    const first = readPaste('abc123')
    const second = readPaste('abc123')

    expect(fetchMock).toHaveBeenCalledTimes(1)
    await expect(first).resolves.toMatchObject({ paste: { code: 'abc123' } })
    await expect(second).resolves.toMatchObject({ paste: { code: 'abc123' } })
  })

  it('clears in-flight cache after a failed request and allows retry', async () => {
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new Error('network failed'))
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          paste: {
            code: 'retry123',
            content: 'retry ok',
            metadata: {
              ttl: '1h',
              created_at: '2026-03-22T00:00:00Z',
              expires_at: '2026-03-22T01:00:00Z',
            },
          },
        }),
      })

    vi.stubGlobal('fetch', fetchMock)

    const first = readPaste('retry123')
    const second = readPaste('retry123')

    expect(fetchMock).toHaveBeenCalledTimes(1)
    await expect(first).rejects.toThrow('network failed')
    await expect(second).rejects.toThrow('network failed')

    const third = readPaste('retry123')
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await expect(third).resolves.toMatchObject({ paste: { code: 'retry123' } })
  })

  it('does not cache settled responses across separate reads', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          paste: {
            code: 'again123',
            content: 'first',
            metadata: {
              ttl: '1h',
              created_at: '2026-03-22T00:00:00Z',
              expires_at: '2026-03-22T01:00:00Z',
            },
          },
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          paste: {
            code: 'again123',
            content: 'second',
            metadata: {
              ttl: '1h',
              created_at: '2026-03-22T00:00:00Z',
              expires_at: '2026-03-22T01:00:00Z',
            },
          },
        }),
      })

    vi.stubGlobal('fetch', fetchMock)

    await expect(readPaste('again123')).resolves.toMatchObject({ paste: { content: 'first' } })
    await expect(readPaste('again123')).resolves.toMatchObject({ paste: { content: 'second' } })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
