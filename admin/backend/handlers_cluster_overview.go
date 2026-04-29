package admin

import (
	"fmt"
	"net/http"

	"github.com/10yihang/autocache/internal/cluster"
)

type ClusterOverviewResponse struct {
	ClusterState   string                    `json:"cluster_state"`
	NodeCount      int                       `json:"node_count"`
	MasterCount    int                       `json:"master_count"`
	ReplicaCount   int                       `json:"replica_count"`
	SlotsAssigned  int                       `json:"slots_assigned"`
	SlotsMigrating int                       `json:"slots_migrating"`
	SelfNodeID     string                    `json:"self_node_id"`
	Nodes          []ClusterOverviewNode     `json:"nodes"`
	Replication    *ClusterReplicationHealth `json:"replication,omitempty"`
}

type ClusterOverviewNode struct {
	ID         string `json:"id"`
	Addr       string `json:"addr"`
	Role       string `json:"role"`
	State      string `json:"state"`
	SlotsCount int    `json:"slots_count"`
	Healthy    bool   `json:"healthy"`
}

type ClusterReplicationHealth struct {
	Available       bool `json:"available"`
	HealthyReplicas int  `json:"healthy_replicas"`
	LaggingReplicas int  `json:"lagging_replicas"`
}

func (h *HTTPHandler) handleClusterOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	if h.deps.Cluster == nil {
		jsonError(w, http.StatusServiceUnavailable, "ERR_CLUSTER_DISABLED", "cluster mode is not enabled")
		return
	}

	info := h.deps.Cluster.GetClusterInfo()
	nodes := h.deps.Cluster.GetNodes()
	slotMap := h.deps.Cluster.GetSlotMap()
	migrating := h.deps.Cluster.GetMigratingSlots()

	slotCounts := make(map[string]int)
	for slot := 0; slot < len(slotMap); slot++ {
		nid := slotMap[slot]
		if nid != "" {
			slotCounts[nid]++
		}
	}

	var masterCount, replicaCount int
	nodeEntries := make([]ClusterOverviewNode, 0, len(nodes))
	for _, n := range nodes {
		role := n.Role.String()
		entry := ClusterOverviewNode{
			ID:         n.ID,
			Addr:       n.Addr(),
			Role:       role,
			State:      n.State.String(),
			SlotsCount: slotCounts[n.ID],
			Healthy:    n.State == cluster.NodeStateConnected,
		}
		nodeEntries = append(nodeEntries, entry)
		if n.Role == cluster.NodeRoleMaster {
			masterCount++
		} else {
			replicaCount++
		}
	}

	var slotsAssigned int
	if v, ok := info["cluster_slots_assigned"]; ok {
		switch n := v.(type) {
		case int:
			slotsAssigned = n
		case int64:
			slotsAssigned = int(n)
		}
	}

	var repHealth *ClusterReplicationHealth
	if repMgr := h.deps.Replication; repMgr != nil {
		repHealth = &ClusterReplicationHealth{Available: true}
		slotReplicas := h.deps.Cluster.GetSlotReplicationState()
		for _, rs := range slotReplicas {
			for _, rep := range rs.Replicas {
				if rep.Healthy {
					repHealth.HealthyReplicas++
				} else {
					repHealth.LaggingReplicas++
				}
			}
		}
	}

	var clusterState string
	if v, ok := info["cluster_state"]; ok {
		clusterState = fmt.Sprintf("%v", v)
	}

	resp := ClusterOverviewResponse{
		ClusterState:   clusterState,
		NodeCount:      len(nodes),
		MasterCount:    masterCount,
		ReplicaCount:   replicaCount,
		SlotsAssigned:  slotsAssigned,
		SlotsMigrating: len(migrating),
		SelfNodeID:     h.deps.Cluster.GetNodeID(),
		Nodes:          nodeEntries,
		Replication:    repHealth,
	}

	writeJSON(w, http.StatusOK, resp)
}
