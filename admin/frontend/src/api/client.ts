export class ApiError extends Error {
  code: string
  status: number

  constructor(message: string, code: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
  }
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
    ...init,
  })

  if (!response.ok) {
    let code = `ERR_HTTP_${response.status}`
    let message = `Request failed: ${response.status} ${response.statusText}`
    try {
      const payload = (await response.json()) as { error?: { code?: string; message?: string } }
      if (payload.error) {
        code = payload.error.code ?? code
        message = payload.error.message ?? message
      }
    } catch {
      // non-JSON body; keep defaults
    }
    throw new ApiError(message, code, response.status)
  }

  return (await response.json()) as T
}

export interface NodeSection {
  hostname: string
  pid: number
  uptime: string
  goroutines: number
}

export interface BuildSection {
  version: string
  go_version: string
}

export interface MemorySection {
  alloc_mb: number
  total_alloc_mb: number
  sys_mb: number
  num_gc: number
}

export interface StoreSection {
  key_count: number
  hits: number
  misses: number
  set_ops: number
  get_ops: number
  del_ops: number
  expired_keys: number
  evicted_keys: number
}

export interface ClusterBrief {
  node_id: string
  role: string
}

export interface OverviewResponse {
  node: NodeSection
  build: BuildSection
  memory: MemorySection
  store: StoreSection
  cluster?: ClusterBrief
}

export interface ClusterInfoResponse {
  info: Record<string, unknown>
}

export interface ClusterNodeResponse {
  id: string
  addr: string
  cluster_port: number
  role: string
  master_id?: string
  state: string
}

export interface ClusterNodesResponse {
  nodes: ClusterNodeResponse[]
}

export interface MigrationInfo {
  source_node_id: string
  target_node_id: string
  state: string
}

export interface ClusterSlotsResponse {
  slot_map: string[]
  migrating?: Record<string, MigrationInfo>
}

export interface KeyListResponse {
  keys: string[]
  cursor: string
  count: number
}

export interface KeyDetailResponse {
  key: string
  type: string
  ttl_seconds: number
  size: number
  value?: string
  truncated?: boolean
}

export interface SlotReplicaInfo {
  node_id: string
  match_lsn: string
  healthy: boolean
}

export interface SlotInfoResponse {
  slot: number
  state: string
  node_id: string
  primary_id: string
  config_epoch: string
  next_lsn: string
  replicas?: SlotReplicaInfo[]
  importing?: string
  exporting?: string
}

export interface SlotLogInfo {
  slot: number
  first_lsn: string
  last_lsn: string
}

export interface ReplicaAckInfo {
  slot: number
  epoch: string
  node_id: string
  applied_lsn: string
}

export interface ReplicationStatusResponse {
  available: boolean
  slots?: SlotLogInfo[]
  acks?: ReplicaAckInfo[]
}

export interface TieredStatsResponse {
  available: boolean
  hot_tier_keys?: number
  hot_tier_size?: number
  warm_tier_keys?: number
  warm_tier_size?: number
  cold_tier_keys?: number
  cold_tier_size?: number
  total_keys?: number
  total_size?: number
  migrations_up?: number
  migrations_down?: number
}

export interface AuditEntry {
  timestamp: string
  remote_addr: string
  user: string
  action: string
  target: string
  result: string
}

export interface AuditResponse {
  entries: AuditEntry[]
}

export interface MetricsSnapshot {
  timestamp: number
  goroutines: number
  alloc_mb: number
  sys_mb: number
  num_gc: number
  key_count: number
  hits: number
  misses: number
  set_ops: number
  get_ops: number
  del_ops: number
  expired_keys: number
  evicted_keys: number
}
