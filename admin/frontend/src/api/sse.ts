import type { MetricsSnapshot } from './client.ts'

type SSEStatus = 'connecting' | 'connected' | 'reconnecting' | 'error'

const BACKOFF_STEPS = [1000, 2000, 4000, 8000, 16000, 30000]

export function subscribeMetrics(
  onData: (snap: MetricsSnapshot) => void,
  onStatus: (status: SSEStatus) => void,
): () => void {
  let es: EventSource | null = null
  let attempt = 0
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let cancelled = false

  function connect() {
    if (cancelled) return
    onStatus(attempt === 0 ? 'connecting' : 'reconnecting')

    es = new EventSource('/api/v1/metrics/stream')

    es.onopen = () => {
      attempt = 0
      onStatus('connected')
    }

    es.onmessage = (evt) => {
      try {
        const snap = JSON.parse(evt.data as string) as MetricsSnapshot
        onData(snap)
      } catch {
        // malformed SSE data; skip
      }
    }

    es.onerror = () => {
      es?.close()
      es = null
      if (cancelled) return
      onStatus('reconnecting')
      const delay = BACKOFF_STEPS[Math.min(attempt, BACKOFF_STEPS.length - 1)] ?? 30000
      attempt++
      retryTimer = setTimeout(connect, delay)
    }
  }

  connect()

  return () => {
    cancelled = true
    if (retryTimer !== null) clearTimeout(retryTimer)
    es?.close()
    es = null
  }
}
