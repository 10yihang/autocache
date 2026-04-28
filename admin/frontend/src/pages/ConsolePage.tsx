import { useState, useRef, useEffect } from 'react'
import './ConsolePage.css'

const BANNER = [
  '──────────────────────────────────────────────',
  '  AutoCache Admin Console',
  '  Command dispatch via Wave 4 in-process RESP',
  '──────────────────────────────────────────────',
  '',
  '  Console requires in-process RESP dispatch',
  '  (Wave 4 work). For now, use redis-cli:',
  '',
  '    redis-cli -p 6379 PING',
  '    redis-cli -p 6379 INFO server',
  '',
  '  The UI is fully built; wiring lands in Wave 4.',
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
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
  }, [lines])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!input.trim()) return
    const cmd = input.trim()
    setLines((prev) => [
      ...prev,
      { text: `> ${cmd}`, type: 'input' },
      { text: '(Not implemented) Wave 4 will wire command dispatch via protocol.Handler.', type: 'error' },
    ])
    setHistory((prev) => [cmd, ...prev].slice(0, 100))
    setHistIdx(-1)
    setInput('')
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
          placeholder="PING"
          autoFocus
          spellCheck={false}
          autoComplete="off"
        />
        <button type="submit" className="btn btn--primary">Send</button>
      </form>
    </div>
  )
}
