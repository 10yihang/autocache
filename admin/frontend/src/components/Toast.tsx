import { useState, useCallback, useEffect, useRef } from 'react'
import './Toast.css'

interface ToastItem {
  id: number
  message: string
  variant: 'ok' | 'warn' | 'danger' | 'info'
}

interface ToastContextValue {
  addToast: (message: string, variant?: ToastItem['variant']) => void
}

import { createContext, useContext } from 'react'

const ToastContext = createContext<ToastContextValue | null>(null)

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used within ToastProvider')
  return ctx
}

let nextId = 0

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<ToastItem[]>([])
  const timers = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map())

  const addToast = useCallback((message: string, variant: ToastItem['variant'] = 'info') => {
    const id = nextId++
    setToasts((prev) => [...prev, { id, message, variant }])
    const timer = setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id))
      timers.current.delete(id)
    }, 4000)
    timers.current.set(id, timer)
  }, [])

  useEffect(() => {
    const t = timers.current
    return () => {
      t.forEach((timer) => clearTimeout(timer))
    }
  }, [])

  return (
    <ToastContext.Provider value={{ addToast }}>
      {children}
      <div className="toast-container">
        {toasts.map((t) => (
          <div key={t.id} className={`toast toast--${t.variant}`}>
            {t.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}
