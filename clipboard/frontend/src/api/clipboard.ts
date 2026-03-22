import { requestJSON } from './client'

export type CreatePasteRequest = {
  content: string
  ttl: string
  max_views?: number
  burn_after_read?: boolean
}

export type PasteRecord = {
  code: string
  content: string
  metadata: {
    ttl: string
    created_at: string
    expires_at: string
    burn_after_read?: boolean
    remaining_views?: number
  }
}

export type CreatePasteResponse = {
  code: string
  share_url: string
  raw_url: string
  paste: PasteRecord
}

export type ReadPasteResponse = {
  paste: PasteRecord
}

export type AdminStatsResponse = {
  usage: {
    pastes_created_total: number
    pastes_read_total: number
    pastes_expired_total: number
    pastes_burned_total: number
    pastes_view_limited_total: number
  }
  rate_limits: {
    create_rate_limited_total: number
    read_rate_limited_total: number
  }
}

export type AdminPasteListResponse = {
  items: Array<{
    code: string
    created_at: string
    expires_at: string
    metadata: PasteRecord['metadata']
  }>
}

export function createPaste(payload: CreatePasteRequest) {
  return requestJSON<CreatePasteResponse>('/api/paste', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function readPaste(code: string) {
  return requestJSON<ReadPasteResponse>(`/api/paste/${code}`)
}

export function fetchAdminStats(token: string) {
  return requestJSON<AdminStatsResponse>('/admin/stats', {
    headers: {
      Authorization: `Bearer ${token}`,
    },
  })
}

export function fetchAdminPastes(token: string) {
  return requestJSON<AdminPasteListResponse>('/admin/pastes', {
    headers: {
      Authorization: `Bearer ${token}`,
    },
  })
}
