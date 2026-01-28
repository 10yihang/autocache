package migration

import (
	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/state"
)

type ClusterSlotProvider struct {
	cluster *cluster.Cluster
}

func NewClusterSlotProvider(c *cluster.Cluster) *ClusterSlotProvider {
	return &ClusterSlotProvider{cluster: c}
}

func (p *ClusterSlotProvider) GetMigratingSlots() map[uint16]MigrationInfo {
	stateMigrations := p.cluster.GetMigratingSlots()
	result := make(map[uint16]MigrationInfo, len(stateMigrations))

	for slot, migration := range stateMigrations {
		if migration.State != "exporting" {
			continue
		}
		info := MigrationInfo{
			TargetNodeID: migration.TargetNodeID,
		}
		if node := p.cluster.GetSlotNode(slot); node != nil {
			info.TargetAddr = p.getNodeAddr(migration.TargetNodeID)
		}
		result[slot] = info
	}

	return result
}

func (p *ClusterSlotProvider) GetNode(nodeID string) (string, bool) {
	nodes := p.cluster.GetNodes()
	for _, node := range nodes {
		if node.ID == nodeID {
			return node.Addr(), true
		}
	}
	return "", false
}

func (p *ClusterSlotProvider) getNodeAddr(nodeID string) string {
	addr, _ := p.GetNode(nodeID)
	return addr
}

var _ SlotProvider = (*ClusterSlotProvider)(nil)

var _ = state.MigrationState{}
