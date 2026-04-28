import { useState } from 'react'
import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar.tsx'
import { TopBar } from './TopBar.tsx'
import { ErrorBoundary } from './ErrorBoundary.tsx'
import './Layout.css'

type SSEStatus = 'connecting' | 'connected' | 'reconnecting' | 'error'

interface LayoutProps {
  sseStatus: SSEStatus
  nodeId?: string
}

export function Layout({ sseStatus, nodeId }: LayoutProps) {
  const [theme, setTheme] = useState<'dark' | 'light'>(() => {
    return (document.documentElement.getAttribute('data-theme') as 'dark' | 'light') ?? 'dark'
  })

  const toggleTheme = () => {
    const next = theme === 'dark' ? 'light' : 'dark'
    setTheme(next)
    document.documentElement.setAttribute('data-theme', next)
  }

  return (
    <div className="layout">
      <Sidebar />
      <div className="layout__main">
        <TopBar
          sseStatus={sseStatus}
          nodeId={nodeId}
          onThemeToggle={toggleTheme}
          currentTheme={theme}
        />
        <main className="layout__content">
          <ErrorBoundary>
            <Outlet />
          </ErrorBoundary>
        </main>
      </div>
    </div>
  )
}
