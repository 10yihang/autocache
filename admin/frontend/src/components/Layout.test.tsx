import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Layout } from './Layout.tsx'

function renderLayout(sseStatus: 'connecting' | 'connected' | 'reconnecting' | 'error' = 'connecting') {
  return render(
    <MemoryRouter>
      <Layout sseStatus={sseStatus} />
    </MemoryRouter>,
  )
}

describe('Layout', () => {
  it('renders sidebar nav links', () => {
    renderLayout()
    expect(screen.getAllByText('Overview').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('Keys').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('Cluster').length).toBeGreaterThanOrEqual(1)
  })

  it('renders topbar breadcrumb', () => {
    renderLayout()
    expect(screen.getAllByText(/autocache/i).length).toBeGreaterThanOrEqual(1)
  })

  it('renders layout grid container', () => {
    const { container } = renderLayout()
    expect(container.querySelector('.layout')).toBeInTheDocument()
    expect(container.querySelector('.layout__main')).toBeInTheDocument()
    expect(container.querySelector('.layout__content')).toBeInTheDocument()
  })

  it('passes sseStatus connected to TopBar', () => {
    renderLayout('connected')
    expect(screen.getByText('Live')).toBeInTheDocument()
  })
})
