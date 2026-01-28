package cluster

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"
)

type NodeState int

const (
	NodeStateUnknown NodeState = iota
	NodeStateHandshake
	NodeStateConnected
	NodeStatePFail
	NodeStateFail
)

func (s NodeState) String() string {
	switch s {
	case NodeStateUnknown:
		return "unknown"
	case NodeStateHandshake:
		return "handshake"
	case NodeStateConnected:
		return "connected"
	case NodeStatePFail:
		return "pfail"
	case NodeStateFail:
		return "fail"
	default:
		return "unknown"
	}
}

type NodeRole int

const (
	NodeRoleMaster NodeRole = iota
	NodeRoleReplica
)

func (r NodeRole) String() string {
	if r == NodeRoleMaster {
		return "master"
	}
	return "replica"
}

type Node struct {
	ID          string
	IP          string
	Port        int
	ClusterPort int

	Role     NodeRole
	MasterID string

	State NodeState

	PingSent     int64
	PongReceived int64

	FailReports map[string]int64

	conn net.Conn
	mu   sync.RWMutex
}

func NewNode(ip string, port int) *Node {
	return &Node{
		ID:          generateNodeID(),
		IP:          ip,
		Port:        port,
		ClusterPort: port + 10000,
		Role:        NodeRoleMaster,
		State:       NodeStateUnknown,
		FailReports: make(map[string]int64),
	}
}

func NodeFromAddr(addr string, port int) *Node {
	return &Node{
		ID:          generateNodeID(),
		IP:          addr,
		Port:        port,
		ClusterPort: port + 10000,
		Role:        NodeRoleMaster,
		State:       NodeStateUnknown,
		FailReports: make(map[string]int64),
	}
}

func generateNodeID() string {
	b := make([]byte, 20)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (n *Node) Addr() string {
	return fmt.Sprintf("%s:%d", n.IP, n.Port)
}

func (n *Node) ClusterAddr() string {
	return fmt.Sprintf("%s:%d", n.IP, n.ClusterPort)
}

func (n *Node) SetState(state NodeState) {
	n.mu.Lock()
	n.State = state
	n.mu.Unlock()
}

func (n *Node) GetState() NodeState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.State
}

func (n *Node) UpdatePong() {
	n.mu.Lock()
	n.PongReceived = time.Now().UnixMilli()
	n.State = NodeStateConnected
	n.mu.Unlock()
}

func (n *Node) MarkPFail() {
	n.mu.Lock()
	if n.State == NodeStateConnected {
		n.State = NodeStatePFail
	}
	n.mu.Unlock()
}

func (n *Node) MarkFail() {
	n.mu.Lock()
	n.State = NodeStateFail
	n.mu.Unlock()
}

func (n *Node) AddFailReport(reporterID string) {
	n.mu.Lock()
	n.FailReports[reporterID] = time.Now().UnixMilli()
	n.mu.Unlock()
}

func (n *Node) CountFailReports(validDuration time.Duration) int {
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

func (n *Node) IsMaster() bool {
	return n.Role == NodeRoleMaster
}

func (n *Node) IsReplica() bool {
	return n.Role == NodeRoleReplica
}

func (n *Node) Clone() *Node {
	n.mu.RLock()
	defer n.mu.RUnlock()

	failReports := make(map[string]int64, len(n.FailReports))
	for k, v := range n.FailReports {
		failReports[k] = v
	}

	return &Node{
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
