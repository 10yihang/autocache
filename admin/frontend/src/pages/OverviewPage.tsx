import { useEffect, useState } from 'react'
import { apiFetch, ApiError } from '../api/client.ts'
import type { OverviewResponse, MetricsSnapshot } from '../api/client.ts'
import { StatCard } from '../components/StatCard.tsx'
import { LoadingSpinner } from '../components/LoadingSpinner.tsx'
import { EmptyState } from '../components/EmptyState.tsx'
import { Badge } from '../components/Badge.tsx'
import { formatBytes, formatNumber } from '../api/format.ts'
import './OverviewPage.css'

interface OverviewPageProps {
  latest: MetricsSnapshot | null
}

export function OverviewPage({ latest }: OverviewPageProps) {
  const [data, setData] = useState<OverviewResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    // eslint-disable-next-line react-hooks/set-state-in-effect -- loading flag must be set synchronously before async fetch
    setLoading(true)
    apiFetch<OverviewResponse>('/api/v1/overview')
      .then((d) => { if (!cancelled) { setData(d); setLoading(false) } })
      .catch((e: unknown) => {
        if (!cancelled) {
          setError(e instanceof ApiError ? e.message : 'Failed to load overview')
          setLoading(false)
        }
      })
    return () => { cancelled = true }
  }, [])

  if (loading) {
    return (
      <div className="page-center">
        <LoadingSpinner size={24} />
      </div>
    )
  }

  if (error) {
    return (
      <EmptyState
        title="Failed to load overview"
        description={error}
        icon={<svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><circle cx="12" cy="12" r="10" /><line x1="12" y1="8" x2="12" y2="12" strokeLinecap="round" /><line x1="12" y1="16" x2="12.01" y2="16" strokeLinecap="round" /></svg>}
      />
    )
  }

  if (!data) return null

  const keyCount = latest?.key_count ?? data.store.key_count
  const hits = latest?.hits ?? data.store.hits
  const misses = latest?.misses ?? data.store.misses
  const total = hits + misses
  const hitRate = total > 0 ? ((hits / total) * 100).toFixed(1) : '—'
  const allocMB = latest?.alloc_mb ?? data.memory.alloc_mb
  const goroutines = latest?.goroutines ?? data.node.goroutines

  return (
    <div className="overview-page">
      <div className="stat-row">
        <StatCard label="Keys" value={formatNumber(keyCount)} style={{ animationDelay: '0ms' }} />
        <StatCard label="Hit Rate" value={hitRate} unit="%" style={{ animationDelay: '60ms' }} />
        <StatCard label="Memory" value={allocMB.toFixed(1)} unit="MB" style={{ animationDelay: '120ms' }} />
        <StatCard label="Goroutines" value={String(goroutines)} style={{ animationDelay: '180ms' }} />
      </div>

      <div className="overview-grid">
        <div className="page-section" style={{ animationDelay: '240ms' }}>
          <div className="page-section__header">
            <span className="page-section__title">Node</span>
          </div>
          <div className="info-card">
            <div className="info-card__row">
              <span className="info-card__label">Hostname</span>
              <span className="info-card__value mono">{data.node.hostname}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">PID</span>
              <span className="info-card__value mono">{data.node.pid}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">Uptime</span>
              <span className="info-card__value mono">{data.node.uptime}</span>
            </div>
            {data.cluster && (
              <>
                <div className="info-card__row">
                  <span className="info-card__label">Node ID</span>
                  <span className="info-card__value mono">{data.cluster.node_id}</span>
                </div>
                <div className="info-card__row">
                  <span className="info-card__label">Role</span>
                  <Badge variant={data.cluster.role === 'primary' ? 'ok' : 'info'}>{data.cluster.role}</Badge>
                </div>
              </>
            )}
          </div>
        </div>

        <div className="page-section" style={{ animationDelay: '300ms' }}>
          <div className="page-section__header">
            <span className="page-section__title">Build</span>
          </div>
          <div className="info-card">
            <div className="info-card__row">
              <span className="info-card__label">Version</span>
              <span className="info-card__value mono">{data.build.version}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">Go Version</span>
              <span className="info-card__value mono">{data.build.go_version}</span>
            </div>
          </div>
        </div>

        <div className="page-section" style={{ animationDelay: '360ms' }}>
          <div className="page-section__header">
            <span className="page-section__title">Store</span>
          </div>
          <div className="info-card">
            <div className="info-card__row">
              <span className="info-card__label">Hits</span>
              <span className="info-card__value mono">{formatNumber(data.store.hits)}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">Misses</span>
              <span className="info-card__value mono">{formatNumber(data.store.misses)}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">Set Ops</span>
              <span className="info-card__value mono">{formatNumber(data.store.set_ops)}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">Get Ops</span>
              <span className="info-card__value mono">{formatNumber(data.store.get_ops)}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">Expired</span>
              <span className="info-card__value mono">{formatNumber(data.store.expired_keys)}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">Evicted</span>
              <span className="info-card__value mono">{formatNumber(data.store.evicted_keys)}</span>
            </div>
          </div>
        </div>

        <div className="page-section" style={{ animationDelay: '420ms' }}>
          <div className="page-section__header">
            <span className="page-section__title">Memory</span>
          </div>
          <div className="info-card">
            <div className="info-card__row">
              <span className="info-card__label">Alloc</span>
              <span className="info-card__value mono">{formatBytes(data.memory.alloc_mb * 1024 * 1024)}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">Total Alloc</span>
              <span className="info-card__value mono">{formatBytes(data.memory.total_alloc_mb * 1024 * 1024)}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">Sys</span>
              <span className="info-card__value mono">{formatBytes(data.memory.sys_mb * 1024 * 1024)}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">GC Cycles</span>
              <span className="info-card__value mono">{data.memory.num_gc}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
