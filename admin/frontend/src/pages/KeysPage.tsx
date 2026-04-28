import { useState, useEffect, useCallback } from 'react'
import { apiFetch, ApiError } from '../api/client.ts'
import type { KeyListResponse, KeyDetailResponse } from '../api/client.ts'
import { DataTable } from '../components/DataTable.tsx'
import type { Column } from '../components/DataTable.tsx'
import { LoadingSpinner } from '../components/LoadingSpinner.tsx'
import { EmptyState } from '../components/EmptyState.tsx'
import { Badge } from '../components/Badge.tsx'
import { ConfirmDialog } from '../components/ConfirmDialog.tsx'
import { useToast } from '../components/Toast.tsx'
import { formatBytes, formatDuration } from '../api/format.ts'
import './KeysPage.css'

export function KeysPage() {
  const [pattern, setPattern] = useState('*')
  const [count, setCount] = useState(50)
  const [cursor, setCursor] = useState('0')
  const [keys, setKeys] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [nextCursor, setNextCursor] = useState('0')
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const [detail, setDetail] = useState<KeyDetailResponse | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [editMode, setEditMode] = useState(false)
  const [editValue, setEditValue] = useState('')
  const [editTTL, setEditTTL] = useState(0)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const toast = useToast()

  const loadKeys = useCallback(() => {
    setLoading(true)
    setError(null)
    apiFetch<KeyListResponse>(`/api/v1/keys?cursor=${cursor}&match=${encodeURIComponent(pattern)}&count=${count}`)
      .then((d) => {
        setKeys(d.keys ?? [])
        setNextCursor(d.cursor)
        setLoading(false)
      })
      .catch((e: unknown) => {
        setError(e instanceof ApiError ? e.message : 'Failed to load keys')
        setLoading(false)
      })
  }, [cursor, pattern, count])

  // eslint-disable-next-line react-hooks/set-state-in-effect -- loadKeys sets loading state synchronously before async fetch
  useEffect(() => { loadKeys() }, [loadKeys])

  const loadDetail = (key: string) => {
    setSelectedKey(key)
    setDetail(null)
    setDetailLoading(true)
    setEditMode(false)
    apiFetch<KeyDetailResponse>(`/api/v1/keys/${encodeURIComponent(key)}`)
      .then((d) => { setDetail(d); setDetailLoading(false) })
      .catch(() => setDetailLoading(false))
  }

  const handleEdit = () => {
    if (!detail) return
    setEditValue(detail.value ?? '')
    setEditTTL(detail.ttl_seconds > 0 ? detail.ttl_seconds : 0)
    setEditMode(true)
  }

  const handleSave = () => {
    if (!selectedKey) return
    apiFetch(`/api/v1/keys/${encodeURIComponent(selectedKey)}`, {
      method: 'PUT',
      body: JSON.stringify({ value: editValue, ttl_seconds: editTTL }),
    })
      .then(() => {
        toast.addToast('Key updated', 'ok')
        setEditMode(false)
        loadKeys()
        if (selectedKey) loadDetail(selectedKey)
      })
      .catch((e: unknown) => {
        toast.addToast(e instanceof ApiError ? e.message : 'Update failed', 'danger')
      })
  }

  const handleDelete = (key: string) => {
    apiFetch(`/api/v1/keys/${encodeURIComponent(key)}`, { method: 'DELETE' })
      .then(() => {
        toast.addToast(`Deleted: ${key}`, 'ok')
        setDeleteTarget(null)
        setSelectedKey(null)
        setDetail(null)
        loadKeys()
      })
      .catch((e: unknown) => {
        toast.addToast(e instanceof ApiError ? e.message : 'Delete failed', 'danger')
        setDeleteTarget(null)
      })
  }

  const cols: Column<{ key: string }>[] = [
    {
      key: 'key',
      header: 'Key',
      mono: true,
      render: (row) => (
        <button
          className={`keys-table__key-btn ${selectedKey === row.key ? 'keys-table__key-btn--active' : ''}`}
          onClick={() => loadDetail(row.key)}
        >
          {row.key}
        </button>
      ),
    },
  ]

  const rows = keys.map((k) => ({ key: k }))

  return (
    <div className="keys-page">
      <div className="keys-page__toolbar">
        <input
          className="keys-page__input"
          value={pattern}
          onChange={(e) => { setPattern(e.target.value); setCursor('0') }}
          placeholder="Pattern (e.g. user:*)"
          onKeyDown={(e) => { if (e.key === 'Enter') { setCursor('0'); loadKeys() } }}
        />
        <label className="keys-page__count-label">
          <span className="keys-page__count-text">Count: {count}</span>
          <input
            type="range"
            min={10}
            max={1000}
            step={10}
            value={count}
            onChange={(e) => { setCount(Number(e.target.value)); setCursor('0') }}
          />
        </label>
        <button className="btn btn--primary" onClick={() => { setCursor('0'); loadKeys() }}>Scan</button>
      </div>

      <div className="keys-page__body">
        <div className="keys-page__list">
          {loading && (
            <div className="page-center"><LoadingSpinner /></div>
          )}
          {!loading && error && (
            <EmptyState title="Load failed" description={error} />
          )}
          {!loading && !error && (
            <>
              <DataTable
                columns={cols}
                rows={rows}
                rowKey={(r) => r.key}
                emptyMessage="No keys found"
              />
              <div className="keys-page__pagination">
                {cursor !== '0' && (
                  <button className="btn btn--mute" onClick={() => setCursor('0')}>← First</button>
                )}
                {nextCursor !== '0' && (
                  <button className="btn btn--mute" onClick={() => setCursor(nextCursor)}>
                    Next → cursor {nextCursor}
                  </button>
                )}
              </div>
            </>
          )}
        </div>

        {selectedKey && (
          <div className="keys-page__detail">
            <div className="keys-page__detail-header">
              <span className="keys-page__detail-key">{selectedKey}</span>
              <div className="keys-page__detail-actions">
                {!editMode && (
                  <button className="btn btn--mute" onClick={handleEdit}>Edit</button>
                )}
                <button
                  className="btn btn--danger"
                  onClick={() => setDeleteTarget(selectedKey)}
                >Delete</button>
              </div>
            </div>
            {detailLoading && <div className="page-center"><LoadingSpinner /></div>}
            {detail && !detailLoading && (
              <div className="keys-page__detail-body">
                <div className="keys-detail-meta">
                  <Badge variant="mute">{detail.type}</Badge>
                  <span className="keys-detail-meta__ttl">
                    TTL: {detail.ttl_seconds < 0 ? '∞' : formatDuration(detail.ttl_seconds)}
                  </span>
                  <span className="keys-detail-meta__size">{formatBytes(detail.size)}</span>
                  {detail.truncated && <Badge variant="warn">truncated</Badge>}
                </div>
                {editMode ? (
                  <div className="keys-page__edit">
                    <textarea
                      className="keys-page__edit-value"
                      value={editValue}
                      onChange={(e) => setEditValue(e.target.value)}
                      rows={10}
                    />
                    <div className="keys-page__edit-ttl">
                      <label>TTL (seconds, 0 = no expiry)</label>
                      <input
                        type="number"
                        min={0}
                        value={editTTL}
                        onChange={(e) => setEditTTL(Number(e.target.value))}
                      />
                    </div>
                    <div className="keys-page__edit-actions">
                      <button className="btn btn--mute" onClick={() => setEditMode(false)}>Cancel</button>
                      <button className="btn btn--primary" onClick={handleSave}>Save</button>
                    </div>
                  </div>
                ) : (
                  <pre className="keys-page__value">{detail.value ?? '(binary or empty)'}</pre>
                )}
              </div>
            )}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={deleteTarget !== null}
        title="Delete Key"
        message={`Delete "${deleteTarget}"? This cannot be undone.`}
        confirmLabel="Delete"
        danger
        onConfirm={() => deleteTarget && handleDelete(deleteTarget)}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  )
}
