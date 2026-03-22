import { useEffect, useState } from 'react'
import type { KeyboardEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { createPaste, readPaste } from '../api/clipboard'

export function PastePage() {
  const { code = '' } = useParams()
  const [content, setContent] = useState('')
  const [status, setStatus] = useState<'loading' | 'ready' | 'missing'>('loading')
  const [isSaving, setIsSaving] = useState(false)
  const [saveMessage, setSaveMessage] = useState('')
  const [saveError, setSaveError] = useState('')
  const [sourceTTL, setSourceTTL] = useState('1h')

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
        setSourceTTL(response.paste.metadata.ttl || '1h')
        setSaveMessage('')
        setSaveError('')
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

  async function handleSave() {
    if (isSaving || content.trim() === '') {
      return
    }

    setIsSaving(true)
    setSaveMessage('')
    setSaveError('')
    try {
      const response = await createPaste({
        content,
        ttl: sourceTTL,
      })
      setSaveMessage(response.share_url)
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : '保存失败')
    } finally {
      setIsSaving(false)
    }
  }

  function handleEditorKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
      event.preventDefault()
      void handleSave()
    }
  }

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
      <p className="muted">你可以继续编辑这段内容，并保存为新的分享链接。</p>
      <label>
        编辑内容
        <textarea
          className="paste-editor"
          value={content}
          onChange={(event) => {
            setContent(event.target.value)
            setSaveMessage('')
            setSaveError('')
          }}
          onKeyDown={handleEditorKeyDown}
          rows={12}
        />
      </label>
      <div className="editor-actions">
        <button disabled={isSaving || content.trim() === ''} onClick={() => void handleSave()} type="button">
          {isSaving ? '保存中...' : '保存为新链接'}
        </button>
        <span className="muted">支持 Ctrl+S / Cmd+S 快捷保存</span>
      </div>
      {saveMessage ? (
        <p className="success-text">
          已保存为新的分享链接：<a href={saveMessage}>{saveMessage}</a>
        </p>
      ) : null}
      {saveError ? <p className="error-text">{saveError}</p> : null}
    </section>
  )
}
