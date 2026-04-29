import { useState, useRef, useEffect } from 'react'
import { executeCommand } from '../api/client'
import './ConsolePage.css'

const BANNER = [
  '──────────────────────────────────────────────',
  '  AutoCache Admin Console',
  '  Redis RESP protocol over HTTP bridge',
  '──────────────────────────────────────────────',
  '',
  '  Type any Redis command below.',
  '  Examples: PING, SET key val, GET key',
  '──────────────────────────────────────────────',
]

interface Line {
  text: string
  type: 'banner' | 'input' | 'output' | 'error'
}

export function ConsolePage() {
  const [input, setInput] = useState('')
  const [lines, setLines] = useState<Line[]>(
    BANNER.map((text) => ({ text, type: 'banner' as const }))
  )
  const [history, setHistory] = useState<string[]>([])
  const [histIdx, setHistIdx] = useState(-1)
  const [running, setRunning] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
  }, [lines])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!input.trim() || running) return
    const cmd = input.trim()
    setLines((prev) => [...prev, { text: `> ${cmd}`, type: 'input' }])
    setHistory((prev) => [cmd, ...prev].slice(0, 100))
    setHistIdx(-1)
    setInput('')
    setRunning(true)

    try {
      const res = await executeCommand(cmd)
      setLines((prev) => [...prev, { text: res.result || '(nil)', type: 'output' }])
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err)
      setLines((prev) => [...prev, { text: `(error) ${msg}`, type: 'error' }])
    } finally {
      setRunning(false)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      const next = Math.min(histIdx + 1, history.length - 1)
      setHistIdx(next)
      if (history[next]) setInput(history[next])
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      const next = Math.max(histIdx - 1, -1)
      setHistIdx(next)
      setInput(next === -1 ? '' : (history[next] ?? ''))
    }
  }

  return (
    <div className="console-page">
      <div className="console-output" ref={scrollRef}>
        {lines.map((line, i) => (
          <div key={i} className={`console-line console-line--${line.type}`}>
            {line.text}
          </div>
        ))}
      </div>
      <form className="console-input-row" onSubmit={handleSubmit}>
        <span className="console-prompt">redis&gt;</span>
        <input
          className="console-input"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={running ? 'Running...' : 'PING'}
          autoFocus
          spellCheck={false}
          autoComplete="off"
          disabled={running}
        />
        <button type="submit" className="btn btn--primary" disabled={running}>
          Send
        </button>
      </form>
    </div>
  )
}
