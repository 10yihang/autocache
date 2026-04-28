import { useState } from 'react'
import './JsonViewer.css'

interface JsonViewerProps {
  data: unknown
  depth?: number
}

function JsonNode({ data, depth }: { data: unknown; depth: number }) {
  const [collapsed, setCollapsed] = useState(depth > 1)

  if (data === null) return <span className="json-null">null</span>
  if (typeof data === 'boolean') return <span className="json-bool">{String(data)}</span>
  if (typeof data === 'number') return <span className="json-num">{String(data)}</span>
  if (typeof data === 'string') return <span className="json-str">"{data}"</span>

  if (Array.isArray(data)) {
    if (data.length === 0) return <span className="json-mute">[]</span>
    return (
      <span className="json-block">
        <button className="json-toggle" onClick={() => setCollapsed(!collapsed)}>
          {collapsed ? '▶' : '▼'} [{data.length}]
        </button>
        {!collapsed && (
          <span className="json-children">
            {data.map((item, i) => (
              <span key={i} className="json-row">
                <span className="json-key json-idx">{i}</span>
                <span className="json-colon">:</span>
                <JsonNode data={item} depth={depth + 1} />
              </span>
            ))}
          </span>
        )}
      </span>
    )
  }

  if (typeof data === 'object') {
    const entries = Object.entries(data as Record<string, unknown>)
    if (entries.length === 0) return <span className="json-mute">{'{}'}</span>
    return (
      <span className="json-block">
        <button className="json-toggle" onClick={() => setCollapsed(!collapsed)}>
          {collapsed ? '▶' : '▼'} {'{'}{entries.length}{'}'}
        </button>
        {!collapsed && (
          <span className="json-children">
            {entries.map(([k, v]) => (
              <span key={k} className="json-row">
                <span className="json-key">"{k}"</span>
                <span className="json-colon">:</span>
                <JsonNode data={v} depth={depth + 1} />
              </span>
            ))}
          </span>
        )}
      </span>
    )
  }

  return <span className="json-mute">{String(data)}</span>
}

export function JsonViewer({ data, depth = 0 }: JsonViewerProps) {
  return (
    <div className="json-viewer">
      <JsonNode data={data} depth={depth} />
    </div>
  )
}
