package admin

import (
	"net/http"
)

type ReplicationStatusResponse struct {
	Available bool             `json:"available"`
	Slots     []SlotLogInfo    `json:"slots,omitempty"`
	Acks      []ReplicaAckInfo `json:"acks,omitempty"`
}

type SlotLogInfo struct {
	Slot     uint16 `json:"slot"`
	FirstLSN uint64 `json:"first_lsn,string"`
	LastLSN  uint64 `json:"last_lsn,string"`
}

type ReplicaAckInfo struct {
	Slot       uint16 `json:"slot"`
	Epoch      uint64 `json:"epoch,string"`
	NodeID     string `json:"node_id"`
	AppliedLSN uint64 `json:"applied_lsn,string"`
}

type TieredStatsResponse struct {
	Available      bool  `json:"available"`
	HotTierKeys    int64 `json:"hot_tier_keys,omitempty"`
	HotTierSize    int64 `json:"hot_tier_size,omitempty"`
	WarmTierKeys   int64 `json:"warm_tier_keys,omitempty"`
	WarmTierSize   int64 `json:"warm_tier_size,omitempty"`
	ColdTierKeys   int64 `json:"cold_tier_keys,omitempty"`
	ColdTierSize   int64 `json:"cold_tier_size,omitempty"`
	TotalKeys      int64 `json:"total_keys,omitempty"`
	TotalSize      int64 `json:"total_size,omitempty"`
	MigrationsUp   int64 `json:"migrations_up,omitempty"`
	MigrationsDown int64 `json:"migrations_down,omitempty"`
}

type AuditResponse struct {
	Entries []AuditEntry `json:"entries"`
}

func (h *HTTPHandler) handleReplicationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	if h.deps.Replication == nil {
		writeJSON(w, http.StatusOK, ReplicationStatusResponse{Available: false})
		return
	}

	logStore := h.deps.Replication.LogStore()
	ackTracker := h.deps.Replication.AckTracker()

	resp := ReplicationStatusResponse{Available: true}

	if logStore != nil {
		for slot := uint16(0); slot < 16384; slot++ {
			first, last, ok := logStore.Range(slot)
			if ok {
				resp.Slots = append(resp.Slots, SlotLogInfo{
					Slot:     slot,
					FirstLSN: first,
					LastLSN:  last,
				})
			}
		}
	}

	if ackTracker != nil {
		for slot := uint16(0); slot < 16384; slot++ {
			acks := ackTracker.Snapshot(slot)
			for _, ack := range acks {
				resp.Acks = append(resp.Acks, ReplicaAckInfo{
					Slot:       ack.Slot,
					Epoch:      ack.Epoch,
					NodeID:     ack.NodeID,
					AppliedLSN: ack.AppliedLSN,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *HTTPHandler) handleTieredStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	if h.deps.Tiered == nil {
		writeJSON(w, http.StatusOK, TieredStatsResponse{Available: false})
		return
	}

	stats := h.deps.Tiered.GetStats()

	writeJSON(w, http.StatusOK, TieredStatsResponse{
		Available:      true,
		HotTierKeys:    stats.HotTierKeys,
		HotTierSize:    stats.HotTierSize,
		WarmTierKeys:   stats.WarmTierKeys,
		WarmTierSize:   stats.WarmTierSize,
		ColdTierKeys:   stats.ColdTierKeys,
		ColdTierSize:   stats.ColdTierSize,
		TotalKeys:      stats.TotalKeys,
		TotalSize:      stats.TotalSize,
		MigrationsUp:   stats.MigrationsUp,
		MigrationsDown: stats.MigrationsDown,
	})
}

func (h *HTTPHandler) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	entries := h.audit.Snapshot()
	if entries == nil {
		entries = []AuditEntry{}
	}

	writeJSON(w, http.StatusOK, AuditResponse{Entries: entries})
}
