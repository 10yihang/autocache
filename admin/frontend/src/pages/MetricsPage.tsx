import {
  LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip,
  ResponsiveContainer, Legend
} from 'recharts'
import type { MetricsSnapshot } from '../api/client.ts'
import { EmptyState } from '../components/EmptyState.tsx'
import { Badge } from '../components/Badge.tsx'
import { formatBytes } from '../api/format.ts'
import './MetricsPage.css'

type SSEStatus = 'connecting' | 'connected' | 'reconnecting' | 'error'

interface MetricsPageProps {
  history: MetricsSnapshot[]
  status: SSEStatus
  isPaused: boolean
  setPaused: (p: boolean) => void
}

interface ChartPoint {
  t: string
  goroutines: number
  alloc_mb: number
  hits: number
  misses: number
  get_ops: number
  set_ops: number
}

function toChartPoints(history: MetricsSnapshot[]): ChartPoint[] {
  return history.map((snap) => {
    const d = new Date(snap.timestamp * 1000)
    const t = `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}:${d.getSeconds().toString().padStart(2, '0')}`
    return {
      t,
      goroutines: snap.goroutines,
      alloc_mb: Math.round(snap.alloc_mb * 10) / 10,
      hits: snap.hits,
      misses: snap.misses,
      get_ops: snap.get_ops,
      set_ops: snap.set_ops,
    }
  })
}

const CHART_STYLE = {
  cartesianGrid: { stroke: 'var(--border)', strokeDasharray: '3 3' },
  xAxis: { stroke: 'var(--fg-dim)', tick: { fill: 'var(--fg-dim)', fontSize: 10, fontFamily: 'var(--mono)' } },
  yAxis: { stroke: 'var(--fg-dim)', tick: { fill: 'var(--fg-dim)', fontSize: 10, fontFamily: 'var(--mono)' }, width: 52 },
  tooltip: { contentStyle: { background: 'var(--surface-3)', border: '1px solid var(--border-strong)', fontFamily: 'var(--mono)', fontSize: 11 }, labelStyle: { color: 'var(--fg-mute)' } },
  legend: { wrapperStyle: { fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--fg-dim)' } },
}

export function MetricsPage({ history, status, isPaused, setPaused }: MetricsPageProps) {
  const points = toChartPoints(history)
  const latest = history[history.length - 1]

  const handlePause = () => setPaused(!isPaused)

  if (history.length === 0) {
    return (
      <div className="metrics-page">
        <div className="metrics-page__header">
          <span className="metrics-page__title">Metrics Stream</span>
          <Badge variant={status === 'connected' ? 'ok' : 'warn'}>{status}</Badge>
        </div>
        <EmptyState title="Waiting for data…" description="SSE stream is initializing." />
      </div>
    )
  }

  return (
    <div className="metrics-page">
      <div className="metrics-page__header">
        <span className="metrics-page__title">Live Metrics — 60s window</span>
        <div className="metrics-page__controls">
          <Badge variant={status === 'connected' ? 'ok' : 'warn'}>{status}</Badge>
          <button className="btn btn--mute" onClick={handlePause}>{isPaused ? 'Resume' : 'Pause'}</button>
        </div>
      </div>

      {latest && (
        <div className="metrics-page__snapshot">
          <span className="metrics-snapshot__item">
            <span className="metrics-snapshot__label">Memory</span>
            <span className="metrics-snapshot__val">{formatBytes(latest.alloc_mb * 1024 * 1024)}</span>
          </span>
          <span className="metrics-snapshot__item">
            <span className="metrics-snapshot__label">Goroutines</span>
            <span className="metrics-snapshot__val">{latest.goroutines}</span>
          </span>
          <span className="metrics-snapshot__item">
            <span className="metrics-snapshot__label">GC Cycles</span>
            <span className="metrics-snapshot__val">{latest.num_gc}</span>
          </span>
          <span className="metrics-snapshot__item">
            <span className="metrics-snapshot__label">Keys</span>
            <span className="metrics-snapshot__val">{latest.key_count}</span>
          </span>
        </div>
      )}

      <div className="metrics-charts">
        <div className="metrics-chart">
          <p className="metrics-chart__label">Goroutines</p>
          <ResponsiveContainer width="100%" height={140}>
            <LineChart data={points} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
              <CartesianGrid {...CHART_STYLE.cartesianGrid} />
              <XAxis dataKey="t" {...CHART_STYLE.xAxis} interval="preserveStartEnd" />
              <YAxis {...CHART_STYLE.yAxis} />
              <Tooltip {...CHART_STYLE.tooltip} />
              <Line type="monotone" dataKey="goroutines" stroke="var(--accent)" strokeWidth={1.5} dot={false} />
            </LineChart>
          </ResponsiveContainer>
        </div>

        <div className="metrics-chart">
          <p className="metrics-chart__label">Memory Alloc (MB)</p>
          <ResponsiveContainer width="100%" height={140}>
            <LineChart data={points} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
              <CartesianGrid {...CHART_STYLE.cartesianGrid} />
              <XAxis dataKey="t" {...CHART_STYLE.xAxis} interval="preserveStartEnd" />
              <YAxis {...CHART_STYLE.yAxis} />
              <Tooltip {...CHART_STYLE.tooltip} />
              <Line type="monotone" dataKey="alloc_mb" stroke="var(--warn)" strokeWidth={1.5} dot={false} />
            </LineChart>
          </ResponsiveContainer>
        </div>

        <div className="metrics-chart">
          <p className="metrics-chart__label">Hits vs Misses</p>
          <ResponsiveContainer width="100%" height={140}>
            <LineChart data={points} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
              <CartesianGrid {...CHART_STYLE.cartesianGrid} />
              <XAxis dataKey="t" {...CHART_STYLE.xAxis} interval="preserveStartEnd" />
              <YAxis {...CHART_STYLE.yAxis} />
              <Tooltip {...CHART_STYLE.tooltip} />
              <Legend {...CHART_STYLE.legend} />
              <Line type="monotone" dataKey="hits" stroke="var(--ok)" strokeWidth={1.5} dot={false} />
              <Line type="monotone" dataKey="misses" stroke="var(--danger)" strokeWidth={1.5} dot={false} />
            </LineChart>
          </ResponsiveContainer>
        </div>

        <div className="metrics-chart">
          <p className="metrics-chart__label">Get vs Set Ops</p>
          <ResponsiveContainer width="100%" height={140}>
            <LineChart data={points} margin={{ top: 4, right: 8, left: 0, bottom: 0 }}>
              <CartesianGrid {...CHART_STYLE.cartesianGrid} />
              <XAxis dataKey="t" {...CHART_STYLE.xAxis} interval="preserveStartEnd" />
              <YAxis {...CHART_STYLE.yAxis} />
              <Tooltip {...CHART_STYLE.tooltip} />
              <Legend {...CHART_STYLE.legend} />
              <Line type="monotone" dataKey="get_ops" stroke="var(--info)" strokeWidth={1.5} dot={false} />
              <Line type="monotone" dataKey="set_ops" stroke="var(--accent)" strokeWidth={1.5} dot={false} />
            </LineChart>
          </ResponsiveContainer>
        </div>
      </div>
    </div>
  )
}
