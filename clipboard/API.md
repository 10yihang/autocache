# Clipboard API Reference

This document describes the HTTP interface implemented by `clipboard/backend`.

Base assumptions:

- Public HTTP service is provided by the clipboard backend process.
- Routes are registered in `clipboard/backend/handler.go`.
- Persistence for paste content and metadata is backed by AutoCache/Redis.
- Admin endpoints require `Authorization: Bearer <admin-token>`.

## Endpoint Summary

| Method | Path | Purpose | Auth |
| --- | --- | --- | --- |
| `ANY` | `/health` | Liveness probe | No |
| `POST` | `/api/paste` | Create a paste | No |
| `GET` | `/api/paste/{code}` | Read a paste as JSON | No |
| `DELETE` | `/api/paste/{code}` | Delete a paste | No |
| `GET` | `/raw/{code}` | Read raw paste body as plain text | No |
| `GET` | `/admin/stats` | Read admin usage and rate-limit stats | Yes |
| `GET` | `/admin/pastes` | List current paste metadata | Yes |
| `GET` | `/` | Serve SPA shell | No |
| `GET` | `/admin` | Serve SPA shell | No |
| `GET` | `/p/{code}` | Serve SPA shell for paste page | No |
| `GET` | `/assets/*` | Serve embedded frontend assets | No |

## Common Behaviors

### Content type

- JSON endpoints produced via `writeJSON()` return `Content-Type: application/json`.
- Raw paste endpoint returns `Content-Type: text/plain; charset=utf-8`.
- SPA shell returns `Content-Type: text/html; charset=utf-8`.
- Middleware plain-text errors are emitted with a trailing newline.

### Error formats

There are two error styles in the current implementation:

1. JSON API errors from handler/service validation:

```json
{
  "error": {
    "code": "invalid_request",
    "message": "content is required"
  }
}
```

2. Plain-text errors emitted directly by middleware:

- `401 unauthorized`
- `415 content type must be application/json`
- `413 request body exceeds content limit`
- `429 create rate limit exceeded`
- `429 read rate limit exceeded`

### Allowed TTL presets

The create API only accepts the following TTL values:

- `5m`
- `30m`
- `1h`
- `6h`
- `1d`
- `7d`

### Access policy rules

- `burn_after_read=true` cannot be combined with `max_views > 0`.
- `max_views <= 0` is treated as unlimited.
- Max JSON request body size is `65536` bytes.
- `content` is also validated against the same `65536`-byte ceiling after decoding.
- JSON wrapper bytes count toward the request-body limit.

## 1. Health Check

### `ANY /health`

Returns a simple health payload.

Notes:

- The current handler does not restrict HTTP method, so non-GET requests also receive the same health response.

Response example:

```json
{
  "status": "ok"
}
```

## 2. Create Paste

### `POST /api/paste`

Creates a new paste entry.

Rate limit:

- Protected by the `create` limiter.
- Default server setting from `cmd/clipboard/main.go`: `30` requests per IP per `60` seconds.

Request headers:

- `Content-Type: application/json`

Request body:

```json
{
  "content": "hello world",
  "ttl": "1h",
  "max_views": 3,
  "burn_after_read": false
}
```

Request fields:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `content` | string | Yes | Must be non-empty after trim; max 64 KiB |
| `ttl` | string | Yes | Must be one of the allowed presets |
| `max_views` | integer | No | `<= 0` means unlimited |
| `burn_after_read` | boolean | No | Cannot be combined with `max_views > 0` |

Success response: `201 Created`

```json
{
  "code": "abc123",
  "share_url": "/p/abc123",
  "raw_url": "/raw/abc123",
  "paste": {
    "code": "abc123",
    "content": "hello world",
    "metadata": {
      "ttl": "1h",
      "created_at": "2026-03-22T12:00:00Z",
      "expires_at": "2026-03-22T13:00:00Z",
      "remaining_views": 3
    }
  },
  "created_at": "2026-03-22T12:00:00Z",
  "expires_at": "2026-03-22T13:00:00Z",
  "ttl": 3600000000000
}
```

Notes:

- `ttl` in the top-level response is a Go `time.Duration` serialized as nanoseconds.
- If the server is started with `-base-url`, `share_url` and `raw_url` become absolute URLs.

Typical failure responses:

| Status | Format | Meaning |
| --- | --- | --- |
| `400` | JSON | Invalid JSON, empty content, invalid TTL, oversized body, conflicting access policy |
| `413` | Plain text | Body exceeds request size limit when `Content-Length` is already above the limit |
| `415` | Plain text | Missing or invalid `Content-Type` |
| `429` | Plain text | Create rate limit exceeded |
| `500` | JSON | Internal storage or service error |

Additional note:

- If the request body is oversized but streamed without an over-limit `Content-Length`, JSON decoding can fail through `MaxBytesReader`, and the handler returns `400` JSON `invalid json body` instead of `413`.

## 3. Read Paste as JSON

### `GET /api/paste/{code}`

Reads a paste in JSON form.

Rate limit:

- Protected by the `read` limiter.
- Default server setting from `cmd/clipboard/main.go`: `120` requests per IP per `60` seconds.

Success response: `200 OK`

```json
{
  "paste": {
    "code": "abc123",
    "content": "hello world",
    "metadata": {
      "ttl": "1h",
      "created_at": "2026-03-22T12:00:00Z",
      "expires_at": "2026-03-22T13:00:00Z",
      "remaining_views": 3
    }
  }
}
```

Behavior notes:

- If `max_views` is enabled, the backend decrements the remaining view counter on read.
- If `burn_after_read=true`, the backend returns the content once and then deletes it.
- If a burn-after-read claim has already been consumed, later reads return not found.
- The current response body reports the pre-consumption `remaining_views` value for that read, not the decremented future value.

Typical failure responses:

| Status | Format | Meaning |
| --- | --- | --- |
| `400` | JSON | Missing paste code |
| `404` | JSON | Paste does not exist, expired, already burned, or exhausted view limit |
| `429` | Plain text | Read rate limit exceeded |
| `500` | JSON | Internal storage or service error |

## 4. Delete Paste

### `DELETE /api/paste/{code}`

Deletes a paste and any related counters/claims.

Success response: `200 OK`

```json
{
  "code": "abc123",
  "deleted": true
}
```

Notes:

- `deleted=false` means the key did not exist at deletion time.
- This endpoint is not admin-protected in the current implementation.
- This endpoint is mounted behind the `read` rate limiter, so delete requests share the same limiter bucket as read requests.

Typical failure responses:

| Status | Format | Meaning |
| --- | --- | --- |
| `400` | JSON | Missing paste code |
| `429` | Plain text | Read rate limit exceeded (same limiter as read path) |
| `500` | JSON | Internal storage error |

## 5. Read Raw Paste Body

### `GET /raw/{code}`

Reads only the paste content as plain text.

Success response: `200 OK`

```text
hello world
```

Behavior notes:

- Shares the same underlying read semantics as `GET /api/paste/{code}`.
- That means view limits and burn-after-read are consumed here as well.

Typical failure responses:

| Status | Format | Meaning |
| --- | --- | --- |
| `400` | JSON | Missing paste code |
| `404` | JSON | Paste not found / expired / burned / exhausted |
| `429` | Plain text | Read rate limit exceeded |
| `500` | JSON | Internal storage error |

## 6. Admin Statistics

### `GET /admin/stats`

Returns usage counters, tier placeholder stats, and rate-limit counters.

Authentication:

- Requires `Authorization: Bearer <admin-token>`.
- If the backend was started without `-admin-token`, all admin requests are rejected.
- Bearer token matching is exact and case-sensitive in the current middleware.

Success response: `200 OK`

```json
{
  "tier": {
    "hot_tier_keys": 0,
    "warm_tier_keys": 0,
    "cold_tier_keys": 0,
    "migrations_up": 0,
    "migrations_down": 0
  },
  "usage": {
    "pastes_created_total": 10,
    "pastes_read_total": 20,
    "pastes_expired_total": 1,
    "pastes_burned_total": 2,
    "pastes_view_limited_total": 3
  },
  "rate_limits": {
    "create_rate_limited_total": 0,
    "read_rate_limited_total": 0
  }
}
```

Typical failure responses:

| Status | Format | Meaning |
| --- | --- | --- |
| `401` | Plain text | Missing or invalid bearer token |
| `500` | JSON | Unexpected internal error |

## 7. Admin Paste List

### `GET /admin/pastes`

Lists active paste metadata for administration.

Authentication:

- Requires `Authorization: Bearer <admin-token>`.
- Bearer token matching is exact and case-sensitive in the current middleware.

Success response: `200 OK`

```json
{
  "items": [
    {
      "code": "abc123",
      "created_at": "2026-03-22T12:00:00Z",
      "expires_at": "2026-03-22T13:00:00Z",
      "metadata": {
        "ttl": "1h",
        "created_at": "2026-03-22T12:00:00Z",
        "expires_at": "2026-03-22T13:00:00Z",
        "remaining_views": 2
      }
    }
  ]
}
```

Behavior notes:

- The list is sorted by `created_at` descending.
- Internal helper keys such as `:views_remaining` and `:burn_claim` are filtered out.
- Expired or exhausted pastes are omitted.

Typical failure responses:

| Status | Format | Meaning |
| --- | --- | --- |
| `401` | Plain text | Missing or invalid bearer token |
| `500` | JSON | Internal storage error |

## 8. SPA and Static Asset Routes

### `GET /`
### `GET /admin`
### `GET /p/{code}`

These routes return the embedded frontend `index.html` shell so the React SPA can handle client-side routing.

### `GET /assets/*`

Serves embedded static assets from the frontend build output.

Notes:

- Only `GET` is allowed on SPA routes.
- Non-SPA unknown paths return `404 Not Found`.

## OpenAPI-style Notes

This service is small enough that the current codebase acts as the source of truth. If you later want machine-readable documentation, the clean next step would be generating an OpenAPI document from the endpoint list and DTOs in:

- `clipboard/backend/handler.go`
- `clipboard/backend/types.go`
- `clipboard/backend/metrics_types.go`
- `clipboard/backend/validate.go`
- `clipboard/backend/middleware.go`
