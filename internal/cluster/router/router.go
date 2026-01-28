// Package router provides routing logic for cluster key distribution.
package router

import (
	"context"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/hash"
)

// Router determines where a key should be handled.
type Router interface {
	Route(ctx context.Context, key []byte, askingFlag bool) RouteResult
	RouteMulti(ctx context.Context, keys [][]byte) RouteResult
}

// RouteResult contains routing decision.
type RouteResult struct {
	Local     bool
	Redirect  *Redirect
	CrossSlot bool
	Slot      uint16
}

// Redirect contains redirection details for MOVED/ASK responses.
type Redirect struct {
	Type RedirectType
	Slot uint16
	Addr string
}

// RedirectType indicates redirect reason.
type RedirectType int

const (
	RedirectMoved RedirectType = iota
	RedirectAsk
)

// ClusterRouter implements Router using cluster state.
type ClusterRouter struct {
	cluster *cluster.Cluster
}

// NewClusterRouter creates a router backed by cluster state.
func NewClusterRouter(c *cluster.Cluster) *ClusterRouter {
	return &ClusterRouter{cluster: c}
}

// Route determines routing for a single key based on cluster slot assignment.
// askingFlag indicates client sent ASKING - allows importing node to serve request.
func (r *ClusterRouter) Route(ctx context.Context, key []byte, askingFlag bool) RouteResult {
	if r.cluster == nil {
		return RouteResult{Local: true}
	}

	slot := hash.KeySlot(string(key))

	slotInfo := r.cluster.GetSlotManager().GetSlotInfo(slot)
	if slotInfo == nil || slotInfo.NodeID == "" {
		return RouteResult{Local: true, Slot: slot}
	}

	self := r.cluster.GetSelf()

	if slotInfo.NodeID == self.ID {
		if slotInfo.State == cluster.SlotStateExporting && slotInfo.Exporting != "" {
			return RouteResult{
				Redirect: &Redirect{
					Type: RedirectAsk,
					Slot: slot,
					Addr: r.getNodeAddr(slotInfo.Exporting),
				},
				Slot: slot,
			}
		}
		return RouteResult{Local: true, Slot: slot}
	}

	if slotInfo.State == cluster.SlotStateImporting && slotInfo.Importing != "" {
		if askingFlag {
			return RouteResult{Local: true, Slot: slot}
		}
	}

	node := r.cluster.GetSlotNode(slot)
	if node == nil {
		return RouteResult{Local: true, Slot: slot}
	}

	return RouteResult{
		Redirect: &Redirect{
			Type: RedirectMoved,
			Slot: slot,
			Addr: node.Addr(),
		},
		Slot: slot,
	}
}

// RouteMulti determines routing for multiple keys, checking for cross-slot access.
func (r *ClusterRouter) RouteMulti(ctx context.Context, keys [][]byte) RouteResult {
	if len(keys) == 0 {
		return RouteResult{Local: true}
	}

	if r.cluster == nil {
		return RouteResult{Local: true}
	}

	firstSlot := hash.KeySlot(string(keys[0]))

	for i := 1; i < len(keys); i++ {
		if hash.KeySlot(string(keys[i])) != firstSlot {
			return RouteResult{CrossSlot: true, Slot: firstSlot}
		}
	}

	return r.Route(ctx, keys[0], false)
}

func (r *ClusterRouter) getNodeAddr(nodeID string) string {
	nodes := r.cluster.GetNodes()
	for _, node := range nodes {
		if node.ID == nodeID {
			return node.Addr()
		}
	}
	return ""
}
