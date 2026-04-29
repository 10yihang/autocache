package cluster

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/10yihang/autocache/internal/cluster/gossip"
	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/cluster/hotspot"
	"github.com/10yihang/autocache/internal/cluster/rebalance"
	"github.com/10yihang/autocache/internal/cluster/replication"
	"github.com/10yihang/autocache/internal/cluster/state"
	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/memory"
)

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

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
	replication  *replication.Manager
	currentEpoch uint64
	myEpoch      uint64
	mu           sync.RWMutex

	engine         DrainEngine
	drainTimeout   time.Duration
	rebalanceMgr   *rebalance.Manager

	onReplicate func(masterAddr, replicaID string) // callback to start ReplicaClient
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
	c.replication = replication.NewManager(replication.NewLogStore(1024), slots.AllocateLSN)
	c.replication.SetReplicaResolver(c.replicationTargetsForSlot)
	c.replication.SetReplicaLSNUpdater(slots.UpdateReplicaLSN)

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
	c.gossip.SetCurrentEpoch(c.currentEpoch)

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
	if c.replication != nil {
		c.replication.Close()
	}
	return c.gossip.Stop()
}

// DrainEngine is the subset of storage operations needed for slot drain.
type DrainEngine interface {
	GetEntry(ctx context.Context, key string) (*engine.Entry, error)
	Del(ctx context.Context, keys ...string) (int64, error)
	KeysInSlot(slot uint16, count int) []string
}

func (c *Cluster) SetEngine(engine DrainEngine) {
	c.engine = engine
}

func (c *Cluster) SetDrainTimeout(timeout time.Duration) {
	c.drainTimeout = timeout
}

// InitRebalance creates and starts the auto-rebalance manager.
func (c *Cluster) InitRebalance(detector *hotspot.Detector, cfg rebalance.Config) {
	c.rebalanceMgr = rebalance.New(c, detector, cfg)
}

// GetPeerLoads returns load info for all peer master nodes.
func (c *Cluster) GetPeerLoads() []rebalance.PeerLoad {
	nodes := c.GetNodes()
	peers := make([]rebalance.PeerLoad, 0, len(nodes))
	for _, n := range nodes {
		if n.ID == c.self.ID || n.Role != NodeRoleMaster {
			continue
		}
		if n.State != NodeStateConnected {
			continue
		}
		peers = append(peers, rebalance.PeerLoad{
			NodeID:   n.ID,
			Addr:     n.Addr(),
			TotalQPS: n.Load.TotalQPS,
		})
	}
	return peers
}

// GetSlotOwner returns the node ID that owns the given slot.
func (c *Cluster) GetSlotOwner(slot uint16) string {
	return c.slots.GetSlotNode(slot)
}

// IsSlotLocal returns true if this node owns the given slot.
func (c *Cluster) IsSlotLocal(slot uint16) bool {
	return c.slots.GetSlotNode(slot) == c.self.ID
}

// MigrateSlot triggers migration of a slot to a target node.
func (c *Cluster) MigrateSlot(slot uint16, targetNodeID string) error {
	// Mark slot as migrating.
	c.slots.SetExporting(slot, targetNodeID)

	// Migrate keys using existing drain infrastructure.
	peer := c.getPeerAddr(targetNodeID)
	if peer == "" {
		c.slots.SetStable(slot)
		return fmt.Errorf("target node %s not found", shortID(targetNodeID))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.drainSlot(ctx, slot, peer, 10*time.Second); err != nil {
		c.slots.SetStable(slot)
		return fmt.Errorf("migrate keys for slot %d: %w", slot, err)
	}

	// Finalize slot ownership.
	c.slots.FinishMigration(slot, targetNodeID)

	if err := c.BroadcastSlotOwnershipChange(slot, targetNodeID); err != nil {
		log.Printf("[rebalance] Failed to broadcast slot %d ownership change: %v", slot, err)
	}

	return nil
}

func (c *Cluster) getPeerAddr(nodeID string) string {
	nodes := c.GetNodes()
	for _, n := range nodes {
		if n.ID == nodeID {
			return n.Addr()
		}
	}
	return ""
}

// StopRebalance stops the rebalance manager.
func (c *Cluster) StopRebalance() {
	if c.rebalanceMgr != nil {
		c.rebalanceMgr.Stop()
	}
}

// DrainSlots migrates all slots owned by this node to peer masters before shutdown.
// Best-effort: logs errors and continues, never blocks shutdown.
func (c *Cluster) DrainSlots(ctx context.Context) error {
	mySlots := c.slots.GetNodeSlots(c.self.ID)
	if len(mySlots) == 0 {
		log.Println("[drain] No slots owned by this node, skipping drain")
		return nil
	}

	nodes := c.GetNodes()
	var peers []*Node
	for _, n := range nodes {
		if n.ID != c.self.ID && n.Role == NodeRoleMaster && n.State == NodeStateConnected {
			if n.IP != "" && n.Port > 0 {
				peers = append(peers, n)
			}
		}
	}
	if len(peers) == 0 {
		log.Println("[drain] No peer master nodes available, skipping drain")
		return nil
	}

	if c.engine == nil {
		return fmt.Errorf("engine not set, cannot drain slots")
	}

	connTimeout := 5 * time.Second
	if c.drainTimeout > 0 && c.drainTimeout < connTimeout {
		connTimeout = c.drainTimeout / time.Duration(len(mySlots)+1)
	}

	log.Printf("[drain] Draining %d slots across %d peer nodes", len(mySlots), len(peers))

	for i, slot := range mySlots {
		target := peers[i%len(peers)]
		targetAddr := target.Addr()

		log.Printf("[drain] Slot %d -> %s (%s)", slot, shortID(target.ID), targetAddr)

		c.slots.SetExporting(slot, target.ID)

		if err := c.drainSlot(ctx, slot, targetAddr, connTimeout); err != nil {
			log.Printf("[drain] Slot %d migration failed: %v", slot, err)
			c.slots.SetStable(slot)
			continue
		}

		c.slots.FinishMigration(slot, target.ID)

		if err := c.BroadcastSlotOwnershipChange(slot, target.ID); err != nil {
			log.Printf("[drain] Slot %d ownership broadcast failed: %v", slot, err)
		}

		log.Printf("[drain] Slot %d drained successfully", slot)
	}

	log.Printf("[drain] Completed drain")
	return nil
}

// drainSlot migrates all keys in a slot to the target address using TCP.
func (c *Cluster) drainSlot(ctx context.Context, slot uint16, targetAddr string, timeout time.Duration) error {
	keys := c.engine.KeysInSlot(slot, 0)
	if len(keys) == 0 {
		return nil
	}

	for _, key := range keys {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := c.drainKey(ctx, key, targetAddr, timeout); err != nil {
			return fmt.Errorf("key %s: %w", key, err)
		}
	}

	return nil
}

func (c *Cluster) drainKey(ctx context.Context, key string, targetAddr string, timeout time.Duration) error {
	entry, err := c.engine.GetEntry(ctx, key)
	if err != nil {
		return fmt.Errorf("get entry: %w", err)
	}

	var ttlMs int64
	if !entry.ExpireAt.IsZero() {
		ttlMs = time.Until(entry.ExpireAt).Milliseconds()
		if ttlMs < 0 {
			// key already expired, just delete locally
			_, _ = c.engine.Del(ctx, key)
			return nil
		}
	}

	serialized, err := memory.SerializeEntryValue(entry)
	if err != nil {
		return fmt.Errorf("serialize: %w", err)
	}

	conn, err := net.DialTimeout("tcp", targetAddr, timeout)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(timeout))

	// ASKING
	if _, err := conn.Write([]byte("*1\r\n$6\r\nASKING\r\n")); err != nil {
		return fmt.Errorf("write ASKING: %w", err)
	}
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("read ASKING: %w", err)
	}
	if resp := string(buf[:n]); resp != "+OK\r\n" {
		return fmt.Errorf("ASKING unexpected response: %s", resp)
	}

	// RESTORE with REPLACE
	keyLen := len(key)
	ttlStr := strconv.FormatInt(ttlMs, 10)
	serializedLen := len(serialized)
	restoreCmd := fmt.Sprintf("*5\r\n$7\r\nRESTORE\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$7\r\nREPLACE\r\n",
		keyLen, key, len(ttlStr), ttlStr, serializedLen, serialized)

	if _, err := conn.Write([]byte(restoreCmd)); err != nil {
		return fmt.Errorf("write RESTORE: %w", err)
	}
	n, err = conn.Read(buf)
	if err != nil {
		return fmt.Errorf("read RESTORE: %w", err)
	}
	if resp := string(buf[:n]); resp != "+OK\r\n" {
		return fmt.Errorf("RESTORE failed: %s", resp)
	}

	// Delete locally
	if _, err := c.engine.Del(ctx, key); err != nil {
		return fmt.Errorf("del local: %w", err)
	}

	return nil
}

func (c *Cluster) SetHotspotDetector(d *hotspot.Detector) {
	c.gossip.SetHotspotDetector(d)
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
		"cluster_current_epoch":  c.GetCurrentEpoch(),
		"cluster_my_epoch":       c.GetMyEpoch(),
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

func (c *Cluster) GetSlotReplicationState() map[uint16]state.SlotReplicaSet {
	return c.slots.GetSlotReplicationState()
}

// ReplicateTo makes this node a replica of the specified master node.
// It configures all slots owned by master as replicated to self.
func (c *Cluster) ReplicateTo(masterNodeID string) error {
	c.mu.Lock()
	node := c.gossip.GetNode(masterNodeID)
	if node == nil {
		c.mu.Unlock()
		return fmt.Errorf("master node %s not known", masterNodeID)
	}
	if node.Role != gossip.NodeRoleMaster {
		c.mu.Unlock()
		return fmt.Errorf("node %s is not a master", masterNodeID)
	}
	c.mu.Unlock()

	selfID := c.self.ID

	// Set self role to replica
	c.self.Role = NodeRoleReplica
	c.self.MasterID = masterNodeID
	c.gossip.SetSelfRole(gossip.NodeRoleReplica, masterNodeID)

	// Increment epoch for this reconfiguration
	c.IncrementEpoch()
	epoch := c.GetCurrentEpoch()

	// Add self as replica for all slots owned by master
	count, err := c.slots.AddReplicaToMaster(masterNodeID, selfID, epoch)
	if err != nil {
		return err
	}
	log.Printf("Configured replication: %d slots of master %s now replicated to self (%s)", count, masterNodeID, selfID)

	// Start persistent replica→master connection (Redis-style replication).
	if c.onReplicate != nil {
		c.onReplicate(node.Addr(), selfID)
	}
	return nil
}

// SetOnReplicate sets a callback that is invoked after a successful CLUSTER REPLICATE
// to start the persistent replica→master connection.
func (c *Cluster) SetOnReplicate(fn func(masterAddr, replicaID string)) {
	c.onReplicate = fn
}

// MasterAddr returns the address of the master this node replicates, if any.
func (c *Cluster) GetMasterAddr() string {
	masterID := c.self.MasterID
	if masterID == "" {
		return ""
	}
	node := c.gossip.GetNode(masterID)
	if node == nil {
		return ""
	}
	return node.Addr()
}

func (c *Cluster) GetReplicationManager() *replication.Manager {
	return c.replication
}

func (c *Cluster) replicationTargetsForSlot(slot uint16) []replication.PeerTarget {
	info := c.slots.GetSlotInfo(slot)
	if info == nil || len(info.Replicas) == 0 {
		return nil
	}

	targets := make([]replication.PeerTarget, 0, len(info.Replicas))
	for _, replica := range info.Replicas {
		node := c.gossip.GetNode(replica.NodeID)
		if node == nil {
			continue
		}
		targets = append(targets, replication.PeerTarget{
			NodeID: replica.NodeID,
			Addr:   node.Addr(),
		})
	}
	return targets
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
	c.gossip.SetCurrentEpoch(epoch)

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
	c.gossip.SetCurrentEpoch(ps.CurrentEpoch)

	c.slots.RestoreFromState(ps.SlotMap, ps.MigratingSlots, ps.SlotReplicas)
	return nil
}

func (c *Cluster) CanAcceptWrite(key string) bool {
	slot := hash.KeySlot(key)
	return c.CanAcceptWriteForSlot(slot)
}

func (c *Cluster) CanAcceptWriteForSlot(slot uint16) bool {
	info := c.slots.GetSlotInfo(slot)
	if info == nil {
		return true
	}
	primaryID := info.PrimaryID
	if primaryID == "" {
		primaryID = info.NodeID
	}
	if primaryID == "" {
		return true
	}
	return primaryID == c.self.ID
}

func (c *Cluster) BroadcastSlotOwnershipChange(slot uint16, newPrimary string) error {
	if c == nil || c.gossip == nil {
		return fmt.Errorf("cluster gossip not configured")
	}
	return c.gossip.BroadcastSlotOwnership(slot, newPrimary)
}

func (c *Cluster) onNodeJoin(node *gossip.GossipNode) {
	log.Printf("Node joined: %s (%s)", shortID(node.ID), node.Addr())
}

func (c *Cluster) onNodeLeave(node *gossip.GossipNode) {
	log.Printf("Node left: %s (%s)", shortID(node.ID), node.Addr())
}

func (c *Cluster) onSlotChange(slot uint16, nodeID string) {
	log.Printf("Slot %d assigned to %s", slot, shortID(nodeID))
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
		Load: NodeLoadInfo{
			TotalQPS:     gn.Load.TotalQPS,
			HotSlotCount: gn.Load.HotSlotCount,
		},
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
