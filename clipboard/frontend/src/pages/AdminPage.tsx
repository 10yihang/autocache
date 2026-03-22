import { useState } from 'react'
import type { FormEvent } from 'react'
import { fetchAdminPastes, fetchAdminStats } from '../api/clipboard'

export function AdminPage() {
  const [token, setToken] = useState('')
  const [errorMessage, setErrorMessage] = useState('')
  const [stats, setStats] = useState<Awaited<ReturnType<typeof fetchAdminStats>> | null>(
    null,
  )
  const [items, setItems] = useState<Awaited<ReturnType<typeof fetchAdminPastes>>['items']>([])

  async function loadAdminData(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setErrorMessage('')
    try {
      const [statsResponse, listResponse] = await Promise.all([
        fetchAdminStats(token),
        fetchAdminPastes(token),
      ])
      setStats(statsResponse)
      setItems(listResponse.items)
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : '管理请求失败')
    }
  }

  return (
    <div className="page-grid">
      <section className="panel">
        <p className="kicker">私有管理</p>
        <h2>运行状态快照</h2>
        <form className="stack" onSubmit={loadAdminData}>
          <label>
            管理令牌
            <input
              value={token}
              onChange={(event) => setToken(event.target.value)}
              placeholder="管理令牌"
            />
          </label>
          <button type="submit">加载统计</button>
          {errorMessage ? <p className="error-text">{errorMessage}</p> : null}
        </form>
      </section>
      {stats ? (
        <section className="panel">
          <h3>使用计数</h3>
          <div className="stats-grid">
            <p>已创建: {stats.usage.pastes_created_total}</p>
            <p>已读取: {stats.usage.pastes_read_total}</p>
            <p>已过期: {stats.usage.pastes_expired_total}</p>
            <p>已焚毁: {stats.usage.pastes_burned_total}</p>
            <p>查看受限: {stats.usage.pastes_view_limited_total}</p>
          </div>
        </section>
      ) : null}
      <section className="panel">
        <h3>最近粘贴</h3>
        {items.length === 0 ? (
          <p className="muted">加载统计后可查看当前粘贴条目。</p>
        ) : (
          <ul className="history-list">
            {items.map((item) => (
              <li key={item.code}>{item.code}</li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
