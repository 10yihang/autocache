// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPaste, readPaste } from '../api/clipboard'
import { PastePage } from './PastePage'

vi.mock('../api/clipboard', () => ({
  createPaste: vi.fn(),
  readPaste: vi.fn(),
}))

function renderPastePage() {
  return render(
    <MemoryRouter initialEntries={['/p/demo123']}>
      <Routes>
        <Route path="/p/:code" element={<PastePage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('PastePage', () => {
  afterEach(() => {
    cleanup()
  })

  beforeEach(() => {
    vi.mocked(readPaste).mockResolvedValue({
      paste: {
        code: 'demo123',
        content: 'original text',
        metadata: {
          ttl: '1h',
          created_at: '2026-03-22T00:00:00Z',
          expires_at: '2026-03-22T01:00:00Z',
        },
      },
    })
    vi.mocked(createPaste).mockReset()
  })

  it('loads shared content into an editable textarea and saves a new paste', async () => {
    vi.mocked(createPaste).mockResolvedValue({
      code: 'saved123',
      share_url: 'http://clipboard.test/p/saved123',
      raw_url: 'http://clipboard.test/raw/saved123',
      paste: {
        code: 'saved123',
        content: 'updated text',
        metadata: {
          ttl: '1h',
          created_at: '2026-03-22T00:00:00Z',
          expires_at: '2026-03-22T01:00:00Z',
        },
      },
    })

    renderPastePage()

    const editor = await screen.findByLabelText('编辑内容')
    await userEvent.clear(editor)
    await userEvent.type(editor, 'updated text')
    await userEvent.click(screen.getByRole('button', { name: '保存为新链接' }))

    expect(createPaste).toHaveBeenCalledWith({ content: 'updated text', ttl: '1h' })
    await screen.findByText('已保存为新的分享链接：')
    expect(screen.getByRole('link', { name: 'http://clipboard.test/p/saved123' })).toBeTruthy()
  })

  it('supports ctrl+s to save the edited content', async () => {
    vi.mocked(createPaste).mockResolvedValue({
      code: 'saved123',
      share_url: 'http://clipboard.test/p/saved123',
      raw_url: 'http://clipboard.test/raw/saved123',
      paste: {
        code: 'saved123',
        content: 'shortcut text',
        metadata: {
          ttl: '1h',
          created_at: '2026-03-22T00:00:00Z',
          expires_at: '2026-03-22T01:00:00Z',
        },
      },
    })

    renderPastePage()

    const editor = await screen.findByLabelText('编辑内容')
    await userEvent.clear(editor)
    await userEvent.type(editor, 'shortcut text')
    fireEvent.keyDown(editor, { key: 's', ctrlKey: true })

    await waitFor(() => {
      expect(createPaste).toHaveBeenCalledWith({ content: 'shortcut text', ttl: '1h' })
    })
  })

  it('supports cmd+s to save the edited content', async () => {
    vi.mocked(createPaste).mockResolvedValue({
      code: 'saved123',
      share_url: 'http://clipboard.test/p/saved123',
      raw_url: 'http://clipboard.test/raw/saved123',
      paste: {
        code: 'saved123',
        content: 'shortcut text',
        metadata: {
          ttl: '1h',
          created_at: '2026-03-22T00:00:00Z',
          expires_at: '2026-03-22T01:00:00Z',
        },
      },
    })

    renderPastePage()

    const editor = await screen.findByLabelText('编辑内容')
    await userEvent.clear(editor)
    await userEvent.type(editor, 'shortcut text')
    fireEvent.keyDown(editor, { key: 's', metaKey: true })

    await waitFor(() => {
      expect(createPaste).toHaveBeenCalledWith({ content: 'shortcut text', ttl: '1h' })
    })
  })
})
