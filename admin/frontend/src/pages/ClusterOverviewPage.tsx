import { useEffect, useState } from 'react'
import { apiFetch, ApiError } from '../api/client.ts'
import type { ClusterOverviewResponse, ClusterOverviewNode } from '../api/client.ts'
import { StatCard } from '../components/StatCard.tsx'
import { Badge } from '../components/Badge.tsx'
import { DataTable } from '../components/DataTable.tsx'
import type { Column } from '../components/DataTable.tsx'
import { LoadingSpinner } from '../components/LoadingSpinner.tsx'
import { EmptyState } from '../components/EmptyState.tsx'
import './ClusterOverviewPage.css'

const ERR_503 = 'ERR_CLUSTER_DISABLED'

export function ClusterOverviewPage() {
  const [data, setData] = useState<ClusterOverviewResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [clusterDisabled, setClusterDisabled] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    apiFetch<ClusterOverviewResponse>('/api/v1/cluster/overview')
      .then((d) => {
        if (!cancelled) { setData(d); setLoading(false) }
      })
      .catch((e: unknown) => {
        if (cancelled) return
        if (e instanceof ApiError && e.code === ERR_503) {
          setClusterDisabled(true)
        } else {
          setError(e instanceof ApiError ? e.message : 'Failed to load cluster overview')
        }
        setLoading(false)
      })
    return () => { cancelled = true }
  }, [])

  if (loading) return <div className="page-center"><LoadingSpinner size={24} /></div>

  if (clusterDisabled) {
    return (
      <EmptyState
        icon={<svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><circle cx="12" cy="5" r="2" /><circle cx="19" cy="19" r="2" /><circle cx="5" cy="19" r="2" /><path d="M12 7v4M12 11l7 6M12 11l-7 6" strokeLinecap="round" strokeLinejoin="round" /></svg>}
        title="Cluster mode not enabled"
        description="Start the server with -cluster-enabled to activate cluster features."
      />
    )
  }

  if (error || !data) {
    return <EmptyState title="Error" description={error ?? 'No data available'} />
  }

  const nodeCols: Column<ClusterOverviewNode>[] = [
    { key: 'id', header: 'Node ID', mono: true, render: (r) => r.id.slice(0, 16) },
    { key: 'addr', header: 'Address', mono: true },
    { key: 'role', header: 'Role', render: (r) => (
      <Badge variant={r.role === 'master' ? 'accent' : 'info'}>{r.role}</Badge>
    )},
    { key: 'state', header: 'State', render: (r) => (
      <Badge variant={r.state === 'connected' ? 'ok' : 'warn'}>{r.state}</Badge>
    )},
    { key: 'slots_count', header: 'Slots' },
    { key: 'healthy', header: 'Health', render: (r) => (
      <Badge variant={r.healthy ? 'ok' : 'danger'}>{r.healthy ? 'OK' : 'DOWN'}</Badge>
    )},
  ]

  const masters = data.nodes.filter((n) => n.role === 'master')

  return (
    <div className="cluster-overview-page">
      <div className="stat-row">
        <StatCard label="Cluster Status" value={data.cluster_state.toUpperCase()} style={{ animationDelay: '0ms' }} />
        <StatCard label="Nodes" value={`${data.master_count}M / ${data.replica_count}R`} style={{ animationDelay: '60ms' }} />
        <StatCard label="Slots Assigned" value={String(data.slots_assigned)} unit="/ 16384" style={{ animationDelay: '120ms' }} />
        <StatCard label="Migrating" value={String(data.slots_migrating)} trend={data.slots_migrating > 0 ? 'up' : 'flat'} style={{ animationDelay: '180ms' }} />
      </div>

      <div className="page-section" style={{ animationDelay: '240ms' }}>
        <div className="page-section__header">
          <span className="page-section__title">Node Health ({data.node_count})</span>
        </div>
        <DataTable<ClusterOverviewNode>
          columns={nodeCols}
          rows={data.nodes}
          rowKey={(r) => r.id}
          emptyMessage="No nodes in cluster"
        />
      </div>

      {masters.length > 0 && (
        <div className="page-section" style={{ animationDelay: '300ms' }}>
          <div className="page-section__header">
            <span className="page-section__title">Slot Distribution</span>
          </div>
          <SlotDistributionBars nodes={masters} total={16384} />
        </div>
      )}

      {data.replication && (
        <div className="page-section" style={{ animationDelay: '360ms' }}>
          <div className="page-section__header">
            <span className="page-section__title">Replication Health</span>
            <Badge variant={data.replication.lagging_replicas === 0 ? 'ok' : 'warn'}>
              {data.replication.lagging_replicas === 0 ? 'OK' : 'LAGGING'}
            </Badge>
          </div>
          <p className="replication-summary">
            {data.replication.healthy_replicas} healthy replica{data.replication.healthy_replicas !== 1 ? 's' : ''}
            {data.replication.lagging_replicas > 0 && (
              <>, {data.replication.lagging_replicas} lagging</>
            )}
          </p>
        </div>
      )}
    </div>
  )
}

function SlotDistributionBars({ nodes, total }: { nodes: ClusterOverviewNode[]; total: number }) {
  return (
    <div className="slot-bars">
      {nodes.map((n) => {
        const pct = total > 0 ? (n.slots_count / total) * 100 : 0
        return (
          <div key={n.id} className="slot-bar-row">
            <span className="slot-bar__label mono">{n.id.slice(0, 8)}</span>
            <div className="slot-bar__track">
              <div className="slot-bar__fill" style={{ width: `${pct}%` }} />
            </div>
            <span className="slot-bar__count">{n.slots_count}</span>
          </div>
        )
      })}
    </div>
  )
}
