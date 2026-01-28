package gossip

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"
)

const (
	ClusterNodeTimeout        = 15 * time.Second
	ClusterPingInterval       = time.Second
	ClusterFailReportValidity = 30 * time.Second
	GossipCount               = 3
)

type NodeState int

const (
	NodeStateUnknown NodeState = iota
	NodeStateHandshake
	NodeStateConnected
	NodeStatePFail
	NodeStateFail
)

type NodeRole int

const (
	NodeRoleMaster NodeRole = iota
	NodeRoleReplica
)

type GossipNode struct {
	ID           string
	IP           string
	Port         int
	ClusterPort  int
	Role         NodeRole
	MasterID     string
	State        NodeState
	PingSent     int64
	PongReceived int64
	FailReports  map[string]int64
	mu           sync.RWMutex
}

func (n *GossipNode) Addr() string {
	return fmt.Sprintf("%s:%d", n.IP, n.Port)
}

func (n *GossipNode) ClusterAddr() string {
	return fmt.Sprintf("%s:%d", n.IP, n.ClusterPort)
}

func (n *GossipNode) UpdatePong() {
	n.mu.Lock()
	n.PongReceived = time.Now().UnixMilli()
	n.State = NodeStateConnected
	n.mu.Unlock()
}

func (n *GossipNode) MarkPFail() {
	n.mu.Lock()
	if n.State == NodeStateConnected {
		n.State = NodeStatePFail
	}
	n.mu.Unlock()
}

func (n *GossipNode) MarkFail() {
	n.mu.Lock()
	n.State = NodeStateFail
	n.mu.Unlock()
}

func (n *GossipNode) CountFailReports(validDuration time.Duration) int {
	n.mu.RLock()
	defer n.mu.RUnlock()

	now := time.Now().UnixMilli()
	count := 0
	for _, ts := range n.FailReports {
		if now-ts < validDuration.Milliseconds() {
			count++
		}
	}
	return count
}

func (n *GossipNode) Clone() *GossipNode {
	n.mu.RLock()
	defer n.mu.RUnlock()

	failReports := make(map[string]int64, len(n.FailReports))
	for k, v := range n.FailReports {
		failReports[k] = v
	}

	return &GossipNode{
		ID:           n.ID,
		IP:           n.IP,
		Port:         n.Port,
		ClusterPort:  n.ClusterPort,
		Role:         n.Role,
		MasterID:     n.MasterID,
		State:        n.State,
		PingSent:     n.PingSent,
		PongReceived: n.PongReceived,
		FailReports:  failReports,
	}
}

type SlotAssigner interface {
	GetNodeSlots(nodeID string) []uint16
	GetSlotNode(slot uint16) string
	AssignSlot(slot uint16, nodeID string) error
}

type Gossip struct {
	self  *GossipNode
	slots SlotAssigner

	nodes   map[string]*GossipNode
	nodesMu sync.RWMutex

	currentEpoch uint64
	listener     net.Listener

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	onNodeJoin   func(node *GossipNode)
	onNodeLeave  func(node *GossipNode)
	onSlotChange func(slot uint16, nodeID string)
}

func NewGossip(self *GossipNode, slots SlotAssigner) *Gossip {
	ctx, cancel := context.WithCancel(context.Background())

	g := &Gossip{
		self:   self,
		nodes:  make(map[string]*GossipNode),
		slots:  slots,
		ctx:    ctx,
		cancel: cancel,
	}

	g.nodes[self.ID] = self
	return g
}

func (g *Gossip) Start() error {
	addr := g.self.ClusterAddr()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	g.listener = listener

	log.Printf("Gossip listening on %s", addr)

	g.wg.Add(1)
	go g.acceptLoop()

	g.wg.Add(1)
	go g.pingLoop()

	g.wg.Add(1)
	go g.failureDetectionLoop()

	return nil
}

func (g *Gossip) Stop() error {
	g.cancel()
	if g.listener != nil {
		g.listener.Close()
	}
	g.wg.Wait()
	return nil
}

func (g *Gossip) Meet(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}

	msg := NewMeetMessage(g.self.ID, g.selfNodeInfo())
	data, err := msg.Encode()
	if err != nil {
		conn.Close()
		return err
	}

	if err := g.writeMessage(conn, data); err != nil {
		conn.Close()
		return err
	}

	respData, err := g.readMessage(conn)
	if err != nil {
		conn.Close()
		return err
	}

	resp, err := Decode(respData)
	if err != nil {
		conn.Close()
		return err
	}

	if resp.Type == MsgPong && resp.NodeInfo != nil {
		g.processNodeInfo(resp.NodeInfo)
	}

	conn.Close()
	log.Printf("Successfully met node at %s", addr)
	return nil
}

func (g *Gossip) acceptLoop() {
	defer g.wg.Done()

	for {
		conn, err := g.listener.Accept()
		if err != nil {
			select {
			case <-g.ctx.Done():
				return
			default:
				log.Printf("Accept error: %v", err)
				continue
			}
		}
		go g.handleConnection(conn)
	}
}

func (g *Gossip) handleConnection(conn net.Conn) {
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(ClusterNodeTimeout))

	data, err := g.readMessage(conn)
	if err != nil {
		return
	}

	msg, err := Decode(data)
	if err != nil {
		return
	}

	switch msg.Type {
	case MsgPing, MsgMeet:
		g.handlePing(conn, msg)
	case MsgPong:
		g.handlePong(msg)
	case MsgFail:
		g.handleFail(msg)
	}
}

func (g *Gossip) handlePing(conn net.Conn, msg *Message) {
	if msg.NodeInfo != nil {
		g.processNodeInfo(msg.NodeInfo)
	}

	for _, info := range msg.GossipNodes {
		g.processNodeInfo(info)
	}

	pong := NewPongMessage(g.self.ID, g.selfNodeInfo(), g.randomGossipNodes())
	data, err := pong.Encode()
	if err != nil {
		return
	}

	g.writeMessage(conn, data)
}

func (g *Gossip) handlePong(msg *Message) {
	g.nodesMu.Lock()
	if node, ok := g.nodes[msg.Sender]; ok {
		node.UpdatePong()
	}
	g.nodesMu.Unlock()

	if msg.NodeInfo != nil {
		g.processNodeInfo(msg.NodeInfo)
	}

	for _, info := range msg.GossipNodes {
		g.processNodeInfo(info)
	}
}

func (g *Gossip) handleFail(msg *Message) {
	g.nodesMu.Lock()
	defer g.nodesMu.Unlock()

	if node, ok := g.nodes[msg.FailNodeID]; ok {
		node.MarkFail()
		log.Printf("Node %s marked as FAIL by %s", msg.FailNodeID, msg.Sender)

		if g.onNodeLeave != nil {
			go g.onNodeLeave(node)
		}
	}
}

func (g *Gossip) processNodeInfo(info *NodeInfo) {
	g.nodesMu.Lock()
	defer g.nodesMu.Unlock()

	node, exists := g.nodes[info.ID]
	if !exists {
		node = &GossipNode{
			ID:          info.ID,
			IP:          info.IP,
			Port:        info.Port,
			ClusterPort: info.ClusterPort,
			State:       NodeStateConnected,
			FailReports: make(map[string]int64),
		}
		g.nodes[info.ID] = node

		log.Printf("Discovered new node: %s (%s:%d)", info.ID[:8], info.IP, info.Port)

		if g.onNodeJoin != nil {
			go g.onNodeJoin(node)
		}
	}

	node.mu.Lock()
	if info.Flags&NodeFlagMaster != 0 {
		node.Role = NodeRoleMaster
	} else if info.Flags&NodeFlagReplica != 0 {
		node.Role = NodeRoleReplica
		node.MasterID = info.MasterID
	}
	isMaster := node.Role == NodeRoleMaster
	node.mu.Unlock()

	if len(info.Slots) > 0 && isMaster {
		slots := BytesToSlots(info.Slots)
		for _, slot := range slots {
			currentOwner := g.slots.GetSlotNode(slot)
			if currentOwner != node.ID {
				g.slots.AssignSlot(slot, node.ID)
				if g.onSlotChange != nil {
					go g.onSlotChange(slot, node.ID)
				}
			}
		}
	}
}

func (g *Gossip) pingLoop() {
	defer g.wg.Done()

	ticker := time.NewTicker(ClusterPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.pingRandomNode()
		}
	}
}

func (g *Gossip) pingRandomNode() {
	g.nodesMu.RLock()
	var candidates []*GossipNode
	now := time.Now().UnixMilli()

	for _, node := range g.nodes {
		if node.ID == g.self.ID {
			continue
		}
		node.mu.RLock()
		pingSent := node.PingSent
		node.mu.RUnlock()
		if now-pingSent > ClusterPingInterval.Milliseconds() {
			candidates = append(candidates, node)
		}
	}
	g.nodesMu.RUnlock()

	if len(candidates) == 0 {
		return
	}

	node := candidates[rand.Intn(len(candidates))]
	g.pingNode(node)
}

func (g *Gossip) pingNode(node *GossipNode) {
	conn, err := net.DialTimeout("tcp", node.ClusterAddr(), 2*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()

	node.mu.Lock()
	node.PingSent = time.Now().UnixMilli()
	node.mu.Unlock()

	msg := NewPingMessage(g.self.ID, g.selfNodeInfo(), g.randomGossipNodes())
	data, err := msg.Encode()
	if err != nil {
		return
	}

	if err := g.writeMessage(conn, data); err != nil {
		return
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	respData, err := g.readMessage(conn)
	if err != nil {
		return
	}

	resp, err := Decode(respData)
	if err != nil {
		return
	}

	if resp.Type == MsgPong {
		g.handlePong(resp)
	}
}

func (g *Gossip) failureDetectionLoop() {
	defer g.wg.Done()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.checkNodeFailures()
		}
	}
}

func (g *Gossip) checkNodeFailures() {
	now := time.Now().UnixMilli()
	timeout := ClusterNodeTimeout.Milliseconds()

	g.nodesMu.Lock()
	defer g.nodesMu.Unlock()

	for _, node := range g.nodes {
		if node.ID == g.self.ID {
			continue
		}

		node.mu.RLock()
		state := node.State
		pongReceived := node.PongReceived
		node.mu.RUnlock()

		if state == NodeStateConnected {
			if now-pongReceived > timeout {
				node.MarkPFail()
				log.Printf("Node %s marked as PFAIL", node.ID[:8])
			}
		}

		// Re-read state after potential MarkPFail
		node.mu.RLock()
		state = node.State
		node.mu.RUnlock()

		if state == NodeStatePFail {
			masterCount := g.countMasters()
			failReports := node.CountFailReports(ClusterFailReportValidity)

			if failReports >= (masterCount/2 + 1) {
				node.MarkFail()
				log.Printf("Node %s marked as FAIL (reports: %d/%d)",
					node.ID[:8], failReports, masterCount)

				go g.broadcastFail(node.ID)

				if g.onNodeLeave != nil {
					go g.onNodeLeave(node)
				}
			}
		}
	}
}

func (g *Gossip) broadcastFail(failNodeID string) {
	g.nodesMu.RLock()
	nodes := make([]*GossipNode, 0, len(g.nodes))
	for _, node := range g.nodes {
		if node.ID != g.self.ID {
			node.mu.RLock()
			state := node.State
			node.mu.RUnlock()
			if state == NodeStateConnected {
				nodes = append(nodes, node)
			}
		}
	}
	g.nodesMu.RUnlock()

	msg := NewFailMessage(g.self.ID, failNodeID)
	data, _ := msg.Encode()

	for _, node := range nodes {
		go func(n *GossipNode) {
			conn, err := net.DialTimeout("tcp", n.ClusterAddr(), 2*time.Second)
			if err != nil {
				return
			}
			defer conn.Close()
			g.writeMessage(conn, data)
		}(node)
	}
}

func (g *Gossip) selfNodeInfo() *NodeInfo {
	var flags uint16
	if g.self.Role == NodeRoleMaster {
		flags |= NodeFlagMaster
	} else {
		flags |= NodeFlagReplica
	}

	slots := g.slots.GetNodeSlots(g.self.ID)

	return &NodeInfo{
		ID:          g.self.ID,
		IP:          g.self.IP,
		Port:        g.self.Port,
		ClusterPort: g.self.ClusterPort,
		Flags:       flags,
		MasterID:    g.self.MasterID,
		ConfigEpoch: g.currentEpoch,
		Slots:       SlotsToBytes(slots),
	}
}

func (g *Gossip) randomGossipNodes() []*NodeInfo {
	g.nodesMu.RLock()
	defer g.nodesMu.RUnlock()

	var nodes []*NodeInfo
	for _, node := range g.nodes {
		if node.ID == g.self.ID {
			continue
		}

		node.mu.RLock()
		var flags uint16
		if node.Role == NodeRoleMaster {
			flags |= NodeFlagMaster
		} else {
			flags |= NodeFlagReplica
		}
		if node.State == NodeStatePFail {
			flags |= NodeFlagPFail
		}
		if node.State == NodeStateFail {
			flags |= NodeFlagFail
		}

		nodes = append(nodes, &NodeInfo{
			ID:          node.ID,
			IP:          node.IP,
			Port:        node.Port,
			ClusterPort: node.ClusterPort,
			Flags:       flags,
			MasterID:    node.MasterID,
			PingSent:    node.PingSent,
			PongRecv:    node.PongReceived,
		})
		node.mu.RUnlock()
	}

	if len(nodes) > GossipCount {
		rand.Shuffle(len(nodes), func(i, j int) {
			nodes[i], nodes[j] = nodes[j], nodes[i]
		})
		nodes = nodes[:GossipCount]
	}

	return nodes
}

func (g *Gossip) countMasters() int {
	count := 0
	for _, node := range g.nodes {
		node.mu.RLock()
		isMaster := node.Role == NodeRoleMaster && node.State == NodeStateConnected
		node.mu.RUnlock()
		if isMaster {
			count++
		}
	}
	return count
}

func (g *Gossip) GetNodes() []*GossipNode {
	g.nodesMu.RLock()
	defer g.nodesMu.RUnlock()

	nodes := make([]*GossipNode, 0, len(g.nodes))
	for _, node := range g.nodes {
		nodes = append(nodes, node.Clone())
	}
	return nodes
}

func (g *Gossip) GetNode(id string) *GossipNode {
	g.nodesMu.RLock()
	defer g.nodesMu.RUnlock()

	if node, ok := g.nodes[id]; ok {
		return node.Clone()
	}
	return nil
}

func (g *Gossip) SetEventHandlers(onJoin, onLeave func(*GossipNode), onSlotChange func(uint16, string)) {
	g.onNodeJoin = onJoin
	g.onNodeLeave = onLeave
	g.onSlotChange = onSlotChange
}

func (g *Gossip) writeMessage(conn net.Conn, data []byte) error {
	length := uint32(len(data))
	buf := make([]byte, 4+len(data))
	buf[0] = byte(length >> 24)
	buf[1] = byte(length >> 16)
	buf[2] = byte(length >> 8)
	buf[3] = byte(length)
	copy(buf[4:], data)

	_, err := conn.Write(buf)
	return err
}

func (g *Gossip) readMessage(conn net.Conn) ([]byte, error) {
	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lengthBuf); err != nil {
		return nil, err
	}

	length := uint32(lengthBuf[0])<<24 | uint32(lengthBuf[1])<<16 |
		uint32(lengthBuf[2])<<8 | uint32(lengthBuf[3])

	if length > 1024*1024 {
		return nil, fmt.Errorf("message too large: %d", length)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, err
	}

	return data, nil
}
