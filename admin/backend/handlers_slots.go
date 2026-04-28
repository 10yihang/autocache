package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/10yihang/autocache/internal/cluster"
)

type SlotInfoResponse struct {
	Slot        uint16            `json:"slot"`
	State       string            `json:"state"`
	NodeID      string            `json:"node_id"`
	PrimaryID   string            `json:"primary_id"`
	ConfigEpoch uint64            `json:"config_epoch,string"`
	NextLSN     uint64            `json:"next_lsn,string"`
	Replicas    []SlotReplicaInfo `json:"replicas,omitempty"`
	Importing   string            `json:"importing,omitempty"`
	Exporting   string            `json:"exporting,omitempty"`
}

type SlotReplicaInfo struct {
	NodeID   string `json:"node_id"`
	MatchLSN uint64 `json:"match_lsn,string"`
	Healthy  bool   `json:"healthy"`
}

type MigrateSlotRequest struct {
	TargetNodeID string `json:"target_node_id"`
}

func (h *HTTPHandler) handleSlotByPath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/slots/")
	if path == "" {
		jsonError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", "slot number is required")
		return
	}

	if h.deps.Cluster == nil {
		jsonError(w, http.StatusServiceUnavailable, "ERR_CLUSTER_DISABLED", "cluster mode is not enabled")
		return
	}

	parts := strings.SplitN(path, "/", 2)
	slotNum, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil || slotNum >= 16384 {
		jsonError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", "invalid slot number (0-16383)")
		return
	}
	slot := uint16(slotNum)

	if len(parts) == 2 && parts[1] == "migrate" {
		h.handleSlotMigrate(w, r, slot)
		return
	}

	if len(parts) == 1 {
		h.handleSlotInfo(w, r, slot)
		return
	}

	jsonError(w, http.StatusNotFound, "ERR_NOT_FOUND", "not found")
}

func (h *HTTPHandler) handleSlotInfo(w http.ResponseWriter, r *http.Request, slot uint16) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	slotInfo := h.deps.Cluster.GetSlotManager().GetSlotInfo(slot)
	if slotInfo == nil {
		jsonError(w, http.StatusNotFound, "ERR_NOT_FOUND", "slot info not available")
		return
	}

	resp := SlotInfoResponse{
		Slot:        slot,
		State:       slotStateName(slotInfo.State),
		NodeID:      slotInfo.NodeID,
		PrimaryID:   slotInfo.PrimaryID,
		ConfigEpoch: slotInfo.ConfigEpoch,
		NextLSN:     slotInfo.NextLSN,
		Importing:   slotInfo.Importing,
		Exporting:   slotInfo.Exporting,
	}

	for _, rep := range slotInfo.Replicas {
		resp.Replicas = append(resp.Replicas, SlotReplicaInfo{
			NodeID:   rep.NodeID,
			MatchLSN: rep.MatchLSN,
			Healthy:  rep.Healthy,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *HTTPHandler) handleSlotMigrate(w http.ResponseWriter, r *http.Request, slot uint16) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	if !h.cfg.AllowDangerous {
		h.audit.Record(AuditEntry{
			Timestamp:  time.Now(),
			RemoteAddr: r.RemoteAddr,
			User:       basicAuthUser(r),
			Action:     "SLOT_MIGRATE",
			Target:     fmt.Sprintf("slot:%d", slot),
			Result:     "denied: dangerous operations disabled",
		})
		jsonError(w, http.StatusForbidden, "ERR_DANGEROUS_DISABLED",
			"slot migration requires -admin-allow-dangerous flag")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", "failed to read request body")
		return
	}

	var req MigrateSlotRequest
	if err := json.Unmarshal(body, &req); err != nil {
		jsonError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if req.TargetNodeID == "" {
		jsonError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", "target_node_id is required")
		return
	}

	h.audit.Record(AuditEntry{
		Timestamp:  time.Now(),
		RemoteAddr: r.RemoteAddr,
		User:       basicAuthUser(r),
		Action:     "SLOT_MIGRATE",
		Target:     fmt.Sprintf("slot:%d -> %s", slot, req.TargetNodeID),
		Result:     "501: not yet implemented in wave 2",
	})

	jsonError(w, http.StatusNotImplemented, "ERR_NOT_IMPLEMENTED",
		"slot migration execution is not yet implemented; migration must flow through the protocol layer")
}

func slotStateName(s cluster.SlotState) string {
	switch s {
	case cluster.SlotStateNormal:
		return "normal"
	case cluster.SlotStateImporting:
		return "importing"
	case cluster.SlotStateExporting:
		return "exporting"
	default:
		return "unknown"
	}
}
