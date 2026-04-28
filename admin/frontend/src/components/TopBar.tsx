import { useLocation } from 'react-router-dom'
import './TopBar.css'

type SSEStatus = 'connecting' | 'connected' | 'reconnecting' | 'error'

interface TopBarProps {
  sseStatus: SSEStatus
  nodeId?: string
  onThemeToggle: () => void
  currentTheme: 'dark' | 'light'
}

const BREADCRUMB_MAP: Record<string, string> = {
  '/': 'Overview',
  '/keys': 'Keys',
  '/cluster': 'Cluster',
  '/slots': 'Slots',
  '/metrics': 'Metrics',
  '/ops': 'Operations',
  '/console': 'Console',
}

const SSE_DOT_LABEL: Record<SSEStatus, string> = {
  connecting: 'Connecting…',
  connected: 'Live',
  reconnecting: 'Reconnecting…',
  error: 'Error',
}

export function TopBar({ sseStatus, nodeId, onThemeToggle, currentTheme }: TopBarProps) {
  const location = useLocation()
  const crumb = BREADCRUMB_MAP[location.pathname] ?? '—'

  return (
    <header className="topbar">
      <div className="topbar__breadcrumb">
        <span className="topbar__crumb-prefix">autocache</span>
        <span className="topbar__crumb-sep">/</span>
        <span className="topbar__crumb-page">{crumb}</span>
      </div>

      <div className="topbar__actions">
        <div className={`topbar__sse topbar__sse--${sseStatus}`}>
          <span className="topbar__sse-dot" />
          <span className="topbar__sse-label">{SSE_DOT_LABEL[sseStatus]}</span>
        </div>

        {nodeId && (
          <span className="topbar__node-badge">{nodeId.slice(0, 12)}</span>
        )}

        <button
          className="topbar__theme-btn"
          onClick={onThemeToggle}
          aria-label="Toggle theme"
        >
          {currentTheme === 'dark' ? (
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <circle cx="12" cy="12" r="5" />
              <path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42" strokeLinecap="round" />
            </svg>
          ) : (
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          )}
        </button>
      </div>
    </header>
  )
}
