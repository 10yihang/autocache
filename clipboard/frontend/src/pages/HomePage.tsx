import { useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import { createPaste } from '../api/clipboard'
import type { CreatePasteResponse } from '../api/clipboard'

type HistoryItem = {
  code: string
  shareURL: string
  createdAt: string
}

const storageKey = 'clipboard_recent_items'

function loadRecentItems(): HistoryItem[] {
  const raw = localStorage.getItem(storageKey)
  if (!raw) {
    return []
  }
  try {
    const parsed = JSON.parse(raw) as HistoryItem[]
    return parsed.slice(0, 6)
  } catch {
    return []
  }
}

function saveRecentItems(items: HistoryItem[]) {
  localStorage.setItem(storageKey, JSON.stringify(items.slice(0, 6)))
}

export function HomePage() {
  const [content, setContent] = useState('')
  const [ttl, setTTL] = useState('1h')
  const [maxViews, setMaxViews] = useState('')
  const [burnAfterRead, setBurnAfterRead] = useState(false)
  const [created, setCreated] = useState<CreatePasteResponse | null>(null)
  const [errorMessage, setErrorMessage] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [recentItems, setRecentItems] = useState<HistoryItem[]>(loadRecentItems)

  const shareLink = useMemo(() => {
    if (!created) {
      return ''
    }
    return created.share_url
  }, [created])

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setErrorMessage('')
    setIsSubmitting(true)
    try {
      const payload: {
        content: string
        ttl: string
        max_views?: number
        burn_after_read?: boolean
      } = {
        content,
        ttl,
      }
      if (maxViews.trim() !== '') {
        payload.max_views = Number(maxViews)
      }
      if (burnAfterRead) {
        payload.burn_after_read = true
      }

      const response = await createPaste(payload)
      setCreated(response)
      const nextItems = [
        {
          code: response.code,
          shareURL: response.share_url,
          createdAt: new Date().toISOString(),
        },
        ...recentItems,
      ].slice(0, 6)
      setRecentItems(nextItems)
      saveRecentItems(nextItems)
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : '创建失败')
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className="page-grid">
      <section className="panel intro-panel">
        <p className="kicker">匿名分享</p>
        <h2>创建一个短时有效的粘贴链接</h2>
        <p>
          这个演示会通过 AutoCache 写入内容，并返回一个可立即分享的链接。
        </p>
      </section>
      <section className="panel">
        <form className="stack" onSubmit={handleCreate}>
          <label>
            内容
            <textarea
              value={content}
              onChange={(event) => setContent(event.target.value)}
              rows={8}
              placeholder="粘贴你的文本"
            />
          </label>
          <div className="row">
            <label>
              生存时间（TTL）
              <select value={ttl} onChange={(event) => setTTL(event.target.value)}>
                <option value="5m">5 分钟</option>
                <option value="30m">30 分钟</option>
                <option value="1h">1 小时</option>
                <option value="6h">6 小时</option>
                <option value="1d">1 天</option>
                <option value="7d">7 天</option>
              </select>
            </label>
            <label>
              最大查看次数
              <input
                value={maxViews}
                onChange={(event) => setMaxViews(event.target.value)}
                type="number"
                min={0}
                step={1}
                placeholder="0 表示不限制"
              />
            </label>
          </div>
          <label className="checkbox-row">
            <input
              type="checkbox"
              checked={burnAfterRead}
              onChange={(event) => setBurnAfterRead(event.target.checked)}
            />
            首次读取后即焚毁
          </label>
          <button disabled={isSubmitting} type="submit">
            {isSubmitting ? '创建中...' : '创建粘贴'}
          </button>
          {errorMessage ? <p className="error-text">{errorMessage}</p> : null}
        </form>
      </section>
      <section className="panel">
        <h3>分享链接</h3>
        {created ? (
          <a href={shareLink}>{shareLink}</a>
        ) : (
          <p className="muted">等待你的第一条粘贴内容。</p>
        )}
      </section>
      <section className="panel">
        <h3>本地最近记录</h3>
        {recentItems.length === 0 ? (
          <p className="muted">此浏览器创建的链接会显示在这里。</p>
        ) : (
          <ul className="history-list">
            {recentItems.map((item) => (
              <li key={item.code}>
                <a href={item.shareURL}>{item.code}</a>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
