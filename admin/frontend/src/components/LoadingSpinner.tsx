import './LoadingSpinner.css'

interface LoadingSpinnerProps {
  size?: number
}

export function LoadingSpinner({ size = 20 }: LoadingSpinnerProps) {
  return (
    <svg
      className="loading-spinner"
      width={size}
      height={size}
      viewBox="0 0 20 20"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <circle cx="10" cy="10" r="8" stroke="var(--border-strong)" strokeWidth="1.5" />
      <path
        d="M 10 2 A 8 8 0 0 1 18 10"
        stroke="var(--accent)"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
    </svg>
  )
}
