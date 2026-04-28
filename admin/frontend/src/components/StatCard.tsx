import './StatCard.css'

interface StatCardProps {
  label: string
  value: string | number
  unit?: string
  trend?: 'up' | 'down' | 'flat'
  style?: React.CSSProperties
}

export function StatCard({ label, value, unit, trend, style }: StatCardProps) {
  return (
    <div className="stat-card" style={style}>
      <span className="stat-card__label">{label}</span>
      <div className="stat-card__value-row">
        <span className="stat-card__value">{value}</span>
        {unit && <span className="stat-card__unit">{unit}</span>}
        {trend && (
          <span className={`stat-card__trend stat-card__trend--${trend}`}>
            {trend === 'up' ? '↑' : trend === 'down' ? '↓' : '→'}
          </span>
        )}
      </div>
    </div>
  )
}
