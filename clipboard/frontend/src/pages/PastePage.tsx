import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { readPaste } from '../api/clipboard'

export function PastePage() {
  const { code = '' } = useParams()
  const [content, setContent] = useState('')
  const [status, setStatus] = useState<'loading' | 'ready' | 'missing'>('loading')

  useEffect(() => {
    let closed = false
    async function run() {
      setStatus('loading')
      try {
        const response = await readPaste(code)
        if (closed) {
          return
        }
        setContent(response.paste.content)
        setStatus('ready')
      } catch {
        if (closed) {
          return
        }
        setStatus('missing')
      }
    }
    void run()
    return () => {
      closed = true
    }
  }, [code])

  if (status === 'loading') {
    return <section className="panel">正在加载分享内容...</section>
  }

  if (status === 'missing') {
    return (
      <section className="panel">
        <h2>内容不可用</h2>
        <p>该链接不存在或已过期。</p>
        <Link to="/">创建新的粘贴</Link>
      </section>
    )
  }

  return (
    <section className="panel">
      <p className="kicker">分享内容</p>
      <pre className="paste-content">{content}</pre>
    </section>
  )
}
