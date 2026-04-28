package admin

import (
	"net/http"
	"strconv"
)

type ClusterInfoResponse struct {
	Info map[string]interface{} `json:"info"`
}

type ClusterNodeResponse struct {
	ID          string `json:"id"`
	Addr        string `json:"addr"`
	ClusterPort int    `json:"cluster_port"`
	Role        string `json:"role"`
	MasterID    string `json:"master_id,omitempty"`
	State       string `json:"state"`
}

type ClusterNodesResponse struct {
	Nodes []ClusterNodeResponse `json:"nodes"`
}

type ClusterSlotsResponse struct {
	SlotMap   [16384]string            `json:"slot_map"`
	Migrating map[string]MigrationInfo `json:"migrating,omitempty"`
}

type MigrationInfo struct {
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
	State        string `json:"state"`
}

func (h *HTTPHandler) handleClusterInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	if h.deps.Cluster == nil {
		jsonError(w, http.StatusServiceUnavailable, "ERR_CLUSTER_DISABLED", "cluster mode is not enabled")
		return
	}

	info := h.deps.Cluster.GetClusterInfo()
	writeJSON(w, http.StatusOK, ClusterInfoResponse{Info: info})
}

func (h *HTTPHandler) handleClusterNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	if h.deps.Cluster == nil {
		jsonError(w, http.StatusServiceUnavailable, "ERR_CLUSTER_DISABLED", "cluster mode is not enabled")
		return
	}

	nodes := h.deps.Cluster.GetNodes()
	resp := ClusterNodesResponse{
		Nodes: make([]ClusterNodeResponse, 0, len(nodes)),
	}
	for _, n := range nodes {
		resp.Nodes = append(resp.Nodes, ClusterNodeResponse{
			ID:          n.ID,
			Addr:        n.Addr(),
			ClusterPort: n.ClusterPort,
			Role:        n.Role.String(),
			MasterID:    n.MasterID,
			State:       n.State.String(),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *HTTPHandler) handleClusterSlots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	if h.deps.Cluster == nil {
		jsonError(w, http.StatusServiceUnavailable, "ERR_CLUSTER_DISABLED", "cluster mode is not enabled")
		return
	}

	slotMap := h.deps.Cluster.GetSlotMap()
	migrating := h.deps.Cluster.GetMigratingSlots()

	var migInfo map[string]MigrationInfo
	if len(migrating) > 0 {
		migInfo = make(map[string]MigrationInfo, len(migrating))
		for slot, ms := range migrating {
			migInfo[slotKey(slot)] = MigrationInfo{
				SourceNodeID: ms.SourceNodeID,
				TargetNodeID: ms.TargetNodeID,
				State:        ms.State,
			}
		}
	}

	writeJSON(w, http.StatusOK, ClusterSlotsResponse{
		SlotMap:   slotMap,
		Migrating: migInfo,
	})
}

func slotKey(slot uint16) string {
	return strconv.FormatUint(uint64(slot), 10)
}
