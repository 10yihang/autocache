package cluster

import (
	"fmt"
	"log"
	"sync"

	"github.com/10yihang/autocache/internal/cluster/gossip"
	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/cluster/state"
)

type ClusterState int

const (
	ClusterStateDown ClusterState = iota
	ClusterStateOK
	ClusterStateFail
)

func (s ClusterState) String() string {
	switch s {
	case ClusterStateDown:
		return "fail"
	case ClusterStateOK:
		return "ok"
	case ClusterStateFail:
		return "fail"
	default:
		return "unknown"
	}
}

type Cluster struct {
	self         *Node
	slots        *SlotManager
	gossip       *gossip.Gossip
	state        ClusterState
	stateManager *state.StateManager
	currentEpoch uint64
	myEpoch      uint64
	mu           sync.RWMutex
}

type Config struct {
	NodeID      string
	BindAddr    string
	Port        int
	ClusterPort int
	Seeds       []string
}

func NewCluster(cfg *Config, stateManager *state.StateManager) (*Cluster, error) {
	self := &Node{
		ID:          cfg.NodeID,
		IP:          cfg.BindAddr,
		Port:        cfg.Port,
		ClusterPort: cfg.ClusterPort,
		Role:        NodeRoleMaster,
		State:       NodeStateConnected,
		FailReports: make(map[string]int64),
	}

	if self.ID == "" {
		self.ID = generateNodeID()
	}

	slots := NewSlotManager()

	c := &Cluster{
		self:         self,
		slots:        slots,
		state:        ClusterStateDown,
		stateManager: stateManager,
	}

	if stateManager != nil {
		stateManager.SetProvider(c)
		slots.SetStateManager(stateManager)
	}

	gossipNode := &gossip.GossipNode{
		ID:          self.ID,
		IP:          self.IP,
		Port:        self.Port,
		ClusterPort: self.ClusterPort,
		Role:        gossip.NodeRoleMaster,
		State:       gossip.NodeStateConnected,
		FailReports: make(map[string]int64),
	}

	c.gossip = gossip.NewGossip(gossipNode, slots)
	c.gossip.SetEventHandlers(c.onNodeJoin, c.onNodeLeave, c.onSlotChange)

	return c, nil
}

func (c *Cluster) Start(seeds []string) error {
	if err := c.gossip.Start(); err != nil {
		return err
	}

	for _, seed := range seeds {
		if err := c.gossip.Meet(seed); err != nil {
			log.Printf("Failed to meet seed %s: %v", seed, err)
		}
	}

	c.state = ClusterStateOK
	return nil
}

func (c *Cluster) Stop() error {
	return c.gossip.Stop()
}

func (c *Cluster) GetSelf() *Node {
	return c.self.Clone()
}

func (c *Cluster) GetNodes() []*Node {
	gossipNodes := c.gossip.GetNodes()
	nodes := make([]*Node, len(gossipNodes))
	for i, gn := range gossipNodes {
		nodes[i] = gossipNodeToNode(gn)
	}
	return nodes
}

func (c *Cluster) GetSlotNode(slot uint16) *Node {
	nodeID := c.slots.GetSlotNode(slot)
	if nodeID == "" {
		return nil
	}
	gn := c.gossip.GetNode(nodeID)
	if gn == nil {
		return nil
	}
	return gossipNodeToNode(gn)
}

func (c *Cluster) GetKeyNode(key string) *Node {
	slot := hash.KeySlot(key)
	return c.GetSlotNode(slot)
}

func (c *Cluster) IsLocalKey(key string) bool {
	slot := hash.KeySlot(key)
	nodeID := c.slots.GetSlotNode(slot)
	return nodeID == c.self.ID
}

func (c *Cluster) GetKeySlot(key string) uint16 {
	return hash.KeySlot(key)
}

func (c *Cluster) RouteKey(key string) (*Node, error) {
	slot := hash.KeySlot(key)
	slotInfo := c.slots.GetSlotInfo(slot)

	if slotInfo == nil || slotInfo.NodeID == "" {
		return nil, fmt.Errorf("slot %d not assigned", slot)
	}

	if slotInfo.NodeID == c.self.ID {
		if slotInfo.State == SlotStateExporting {
			gn := c.gossip.GetNode(slotInfo.Exporting)
			if gn != nil {
				return gossipNodeToNode(gn), ErrAsk
			}
		}
		return nil, nil
	}

	gn := c.gossip.GetNode(slotInfo.NodeID)
	if gn != nil {
		return gossipNodeToNode(gn), ErrMoved
	}
	return nil, ErrMoved
}

func (c *Cluster) AssignSlots(slots []uint16) error {
	for _, slot := range slots {
		if err := c.slots.AssignSlot(slot, c.self.ID); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cluster) AssignSlotRange(start, end uint16) error {
	return c.slots.AssignSlotRange(start, end, c.self.ID)
}

func (c *Cluster) Meet(addr string) error {
	return c.gossip.Meet(addr)
}

func (c *Cluster) GetClusterInfo() map[string]interface{} {
	nodes := c.gossip.GetNodes()

	masterCount := 0
	for _, node := range nodes {
		if node.Role == gossip.NodeRoleMaster {
			masterCount++
		}
	}

	return map[string]interface{}{
		"cluster_state":          c.state.String(),
		"cluster_slots_assigned": c.slots.CountAssigned(),
		"cluster_slots_ok":       c.slots.CountAssigned(),
		"cluster_known_nodes":    len(nodes),
		"cluster_size":           masterCount,
		"cluster_current_epoch":  0,
		"cluster_my_epoch":       0,
	}
}

func (c *Cluster) GetClusterSlots() []SlotRange {
	return c.slots.GetClusterSlots()
}

func (c *Cluster) GetSlotManager() *SlotManager {
	return c.slots
}

func (c *Cluster) GetNodeID() string {
	return c.self.ID
}

func (c *Cluster) GetNodeInfos() []state.NodeInfo {
	gossipNodes := c.gossip.GetNodes()
	nodes := make([]state.NodeInfo, len(gossipNodes))
	for i, gn := range gossipNodes {
		role := "master"
		if gn.Role == gossip.NodeRoleReplica {
			role = "replica"
		}
		nodes[i] = state.NodeInfo{
			ID:          gn.ID,
			Addr:        gn.Addr(),
			ClusterPort: gn.ClusterPort,
			Role:        role,
			MasterID:    gn.MasterID,
		}
	}
	return nodes
}

func (c *Cluster) GetSlotMap() [16384]string {
	return c.slots.GetSlotMapSnapshot()
}

func (c *Cluster) GetMigratingSlots() map[uint16]state.MigrationState {
	return c.slots.GetMigratingSlots()
}

func (c *Cluster) GetCurrentEpoch() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentEpoch
}

func (c *Cluster) GetMyEpoch() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.myEpoch
}

func (c *Cluster) IncrementEpoch() uint64 {
	c.mu.Lock()
	c.currentEpoch++
	epoch := c.currentEpoch
	c.mu.Unlock()

	if c.stateManager != nil {
		c.stateManager.MarkDirty()
	}
	return epoch
}

func (c *Cluster) RestoreState(ps *state.PersistentState) error {
	c.mu.Lock()
	c.currentEpoch = ps.CurrentEpoch
	c.myEpoch = ps.MyEpoch
	c.mu.Unlock()

	c.slots.RestoreFromState(ps.SlotMap, ps.MigratingSlots)
	return nil
}

func (c *Cluster) onNodeJoin(node *gossip.GossipNode) {
	log.Printf("Node joined: %s (%s)", node.ID[:8], node.Addr())
}

func (c *Cluster) onNodeLeave(node *gossip.GossipNode) {
	log.Printf("Node left: %s (%s)", node.ID[:8], node.Addr())
}

func (c *Cluster) onSlotChange(slot uint16, nodeID string) {
	log.Printf("Slot %d assigned to %s", slot, nodeID[:8])
}

func gossipNodeToNode(gn *gossip.GossipNode) *Node {
	var role NodeRole
	if gn.Role == gossip.NodeRoleMaster {
		role = NodeRoleMaster
	} else {
		role = NodeRoleReplica
	}

	var state NodeState
	switch gn.State {
	case gossip.NodeStateConnected:
		state = NodeStateConnected
	case gossip.NodeStatePFail:
		state = NodeStatePFail
	case gossip.NodeStateFail:
		state = NodeStateFail
	default:
		state = NodeStateUnknown
	}

	return &Node{
		ID:           gn.ID,
		IP:           gn.IP,
		Port:         gn.Port,
		ClusterPort:  gn.ClusterPort,
		Role:         role,
		MasterID:     gn.MasterID,
		State:        state,
		PingSent:     gn.PingSent,
		PongReceived: gn.PongReceived,
		FailReports:  gn.FailReports,
	}
}

type ClusterError struct {
	Type string
	Slot uint16
	Addr string
}

func (e *ClusterError) Error() string {
	return fmt.Sprintf("%s %d %s", e.Type, e.Slot, e.Addr)
}

var (
	ErrMoved = &ClusterError{Type: "MOVED"}
	ErrAsk   = &ClusterError{Type: "ASK"}
)
