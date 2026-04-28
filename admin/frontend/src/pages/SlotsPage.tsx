import { useState, useEffect, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { apiFetch, ApiError } from '../api/client.ts'
import type { SlotInfoResponse } from '../api/client.ts'
import { LoadingSpinner } from '../components/LoadingSpinner.tsx'
import { EmptyState } from '../components/EmptyState.tsx'
import { Badge } from '../components/Badge.tsx'
import { DataTable } from '../components/DataTable.tsx'
import type { Column } from '../components/DataTable.tsx'
import { useToast } from '../components/Toast.tsx'
import './SlotsPage.css'

function slotStateBadge(state: string) {
  if (state === 'normal') return <Badge variant="ok">normal</Badge>
  if (state === 'importing') return <Badge variant="warn">importing</Badge>
  if (state === 'exporting') return <Badge variant="info">exporting</Badge>
  return <Badge variant="mute">{state}</Badge>
}

export function SlotsPage() {
  const [params, setParams] = useSearchParams()
  const [inputSlot, setInputSlot] = useState(params.get('slot') ?? '')
  const [data, setData] = useState<SlotInfoResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const toast = useToast()

  const lookup = useCallback((slotStr: string) => {
    const n = parseInt(slotStr, 10)
    if (isNaN(n) || n < 0 || n > 16383) {
      setError('Slot must be 0–16383')
      return
    }
    setLoading(true)
    setError(null)
    setData(null)
    setParams({ slot: String(n) })
    apiFetch<SlotInfoResponse>(`/api/v1/slots/${n}`)
      .then((d) => { setData(d); setLoading(false) })
      .catch((e: unknown) => {
        setError(e instanceof ApiError ? e.message : 'Lookup failed')
        setLoading(false)
      })
  }, [setParams])

  useEffect(() => {
    const slot = params.get('slot')
    // eslint-disable-next-line react-hooks/set-state-in-effect -- lookup sets loading state synchronously before async fetch
    if (slot) lookup(slot)
  }, [params, lookup])

  type ReplicaRow = { node_id: string; match_lsn: string; healthy: boolean }
  const replicaCols: Column<ReplicaRow>[] = [
    { key: 'node_id', header: 'Node ID', mono: true, render: (r) => r.node_id.slice(0, 16) },
    { key: 'match_lsn', header: 'Match LSN', mono: true },
    { key: 'healthy', header: 'Health', render: (r) => <Badge variant={r.healthy ? 'ok' : 'danger'}>{r.healthy ? 'healthy' : 'degraded'}</Badge> },
  ]

  return (
    <div className="slots-page">
      <div className="slots-page__lookup">
        <input
          className="slots-page__input"
          type="number"
          min={0}
          max={16383}
          value={inputSlot}
          onChange={(e) => setInputSlot(e.target.value)}
          placeholder="0–16383"
          onKeyDown={(e) => { if (e.key === 'Enter') lookup(inputSlot) }}
        />
        <button className="btn btn--primary" onClick={() => lookup(inputSlot)}>Lookup</button>
      </div>

      {loading && <div className="page-center"><LoadingSpinner /></div>}

      {!loading && error && (
        <EmptyState title="Lookup failed" description={error} />
      )}

      {!loading && data && (
        <div className="slots-detail">
          <div className="slots-detail__header">
            <span className="slots-detail__slot-num">Slot {data.slot}</span>
            {slotStateBadge(data.state)}
          </div>

          <div className="info-card slots-detail__info">
            <div className="info-card__row">
              <span className="info-card__label">Node ID</span>
              <span className="info-card__value mono">{data.node_id || '—'}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">Primary ID</span>
              <span className="info-card__value mono">{data.primary_id || '—'}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">Config Epoch</span>
              <span className="info-card__value mono">{data.config_epoch}</span>
            </div>
            <div className="info-card__row">
              <span className="info-card__label">Next LSN</span>
              <span className="info-card__value mono">{data.next_lsn}</span>
            </div>
            {data.importing && (
              <div className="info-card__row">
                <span className="info-card__label">Importing from</span>
                <span className="info-card__value mono">{data.importing}</span>
              </div>
            )}
            {data.exporting && (
              <div className="info-card__row">
                <span className="info-card__label">Exporting to</span>
                <span className="info-card__value mono">{data.exporting}</span>
              </div>
            )}
          </div>

          {(data.replicas ?? []).length > 0 && (
            <div className="page-section">
              <div className="page-section__header">
                <span className="page-section__title">Replicas ({(data.replicas ?? []).length})</span>
              </div>
              <DataTable
                columns={replicaCols}
                rows={(data.replicas ?? []) as ReplicaRow[]}
                rowKey={(r) => r.node_id}
              />
            </div>
          )}

          <div className="slots-detail__migrate">
            <button
              className="btn btn--mute"
              onClick={() => toast.addToast('Slot migration: ERR_NOT_IMPLEMENTED (Wave 4)', 'warn')}
            >
              Migrate Slot…
            </button>
            <span className="slots-detail__migrate-note">Migration dispatch lands in Wave 4</span>
          </div>
        </div>
      )}
    </div>
  )
}
