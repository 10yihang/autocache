import './Badge.css'

type BadgeVariant = 'ok' | 'warn' | 'danger' | 'info' | 'mute' | 'accent'

interface BadgeProps {
  variant?: BadgeVariant
  children: React.ReactNode
}

export function Badge({ variant = 'mute', children }: BadgeProps) {
  return (
    <span className={`badge badge--${variant}`}>
      {children}
    </span>
  )
}
