import { useNavigate } from 'react-router-dom'
import './NotFoundPage.css'

export function NotFoundPage() {
  const navigate = useNavigate()

  return (
    <div className="not-found-page">
      <div className="not-found-page__inner">
        <div className="not-found-page__code">404</div>
        <div className="not-found-page__divider" />
        <div className="not-found-page__content">
          <h1 className="not-found-page__title">Page not found</h1>
          <p className="not-found-page__desc">
            The route you requested does not exist in this admin UI.
          </p>
          <div className="not-found-page__actions">
            <button className="btn btn--accent" onClick={() => navigate('/')}>
              Go to Overview
            </button>
            <button className="btn btn--mute" onClick={() => navigate(-1)}>
              Go back
            </button>
          </div>
        </div>
      </div>

      <div className="not-found-page__slot-grid" aria-hidden="true">
        {Array.from({ length: 128 }, (_, i) => (
          <div
            key={i}
            className="not-found-page__slot-cell"
            style={{ opacity: 0.03 + ((i * 17 + 7) % 97) / 97 * 0.15 }}
          />
        ))}
      </div>
    </div>
  )
}
