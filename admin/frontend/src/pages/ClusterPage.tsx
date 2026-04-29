import { useEffect, useState } from 'react'
import { apiFetch, ApiError } from '../api/client.ts'
import type { ClusterInfoResponse, ClusterNodesResponse, ClusterSlotsResponse } from '../api/client.ts'
import { LoadingSpinner } from '../components/LoadingSpinner.tsx'
import { EmptyState } from '../components/EmptyState.tsx'
import { JsonViewer } from '../components/JsonViewer.tsx'
import { DataTable } from '../components/DataTable.tsx'
import type { Column } from '../components/DataTable.tsx'
import { Badge } from '../components/Badge.tsx'
import { SlotHeatmap } from '../components/SlotHeatmap.tsx'
import { ClusterOverviewPage } from './ClusterOverviewPage.tsx'
import './ClusterPage.css'

export function ClusterPage() {
  const [tab, setTab] = useState<'overview' | 'details'>('overview')

  return (
    <div className="cluster-page">
      <div className="page-tabs">
        <button
          className={`page-tab${tab === 'overview' ? ' page-tab--active' : ''}`}
          onClick={() => setTab('overview')}
        >Overview</button>
        <button
          className={`page-tab${tab === 'details' ? ' page-tab--active' : ''}`}
          onClick={() => setTab('details')}
        >Details</button>
      </div>
      {tab === 'overview' ? <ClusterOverviewPage /> : <ClusterDetails />}
    </div>
  )
}

const ERR_503 = 'ERR_CLUSTER_DISABLED'

function is503(e: unknown): boolean {
  return e instanceof ApiError && e.code === ERR_503
}

function ClusterDetails() {
  const [info, setInfo] = useState<ClusterInfoResponse | null>(null)
  const [nodes, setNodes] = useState<ClusterNodesResponse | null>(null)
  const [slots, setSlots] = useState<ClusterSlotsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [clusterDisabled, setClusterDisabled] = useState(false)

  useEffect(() => {
    let cancelled = false
    // eslint-disable-next-line react-hooks/set-state-in-effect -- loading flag must be set synchronously before async fetch
    setLoading(true)
    Promise.allSettled([
      apiFetch<ClusterInfoResponse>('/api/v1/cluster/info'),
      apiFetch<ClusterNodesResponse>('/api/v1/cluster/nodes'),
      apiFetch<ClusterSlotsResponse>('/api/v1/cluster/slots'),
    ]).then(([infoR, nodesR, slotsR]) => {
      if (cancelled) return
      if (infoR.status === 'rejected' && is503(infoR.reason)) {
        setClusterDisabled(true)
        setLoading(false)
        return
      }
      if (infoR.status === 'fulfilled') setInfo(infoR.value)
      if (nodesR.status === 'fulfilled') setNodes(nodesR.value)
      if (slotsR.status === 'fulfilled') setSlots(slotsR.value)
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

  type NodeRow = { id: string; addr: string; cluster_port: number; role: string; state: string; master_id?: string }
  const nodeCols: Column<NodeRow>[] = [
    { key: 'id', header: 'Node ID', mono: true, render: (r) => r.id.slice(0, 16) },
    { key: 'addr', header: 'Address', mono: true },
    { key: 'role', header: 'Role', render: (r) => (
      <Badge variant={r.role === 'primary' ? 'ok' : 'info'}>{r.role}</Badge>
    )},
    { key: 'state', header: 'State', render: (r) => (
      <Badge variant={r.state === 'connected' ? 'ok' : 'warn'}>{r.state}</Badge>
    )},
    { key: 'master_id', header: 'Master ID', mono: true, render: (r) => r.master_id ? r.master_id.slice(0, 12) : '—' },
  ]

  return (
    <>
      <div className="page-section">
        <div className="page-section__header">
          <span className="page-section__title">Cluster Info</span>
        </div>
        {info ? <JsonViewer data={info.info} /> : <EmptyState title="No info available" />}
      </div>

      <div className="page-section">
        <div className="page-section__header">
          <span className="page-section__title">Nodes ({nodes?.nodes.length ?? 0})</span>
        </div>
        <DataTable
          columns={nodeCols}
          rows={(nodes?.nodes ?? []) as NodeRow[]}
          rowKey={(r) => r.id}
          emptyMessage="No nodes"
        />
      </div>

      <div className="page-section">
        <div className="page-section__header">
          <span className="page-section__title">Slot Distribution — 16384 slots</span>
          {slots && Object.keys(slots.migrating ?? {}).length > 0 && (
            <Badge variant="warn">{Object.keys(slots.migrating ?? {}).length} migrating</Badge>
          )}
        </div>
        {slots ? (
          <SlotHeatmap slotMap={Array.from(slots.slot_map)} migrating={slots.migrating} />
        ) : (
          <EmptyState title="Slot data unavailable" />
        )}
      </div>
    </>
  )
}
