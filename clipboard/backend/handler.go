package clipboard

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strings"
)

type HTTPHandlerOptions struct{}

type HTTPHandler struct {
	service    *PasteService
	middleware *Middleware
	staticFS   fs.FS
	indexPath  string
}

func NewHTTPHandler(service *PasteService, middleware *Middleware, _ HTTPHandlerOptions) *HTTPHandler {
	assetsFS, indexPath := resolveStaticFS(StaticFS())
	return &HTTPHandler{
		service:    service,
		middleware: middleware,
		staticFS:   assetsFS,
		indexPath:  indexPath,
	}
}

func (h *HTTPHandler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/health", http.HandlerFunc(h.handleHealth))
	mux.Handle("/api/paste", h.middleware.RateLimit("create", h.middleware.RequireJSONBody(http.HandlerFunc(h.handleCreatePaste), MaxContentBytes)))
	mux.Handle("/api/paste/", h.middleware.RateLimit("read", http.HandlerFunc(h.handlePasteByCode)))
	mux.Handle("/raw/", h.middleware.RateLimit("read", http.HandlerFunc(h.handleRawPaste)))
	mux.Handle("/admin/stats", h.middleware.RequireAdmin(http.HandlerFunc(h.handleAdminStats)))
	mux.Handle("/admin/pastes", h.middleware.RequireAdmin(http.HandlerFunc(h.handleAdminPastes)))
	mux.Handle("/assets/", http.StripPrefix("/", http.FileServer(http.FS(h.staticFS))))
	mux.Handle("/", http.HandlerFunc(h.handleSPA))
	return mux
}

func (h *HTTPHandler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *HTTPHandler) handleCreatePaste(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var request CreatePasteRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
		return
	}

	response, err := h.service.Create(r.Context(), request)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, response)
}

func (h *HTTPHandler) handlePasteByCode(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimPrefix(r.URL.Path, "/api/paste/")
	if code == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "paste code is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		response, err := h.service.Get(r.Context(), code)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	case http.MethodDelete:
		response, err := h.service.Delete(r.Context(), code)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	default:
		methodNotAllowed(w)
	}
}

func (h *HTTPHandler) handleRawPaste(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	code := strings.TrimPrefix(r.URL.Path, "/raw/")
	if code == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "paste code is required")
		return
	}

	response, err := h.service.Get(r.Context(), code)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(response.Paste.Content))
}

func (h *HTTPHandler) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	stats := AdminStatsDTO{
		Tier:       TierStatsDTO{},
		Usage:      h.service.Stats(),
		RateLimits: h.middleware.Stats(),
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *HTTPHandler) handleAdminPastes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	list, err := h.service.ListAdmin(r.Context())
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *HTTPHandler) handleSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	if !isSPARoute(r.URL.Path) {
		http.NotFound(w, r)
		return
	}

	data, err := fs.ReadFile(h.staticFS, h.indexPath)
	if err != nil {
		http.Error(w, "index not available", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func isSPARoute(path string) bool {
	if path == "/" || path == "/admin" {
		return true
	}

	return strings.HasPrefix(path, "/p/")
}

func resolveStaticFS(staticFiles fs.FS) (fs.FS, string) {
	if _, err := fs.Stat(staticFiles, "embed/index.html"); err == nil {
		embedFS, subErr := fs.Sub(staticFiles, "embed")
		if subErr == nil {
			return embedFS, "index.html"
		}
	}

	return staticFiles, "index.html"
}

func (h *HTTPHandler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPasteNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, ErrContentRequired), errors.Is(err, ErrInvalidTTL), errors.Is(err, ErrContentTooLarge), errors.Is(err, ErrConflictingAccessPolicy):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func methodNotAllowed(w http.ResponseWriter) {
	writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
