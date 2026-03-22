# Clipboard Migration Map

This file tracks how the clipboard prototype is reorganized into the new application layout.

## Target Layout

- `clipboard/frontend`: React + Vite + TypeScript SPA source.
- `clipboard/backend`: Go backend implementation and embedded SPA assets.
- `clipboard/backend/embed`: Go-embeddable frontend build output.

## Source to Target Mapping

- `internal/clipboard/*.go` -> `clipboard/backend/*.go`
- `internal/clipboard/web/*` -> replaced by frontend build output copied to `clipboard/backend/embed/`
- `cmd/clipboard/main.go` import path:
  - from `github.com/10yihang/autocache/internal/clipboard`
  - to `github.com/10yihang/autocache/clipboard/backend`

## Transitional Policy

- During migration, `internal/clipboard` may remain as temporary glue while tests and imports are moved.
- Final authoritative clipboard backend code lives in `clipboard/backend`.
