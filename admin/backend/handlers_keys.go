package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxKeyScanCount = 1000
	bigKeyWarnSize  = 1 << 20  // 1MB
	bigKeyMaxSize   = 50 << 20 // 50MB
)

type KeyListResponse struct {
	Keys   []string `json:"keys"`
	Cursor uint64   `json:"cursor,string"`
	Count  int      `json:"count"`
}

type KeyDetailResponse struct {
	Key       string `json:"key"`
	Type      string `json:"type"`
	TTL       int64  `json:"ttl_seconds"`
	Size      int    `json:"size"`
	Value     string `json:"value,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type KeySetRequest struct {
	Value string `json:"value"`
	TTL   int    `json:"ttl_seconds,omitempty"`
}

func (h *HTTPHandler) handleKeysList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	if h.deps.Store == nil {
		jsonError(w, http.StatusServiceUnavailable, "ERR_STORE_UNAVAILABLE", "store is not available")
		return
	}

	q := r.URL.Query()
	cursor, _ := strconv.ParseUint(q.Get("cursor"), 10, 64)
	pattern := q.Get("match")
	if pattern == "" {
		pattern = "*"
	}
	count, _ := strconv.Atoi(q.Get("count"))
	if count <= 0 || count > maxKeyScanCount {
		count = maxKeyScanCount
	}

	ctx := context.Background()
	keys, nextCursor, err := h.deps.Store.Scan(ctx, cursor, pattern, count)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "ERR_INTERNAL", fmt.Sprintf("scan failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, KeyListResponse{
		Keys:   keys,
		Cursor: nextCursor,
		Count:  len(keys),
	})
}

func (h *HTTPHandler) handleKeyByPath(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/v1/keys/")
	if key == "" {
		jsonError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", "key name is required")
		return
	}

	if h.deps.Store == nil {
		jsonError(w, http.StatusServiceUnavailable, "ERR_STORE_UNAVAILABLE", "store is not available")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleKeyGet(w, r, key)
	case http.MethodPut:
		h.handleKeySet(w, r, key)
	case http.MethodDelete:
		h.handleKeyDelete(w, r, key)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
	}
}

func (h *HTTPHandler) handleKeyGet(w http.ResponseWriter, _ *http.Request, key string) {
	ctx := context.Background()

	keyType, err := h.deps.Store.Type(ctx, key)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "ERR_INTERNAL", fmt.Sprintf("type check failed: %v", err))
		return
	}
	if keyType == "none" {
		jsonError(w, http.StatusNotFound, "ERR_NOT_FOUND", "key not found")
		return
	}

	ttl, err := h.deps.Store.TTL(ctx, key)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "ERR_INTERNAL", fmt.Sprintf("ttl check failed: %v", err))
		return
	}

	resp := KeyDetailResponse{
		Key:  key,
		Type: keyType,
		TTL:  int64(ttl.Seconds()),
	}

	data, err := h.deps.Store.GetBytes(ctx, key)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "ERR_INTERNAL", fmt.Sprintf("get failed: %v", err))
		return
	}

	resp.Size = len(data)

	if len(data) > bigKeyMaxSize {
		jsonError(w, http.StatusRequestEntityTooLarge, "ERR_KEY_TOO_LARGE",
			fmt.Sprintf("value size %d exceeds maximum %d bytes", len(data), bigKeyMaxSize))
		return
	}

	if len(data) > bigKeyWarnSize {
		resp.Value = string(data[:bigKeyWarnSize])
		resp.Truncated = true
	} else {
		resp.Value = string(data)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *HTTPHandler) handleKeySet(w http.ResponseWriter, r *http.Request, key string) {
	if !h.cfg.AllowDangerous {
		h.audit.Record(AuditEntry{
			Timestamp:  time.Now(),
			RemoteAddr: r.RemoteAddr,
			User:       basicAuthUser(r),
			Action:     "KEY_SET",
			Target:     key,
			Result:     "denied: dangerous operations disabled",
		})
		jsonError(w, http.StatusForbidden, "ERR_DANGEROUS_DISABLED",
			"write operations require -admin-allow-dangerous flag")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", "failed to read request body")
		return
	}

	var req KeySetRequest
	if err := json.Unmarshal(body, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if len(req.Value) > bigKeyMaxSize {
		jsonError(w, http.StatusRequestEntityTooLarge, "ERR_VALUE_TOO_LARGE",
			fmt.Sprintf("value size %d exceeds maximum %d bytes", len(req.Value), bigKeyMaxSize))
		return
	}

	ttl := time.Duration(req.TTL) * time.Second
	ctx := context.Background()
	setErr := h.deps.Store.Set(ctx, key, req.Value, ttl)

	result := "ok"
	if setErr != nil {
		result = fmt.Sprintf("failed: %v", setErr)
	}
	h.audit.Record(AuditEntry{
		Timestamp:  time.Now(),
		RemoteAddr: r.RemoteAddr,
		User:       basicAuthUser(r),
		Action:     "KEY_SET",
		Target:     key,
		Result:     result,
	})

	if setErr != nil {
		jsonError(w, http.StatusInternalServerError, "ERR_INTERNAL", fmt.Sprintf("set failed: %v", setErr))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *HTTPHandler) handleKeyDelete(w http.ResponseWriter, r *http.Request, key string) {
	if !h.cfg.AllowDangerous {
		h.audit.Record(AuditEntry{
			Timestamp:  time.Now(),
			RemoteAddr: r.RemoteAddr,
			User:       basicAuthUser(r),
			Action:     "KEY_DELETE",
			Target:     key,
			Result:     "denied: dangerous operations disabled",
		})
		jsonError(w, http.StatusForbidden, "ERR_DANGEROUS_DISABLED",
			"write operations require -admin-allow-dangerous flag")
		return
	}

	ctx := context.Background()
	deleted, delErr := h.deps.Store.Del(ctx, key)

	result := fmt.Sprintf("ok: %d deleted", deleted)
	if delErr != nil {
		result = fmt.Sprintf("failed: %v", delErr)
	}
	h.audit.Record(AuditEntry{
		Timestamp:  time.Now(),
		RemoteAddr: r.RemoteAddr,
		User:       basicAuthUser(r),
		Action:     "KEY_DELETE",
		Target:     key,
		Result:     result,
	})

	if delErr != nil {
		jsonError(w, http.StatusInternalServerError, "ERR_INTERNAL", fmt.Sprintf("delete failed: %v", delErr))
		return
	}

	if deleted == 0 {
		jsonError(w, http.StatusNotFound, "ERR_NOT_FOUND", "key not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func basicAuthUser(r *http.Request) string {
	user, _, ok := r.BasicAuth()
	if !ok {
		return ""
	}
	return user
}
