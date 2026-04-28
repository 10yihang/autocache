import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { OverviewPage } from './OverviewPage.tsx'
import type { OverviewResponse, MetricsSnapshot } from '../api/client.ts'

const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

const overviewData: OverviewResponse = {
  node: { hostname: 'cache-node-1', pid: 1234, uptime: '2h 30m', goroutines: 42 },
  build: { version: 'v0.1.0', go_version: 'go1.24.0' },
  memory: { alloc_mb: 64.5, total_alloc_mb: 128.0, sys_mb: 256.0, num_gc: 10 },
  store: { key_count: 5000, hits: 90000, misses: 10000, set_ops: 20000, get_ops: 100000, del_ops: 500, expired_keys: 100, evicted_keys: 50 },
}

function makeOkResponse(body: unknown): Response {
  return {
    ok: true,
    status: 200,
    statusText: 'OK',
    json: () => Promise.resolve(body),
  } as unknown as Response
}

beforeEach(() => {
  mockFetch.mockReset()
})

function renderPage(latest: MetricsSnapshot | null = null) {
  return render(
    <MemoryRouter>
      <OverviewPage latest={latest} />
    </MemoryRouter>,
  )
}

describe('OverviewPage', () => {
  it('renders StatCards after data loads', async () => {
    mockFetch.mockResolvedValueOnce(makeOkResponse(overviewData))
    renderPage()
    await waitFor(() => expect(screen.getByText('Keys')).toBeInTheDocument())
    expect(screen.getByText('Hit Rate')).toBeInTheDocument()
    expect(screen.getAllByText('Memory').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('Goroutines')).toBeInTheDocument()
  })

  it('shows loading spinner initially', () => {
    mockFetch.mockReturnValueOnce(new Promise(() => {}))
    renderPage()
    expect(document.querySelector('.loading-spinner')).toBeInTheDocument()
  })

  it('shows error state when fetch fails', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
      json: () => Promise.resolve({ error: { code: 'ERR_SERVER', message: 'internal error' } }),
    } as unknown as Response)
    renderPage()
    await waitFor(() => expect(screen.getByText('Failed to load overview')).toBeInTheDocument())
  })

  it('renders hostname in node section', async () => {
    mockFetch.mockResolvedValueOnce(makeOkResponse(overviewData))
    renderPage()
    await waitFor(() => expect(screen.getByText('cache-node-1')).toBeInTheDocument())
  })

  it('uses latest SSE snapshot for key count', async () => {
    mockFetch.mockResolvedValueOnce(makeOkResponse(overviewData))
    const latest: MetricsSnapshot = {
      timestamp: Date.now() / 1000,
      goroutines: 99,
      alloc_mb: 99,
      sys_mb: 200,
      num_gc: 5,
      key_count: 9999,
      hits: 100,
      misses: 0,
      set_ops: 10,
      get_ops: 100,
      del_ops: 0,
      expired_keys: 0,
      evicted_keys: 0,
    }
    renderPage(latest)
    await waitFor(() => expect(screen.getAllByText('10.0K').length).toBeGreaterThanOrEqual(1))
  })
})
