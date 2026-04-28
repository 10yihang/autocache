import { useEffect, useRef, useState } from 'react'
import { subscribeMetrics } from '../api/sse.ts'
import type { MetricsSnapshot } from '../api/client.ts'

type SSEStatus = 'connecting' | 'connected' | 'reconnecting' | 'error'

export function useMetricsSSE(maxPoints: number = 60) {
  const [history, setHistory] = useState<MetricsSnapshot[]>([])
  const [latest, setLatest] = useState<MetricsSnapshot | null>(null)
  const [status, setStatus] = useState<SSEStatus>('connecting')
  const [isPaused, setIsPaused] = useState(false)
  const pausedRef = useRef(false)

  useEffect(() => {
    const unsub = subscribeMetrics(
      (snap) => {
        if (pausedRef.current) return
        setLatest(snap)
        setHistory((prev) => {
          const next = [...prev, snap]
          return next.length > maxPoints ? next.slice(next.length - maxPoints) : next
        })
      },
      (s) => setStatus(s),
    )
    return unsub
  }, [maxPoints])

  const setPaused = (p: boolean) => {
    pausedRef.current = p
    setIsPaused(p)
  }

  return { history, latest, status, isPaused, setPaused }
}
