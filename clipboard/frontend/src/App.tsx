import { NavLink, Route, Routes } from 'react-router-dom'
import { AdminPage } from './pages/AdminPage'
import { HomePage } from './pages/HomePage'
import { PastePage } from './pages/PastePage'

function App() {
  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand-block">
          <p className="kicker">AutoCache 演示</p>
          <h1>剪贴板</h1>
        </div>
        <nav className="topnav">
          <NavLink to="/">首页</NavLink>
          <NavLink to="/admin">管理台</NavLink>
        </nav>
      </header>
      <main className="page-root">
        <Routes>
          <Route path="/" element={<HomePage />} />
          <Route path="/p/:code" element={<PastePage />} />
          <Route path="/admin" element={<AdminPage />} />
        </Routes>
      </main>
    </div>
  )
}

export default App
