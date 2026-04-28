import { useState, useRef, useCallback, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { hashColor } from '../api/format.ts'
import './SlotHeatmap.css'

const COLS = 128
const ROWS = 128
const TOTAL = COLS * ROWS
const CELL = 4

interface TooltipState {
  slot: number
  nodeId: string
  state: string
  x: number
  y: number
}

interface SlotHeatmapProps {
  slotMap: string[]
  migrating?: Record<string, { source_node_id: string; target_node_id: string; state: string }>
}

export function SlotHeatmap({ slotMap, migrating = {} }: SlotHeatmapProps) {
  const navigate = useNavigate()
  const [tooltip, setTooltip] = useState<TooltipState | null>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)

  const drawFrame = useCallback(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    const now = Date.now()

    for (let slot = 0; slot < TOTAL; slot++) {
      const nodeId = slotMap[slot] ?? ''
      const col = slot % COLS
      const row = Math.floor(slot / COLS)
      const x = col * CELL
      const y = row * CELL

      const isMigrating = migrating[String(slot)] !== undefined
      let alpha = 1
      if (isMigrating) {
        alpha = 0.5 + 0.5 * Math.abs(Math.sin((now / 600) + slot * 0.3))
      }

      const baseColor = nodeId ? hashColor(nodeId) : '#1c2532'
      ctx.globalAlpha = alpha
      ctx.fillStyle = baseColor
      ctx.fillRect(x, y, CELL - 1, CELL - 1)
    }
    ctx.globalAlpha = 1
  }, [slotMap, migrating])

  const hasMigrating = Object.keys(migrating).length > 0

  useEffect(() => {
    drawFrame()
    if (!hasMigrating) return

    let id: number
    const loop = () => { drawFrame(); id = requestAnimationFrame(loop) }
    id = requestAnimationFrame(loop)
    return () => cancelAnimationFrame(id)
  }, [drawFrame, hasMigrating])

  const canvasRefCallback = useCallback(
    (el: HTMLCanvasElement | null) => {
      (canvasRef as React.MutableRefObject<HTMLCanvasElement | null>).current = el
      if (el) {
        drawFrame()
      }
    },
    [drawFrame],
  )

  const handleMouseMove = useCallback(
    (e: React.MouseEvent<HTMLCanvasElement>) => {
      const canvas = canvasRef.current
      if (!canvas) return
      const rect = canvas.getBoundingClientRect()
      const scaleX = canvas.width / rect.width
      const scaleY = canvas.height / rect.height
      const cx = Math.floor(((e.clientX - rect.left) * scaleX) / CELL)
      const cy = Math.floor(((e.clientY - rect.top) * scaleY) / CELL)
      if (cx < 0 || cx >= COLS || cy < 0 || cy >= ROWS) return
      const slot = cy * COLS + cx
      const nodeId = slotMap[slot] ?? '—'
      const mig = migrating[String(slot)]
      const state = mig ? `migrating → ${mig.target_node_id.slice(0, 8)}` : 'normal'
      setTooltip({ slot, nodeId, state, x: e.clientX + 12, y: e.clientY - 8 })
    },
    [slotMap, migrating],
  )

  const handleMouseLeave = useCallback(() => setTooltip(null), [])

  const handleClick = useCallback(
    (e: React.MouseEvent<HTMLCanvasElement>) => {
      const canvas = canvasRef.current
      if (!canvas) return
      const rect = canvas.getBoundingClientRect()
      const scaleX = canvas.width / rect.width
      const scaleY = canvas.height / rect.height
      const cx = Math.floor(((e.clientX - rect.left) * scaleX) / CELL)
      const cy = Math.floor(((e.clientY - rect.top) * scaleY) / CELL)
      const slot = cy * COLS + cx
      if (slot >= 0 && slot < TOTAL) {
        navigate(`/slots?slot=${slot}`)
      }
    },
    [navigate],
  )

  const uniqueNodes = [...new Set(slotMap.filter(Boolean))]

  return (
    <div className="slot-heatmap">
      <div className="slot-heatmap__canvas-wrap">
        <canvas
          ref={canvasRefCallback}
          width={COLS * CELL}
          height={ROWS * CELL}
          className="slot-heatmap__canvas"
          onMouseMove={handleMouseMove}
          onMouseLeave={handleMouseLeave}
          onClick={handleClick}
          title="Click a slot to inspect details"
        />
      </div>
      {uniqueNodes.length > 0 && (
        <div className="slot-heatmap__legend">
          {uniqueNodes.map((nid) => (
            <span key={nid} className="slot-heatmap__legend-item">
              <span
                className="slot-heatmap__legend-swatch"
                style={{ background: hashColor(nid) }}
              />
              <span className="slot-heatmap__legend-label">{nid.slice(0, 8)}</span>
            </span>
          ))}
          {Object.keys(migrating).length > 0 && (
            <span className="slot-heatmap__legend-item slot-heatmap__legend-item--migrating">
              <span className="slot-heatmap__legend-pulse" />
              <span className="slot-heatmap__legend-label">migrating</span>
            </span>
          )}
        </div>
      )}
      {tooltip && (
        <div
          className="slot-heatmap__tooltip"
          style={{ left: tooltip.x, top: tooltip.y }}
        >
          <span className="slot-heatmap__tooltip-slot">slot {tooltip.slot}</span>
          <span className="slot-heatmap__tooltip-node">{tooltip.nodeId.slice(0, 16) || '—'}</span>
          <span className="slot-heatmap__tooltip-state">{tooltip.state}</span>
        </div>
      )}
    </div>
  )
}
