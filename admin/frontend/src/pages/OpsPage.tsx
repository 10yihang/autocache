import { useEffect, useState, useCallback } from 'react'
import { apiFetch } from '../api/client.ts'
import type { ReplicationStatusResponse, TieredStatsResponse, AuditResponse } from '../api/client.ts'
import { LoadingSpinner } from '../components/LoadingSpinner.tsx'
import { EmptyState } from '../components/EmptyState.tsx'
import { DataTable } from '../components/DataTable.tsx'
import type { Column } from '../components/DataTable.tsx'
import { Badge } from '../components/Badge.tsx'
import { formatBytes, formatTimestamp } from '../api/format.ts'
import './OpsPage.css'

export function OpsPage() {
  const [replication, setReplication] = useState<ReplicationStatusResponse | null>(null)
  const [tiered, setTiered] = useState<TieredStatsResponse | null>(null)
  const [audit, setAudit] = useState<AuditResponse | null>(null)
  const [loading, setLoading] = useState(true)

  const loadAll = useCallback(() => {
    Promise.allSettled([
      apiFetch<ReplicationStatusResponse>('/api/v1/replication/status'),
      apiFetch<TieredStatsResponse>('/api/v1/tiered/stats'),
      apiFetch<AuditResponse>('/api/v1/audit'),
    ]).then(([replR, tierR, auditR]) => {
      if (replR.status === 'fulfilled') setReplication(replR.value)
      if (tierR.status === 'fulfilled') setTiered(tierR.value)
      if (auditR.status === 'fulfilled') setAudit(auditR.value)
      setLoading(false)
    })
  }, [])

  useEffect(() => {
    loadAll()
    const timer = setInterval(loadAll, 10000)
    return () => clearInterval(timer)
  }, [loadAll])

  type SlotLogRow = { slot: number; first_lsn: string; last_lsn: string }
  const slotCols: Column<SlotLogRow>[] = [
    { key: 'slot', header: 'Slot', mono: true },
    { key: 'first_lsn', header: 'First LSN', mono: true },
    { key: 'last_lsn', header: 'Last LSN', mono: true },
  ]

  type AckRow = { slot: number; epoch: string; node_id: string; applied_lsn: string }
  const ackCols: Column<AckRow>[] = [
    { key: 'slot', header: 'Slot', mono: true },
    { key: 'epoch', header: 'Epoch', mono: true },
    { key: 'node_id', header: 'Node ID', mono: true, render: (r) => r.node_id.slice(0, 16) },
    { key: 'applied_lsn', header: 'Applied LSN', mono: true },
  ]

  type AuditRow = { timestamp: string; remote_addr: string; user: string; action: string; target: string; result: string }
  const auditCols: Column<AuditRow>[] = [
    { key: 'timestamp', header: 'Time', mono: true, render: (r) => formatTimestamp(r.timestamp), width: '160px' },
    { key: 'user', header: 'User', mono: true },
    { key: 'action', header: 'Action', render: (r) => <Badge variant="mute">{r.action}</Badge> },
    { key: 'target', header: 'Target', mono: true },
    { key: 'result', header: 'Result', render: (r) => (
      <span style={{ color: r.result.includes('denied') || r.result.includes('501') ? 'var(--danger)' : 'var(--fg-mute)', fontFamily: 'var(--mono)', fontSize: 11 }}>{r.result}</span>
    )},
  ]

  if (loading) return <div className="page-center"><LoadingSpinner size={24} /></div>

  return (
    <div className="ops-page">
      <div className="page-section">
        <div className="page-section__header">
          <span className="page-section__title">Replication</span>
          <Badge variant={replication?.available ? 'ok' : 'mute'}>
            {replication?.available ? 'available' : 'disabled'}
          </Badge>
        </div>
        {!replication?.available ? (
          <EmptyState title="Replication not active" description="Start with -cluster-enabled and replica nodes." />
        ) : (
          <>
            <DataTable
              columns={slotCols}
              rows={(replication.slots ?? []) as SlotLogRow[]}
              rowKey={(r) => String(r.slot)}
              emptyMessage="No slot log entries"
            />
            {(replication.acks ?? []).length > 0 && (
              <>
                <div className="ops-page__section-subtitle">Replica ACKs</div>
                <DataTable
                  columns={ackCols}
                  rows={(replication.acks ?? []) as AckRow[]}
                  rowKey={(r) => `${r.slot}-${r.node_id}`}
                />
              </>
            )}
          </>
        )}
      </div>

      <div className="page-section">
        <div className="page-section__header">
          <span className="page-section__title">Tiered Storage</span>
          <Badge variant={tiered?.available ? 'ok' : 'mute'}>
            {tiered?.available ? 'available' : 'disabled'}
          </Badge>
        </div>
        {!tiered?.available ? (
          <EmptyState title="Tiered storage not active" description="Start with -tiered-enabled to activate hot/warm tiers." />
        ) : (
          <div className="ops-tier-grid">
            <div className="ops-tier-card">
              <span className="ops-tier-card__label">Hot Tier</span>
              <span className="ops-tier-card__value">{tiered.hot_tier_keys?.toLocaleString() ?? '—'} keys</span>
              <span className="ops-tier-card__size">{formatBytes(tiered.hot_tier_size ?? 0)}</span>
            </div>
            <div className="ops-tier-card">
              <span className="ops-tier-card__label">Warm Tier</span>
              <span className="ops-tier-card__value">{tiered.warm_tier_keys?.toLocaleString() ?? '—'} keys</span>
              <span className="ops-tier-card__size">{formatBytes(tiered.warm_tier_size ?? 0)}</span>
            </div>
            <div className="ops-tier-card">
              <span className="ops-tier-card__label">Cold Tier</span>
              <span className="ops-tier-card__value">{tiered.cold_tier_keys?.toLocaleString() ?? '—'} keys</span>
              <span className="ops-tier-card__size">{formatBytes(tiered.cold_tier_size ?? 0)}</span>
            </div>
            <div className="ops-tier-card ops-tier-card--total">
              <span className="ops-tier-card__label">Total</span>
              <span className="ops-tier-card__value">{tiered.total_keys?.toLocaleString() ?? '—'} keys</span>
              <span className="ops-tier-card__size">{formatBytes(tiered.total_size ?? 0)}</span>
            </div>
            <div className="ops-tier-card">
              <span className="ops-tier-card__label">Migrations ↑</span>
              <span className="ops-tier-card__value">{tiered.migrations_up?.toLocaleString() ?? '0'}</span>
            </div>
            <div className="ops-tier-card">
              <span className="ops-tier-card__label">Migrations ↓</span>
              <span className="ops-tier-card__value">{tiered.migrations_down?.toLocaleString() ?? '0'}</span>
            </div>
          </div>
        )}
      </div>

      <div className="page-section">
        <div className="page-section__header">
          <span className="page-section__title">Audit Log ({audit?.entries.length ?? 0})</span>
          <button className="btn btn--mute" onClick={loadAll} style={{ fontSize: 11 }}>Refresh</button>
        </div>
        <DataTable
          columns={auditCols}
          rows={(audit?.entries ?? []) as AuditRow[]}
          rowKey={(_, i) => String(i)}
          emptyMessage="No audit entries"
        />
      </div>
    </div>
  )
}
