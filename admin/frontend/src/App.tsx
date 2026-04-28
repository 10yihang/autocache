import { lazy, Suspense } from 'react'
import { Routes, Route } from 'react-router-dom'
import { Layout } from './components/Layout.tsx'
import { LoadingSpinner } from './components/LoadingSpinner.tsx'
import { ToastProvider } from './components/Toast.tsx'
import { useMetricsSSE } from './hooks/useSSE.ts'
import { OverviewPage } from './pages/OverviewPage.tsx'
import { KeysPage } from './pages/KeysPage.tsx'
import { ClusterPage } from './pages/ClusterPage.tsx'
import { ConsolePage } from './pages/ConsolePage.tsx'
import { SlotsPage } from './pages/SlotsPage.tsx'
import { OpsPage } from './pages/OpsPage.tsx'
import { NotFoundPage } from './pages/NotFoundPage.tsx'

const MetricsPage = lazy(() =>
  import('./pages/MetricsPage.tsx').then((m) => ({ default: m.MetricsPage })),
)

function AppShell() {
  const { status, history, latest, isPaused, setPaused } = useMetricsSSE(60)

  return (
    <Routes>
      <Route element={<Layout sseStatus={status} />}>
        <Route index element={<OverviewPage latest={latest} />} />
        <Route path="/keys" element={<KeysPage />} />
        <Route path="/cluster" element={<ClusterPage />} />
        <Route path="/metrics" element={<Suspense fallback={<LoadingSpinner />}><MetricsPage history={history} status={status} isPaused={isPaused} setPaused={setPaused} /></Suspense>} />
        <Route path="/console" element={<ConsolePage />} />
        <Route path="/slots" element={<SlotsPage />} />
        <Route path="/ops" element={<OpsPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}

export default function App() {
  return (
    <ToastProvider>
      <AppShell />
    </ToastProvider>
  )
}
