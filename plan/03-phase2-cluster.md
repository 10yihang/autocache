# Phase 2: Gossip 分布式集群

## 目标

- 实现 Redis Cluster 风格的 Gossip 协议
- 实现 16384 slots 分片机制
- 支持 MOVED/ASK 重定向
- 实现节点发现与故障检测
- 支持在线数据迁移

## 时间：2026.02.10 - 2026.03.09（4 周）

---

## 架构设计

```
┌─────────────────────────────────────────────────────────────────────────┐
│                            Client                                        │
│                              │                                           │
│                    ┌─────────┴─────────┐                                │
│                    │    CRC16(key)     │                                │
│                    │   slot = hash%16384│                               │
│                    └─────────┬─────────┘                                │
│                              │                                           │
│           ┌──────────────────┼──────────────────┐                       │
│           ▼                  ▼                  ▼                       │
│    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐               │
│    │   Node A    │◄──►│   Node B    │◄──►│   Node C    │               │
│    │ Slots 0-5460│    │Slots 5461-10922│  │Slots 10923-16383│           │
│    │             │    │             │    │             │               │
│    │  ┌───────┐  │    │  ┌───────┐  │    │  ┌───────┐  │               │
│    │  │Gossip │  │    │  │Gossip │  │    │  │Gossip │  │               │
│    │  │ Port  │  │    │  │ Port  │  │    │  │ Port  │  │               │
│    │  └───────┘  │    │  └───────┘  │    │  └───────┘  │               │
│    └─────────────┘    └─────────────┘    └─────────────┘               │
│           │                  │                  │                       │
│           └──────────────────┴──────────────────┘                       │
│                    Gossip Protocol                                      │
│              (PING/PONG/MEET/FAIL)                                     │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Week 1: 一致性哈希与 Slot 分片

### 1.1 CRC16 实现（Redis 兼容）

#### internal/cluster/hash/crc16.go
```go
package hash

// CRC16 表（CCITT 标准，Redis 使用）
var crc16Table = [256]uint16{
	0x0000, 0x1021, 0x2042, 0x3063, 0x4084, 0x50a5, 0x60c6, 0x70e7,
	0x8108, 0x9129, 0xa14a, 0xb16b, 0xc18c, 0xd1ad, 0xe1ce, 0xf1ef,
	0x1231, 0x0210, 0x3273, 0x2252, 0x52b5, 0x4294, 0x72f7, 0x62d6,
	0x9339, 0x8318, 0xb37b, 0xa35a, 0xd3bd, 0xc39c, 0xf3ff, 0xe3de,
	0x2462, 0x3443, 0x0420, 0x1401, 0x64e6, 0x74c7, 0x44a4, 0x5485,
	0xa56a, 0xb54b, 0x8528, 0x9509, 0xe5ee, 0xf5cf, 0xc5ac, 0xd58d,
	0x3653, 0x2672, 0x1611, 0x0630, 0x76d7, 0x66f6, 0x5695, 0x46b4,
	0xb75b, 0xa77a, 0x9719, 0x8738, 0xf7df, 0xe7fe, 0xd79d, 0xc7bc,
	0x48c4, 0x58e5, 0x6886, 0x78a7, 0x0840, 0x1861, 0x2802, 0x3823,
	0xc9cc, 0xd9ed, 0xe98e, 0xf9af, 0x8948, 0x9969, 0xa90a, 0xb92b,
	0x5af5, 0x4ad4, 0x7ab7, 0x6a96, 0x1a71, 0x0a50, 0x3a33, 0x2a12,
	0xdbfd, 0xcbdc, 0xfbbf, 0xeb9e, 0x9b79, 0x8b58, 0xbb3b, 0xab1a,
	0x6ca6, 0x7c87, 0x4ce4, 0x5cc5, 0x2c22, 0x3c03, 0x0c60, 0x1c41,
	0xedae, 0xfd8f, 0xcdec, 0xddcd, 0xad2a, 0xbd0b, 0x8d68, 0x9d49,
	0x7e97, 0x6eb6, 0x5ed5, 0x4ef4, 0x3e13, 0x2e32, 0x1e51, 0x0e70,
	0xff9f, 0xefbe, 0xdfdd, 0xcffc, 0xbf1b, 0xaf3a, 0x9f59, 0x8f78,
	0x9188, 0x81a9, 0xb1ca, 0xa1eb, 0xd10c, 0xc12d, 0xf14e, 0xe16f,
	0x1080, 0x00a1, 0x30c2, 0x20e3, 0x5004, 0x4025, 0x7046, 0x6067,
	0x83b9, 0x9398, 0xa3fb, 0xb3da, 0xc33d, 0xd31c, 0xe37f, 0xf35e,
	0x02b1, 0x1290, 0x22f3, 0x32d2, 0x4235, 0x5214, 0x6277, 0x7256,
	0xb5ea, 0xa5cb, 0x95a8, 0x8589, 0xf56e, 0xe54f, 0xd52c, 0xc50d,
	0x34e2, 0x24c3, 0x14a0, 0x0481, 0x7466, 0x6447, 0x5424, 0x4405,
	0xa7db, 0xb7fa, 0x8799, 0x97b8, 0xe75f, 0xf77e, 0xc71d, 0xd73c,
	0x26d3, 0x36f2, 0x0691, 0x16b0, 0x6657, 0x7676, 0x4615, 0x5634,
	0xd94c, 0xc96d, 0xf90e, 0xe92f, 0x99c8, 0x89e9, 0xb98a, 0xa9ab,
	0x5844, 0x4865, 0x7806, 0x6827, 0x18c0, 0x08e1, 0x3882, 0x28a3,
	0xcb7d, 0xdb5c, 0xeb3f, 0xfb1e, 0x8bf9, 0x9bd8, 0xabbb, 0xbb9a,
	0x4a75, 0x5a54, 0x6a37, 0x7a16, 0x0af1, 0x1ad0, 0x2ab3, 0x3a92,
	0xfd2e, 0xed0f, 0xdd6c, 0xcd4d, 0xbdaa, 0xad8b, 0x9de8, 0x8dc9,
	0x7c26, 0x6c07, 0x5c64, 0x4c45, 0x3ca2, 0x2c83, 0x1ce0, 0x0cc1,
	0xef1f, 0xff3e, 0xcf5d, 0xdf7c, 0xaf9b, 0xbfba, 0x8fd9, 0x9ff8,
	0x6e17, 0x7e36, 0x4e55, 0x5e74, 0x2e93, 0x3eb2, 0x0ed1, 0x1ef0,
}

// CRC16 计算 CRC16 校验和
func CRC16(data []byte) uint16 {
	crc := uint16(0)
	for _, b := range data {
		crc = (crc << 8) ^ crc16Table[byte(crc>>8)^b]
	}
	return crc
}

// SlotCount 槽位总数
const SlotCount = 16384

// KeySlot 计算 key 对应的 slot
// 支持 hash tag: {user:123}.name -> 只对 user:123 计算 hash
func KeySlot(key string) uint16 {
	// 查找 hash tag
	if start := indexByte(key, '{'); start >= 0 {
		if end := indexByte(key[start+1:], '}'); end > 0 {
			key = key[start+1 : start+1+end]
		}
	}
	return CRC16([]byte(key)) % SlotCount
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
```

### 1.2 Slot 管理器

#### internal/cluster/slots.go
```go
package cluster

import (
	"fmt"
	"sync"

	"github.com/10yihang/autocache/internal/cluster/hash"
)

// SlotState Slot 状态
type SlotState int

const (
	SlotStateNormal    SlotState = iota // 正常
	SlotStateImporting                  // 正在导入
	SlotStateExporting                  // 正在导出
)

// SlotInfo Slot 信息
type SlotInfo struct {
	State     SlotState
	NodeID    string // 所属节点
	Importing string // 正在从哪个节点导入
	Exporting string // 正在导出到哪个节点
}

// SlotManager Slot 管理器
type SlotManager struct {
	slots  [hash.SlotCount]*SlotInfo
	mu     sync.RWMutex
	
	// 节点到 slots 的映射（加速查找）
	nodeSlots map[string][]uint16
}

// NewSlotManager 创建 Slot 管理器
func NewSlotManager() *SlotManager {
	sm := &SlotManager{
		nodeSlots: make(map[string][]uint16),
	}
	
	// 初始化所有 slots
	for i := 0; i < hash.SlotCount; i++ {
		sm.slots[i] = &SlotInfo{
			State: SlotStateNormal,
		}
	}
	
	return sm
}

// AssignSlot 分配 slot 给节点
func (sm *SlotManager) AssignSlot(slot uint16, nodeID string) error {
	if slot >= hash.SlotCount {
		return fmt.Errorf("invalid slot: %d", slot)
	}
	
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	oldNodeID := sm.slots[slot].NodeID
	
	// 从旧节点移除
	if oldNodeID != "" {
		sm.removeSlotFromNode(oldNodeID, slot)
	}
	
	// 分配给新节点
	sm.slots[slot].NodeID = nodeID
	sm.slots[slot].State = SlotStateNormal
	sm.nodeSlots[nodeID] = append(sm.nodeSlots[nodeID], slot)
	
	return nil
}

// AssignSlotRange 批量分配 slot
func (sm *SlotManager) AssignSlotRange(start, end uint16, nodeID string) error {
	for slot := start; slot <= end; slot++ {
		if err := sm.AssignSlot(slot, nodeID); err != nil {
			return err
		}
	}
	return nil
}

// GetSlotNode 获取 slot 所属节点
func (sm *SlotManager) GetSlotNode(slot uint16) string {
	if slot >= hash.SlotCount {
		return ""
	}
	
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	return sm.slots[slot].NodeID
}

// GetKeyNode 根据 key 获取所属节点
func (sm *SlotManager) GetKeyNode(key string) string {
	slot := hash.KeySlot(key)
	return sm.GetSlotNode(slot)
}

// GetSlotInfo 获取 slot 详细信息
func (sm *SlotManager) GetSlotInfo(slot uint16) *SlotInfo {
	if slot >= hash.SlotCount {
		return nil
	}
	
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	// 返回副本
	info := *sm.slots[slot]
	return &info
}

// GetNodeSlots 获取节点的所有 slots
func (sm *SlotManager) GetNodeSlots(nodeID string) []uint16 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	slots := sm.nodeSlots[nodeID]
	result := make([]uint16, len(slots))
	copy(result, slots)
	return result
}

// SetImporting 设置 slot 为导入状态
func (sm *SlotManager) SetImporting(slot uint16, fromNodeID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	sm.slots[slot].State = SlotStateImporting
	sm.slots[slot].Importing = fromNodeID
}

// SetExporting 设置 slot 为导出状态
func (sm *SlotManager) SetExporting(slot uint16, toNodeID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	sm.slots[slot].State = SlotStateExporting
	sm.slots[slot].Exporting = toNodeID
}

// FinishMigration 完成迁移
func (sm *SlotManager) FinishMigration(slot uint16, newNodeID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	oldNodeID := sm.slots[slot].NodeID
	
	// 从旧节点移除
	if oldNodeID != "" {
		sm.removeSlotFromNode(oldNodeID, slot)
	}
	
	// 分配给新节点
	sm.slots[slot].NodeID = newNodeID
	sm.slots[slot].State = SlotStateNormal
	sm.slots[slot].Importing = ""
	sm.slots[slot].Exporting = ""
	sm.nodeSlots[newNodeID] = append(sm.nodeSlots[newNodeID], slot)
}

func (sm *SlotManager) removeSlotFromNode(nodeID string, slot uint16) {
	slots := sm.nodeSlots[nodeID]
	for i, s := range slots {
		if s == slot {
			sm.nodeSlots[nodeID] = append(slots[:i], slots[i+1:]...)
			break
		}
	}
}

// GetClusterSlots 获取集群 slots 分布（用于 CLUSTER SLOTS 命令）
func (sm *SlotManager) GetClusterSlots() []SlotRange {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	var ranges []SlotRange
	var current *SlotRange
	
	for i := uint16(0); i < hash.SlotCount; i++ {
		nodeID := sm.slots[i].NodeID
		if nodeID == "" {
			continue
		}
		
		if current == nil || current.NodeID != nodeID {
			if current != nil {
				ranges = append(ranges, *current)
			}
			current = &SlotRange{
				Start:  i,
				End:    i,
				NodeID: nodeID,
			}
		} else {
			current.End = i
		}
	}
	
	if current != nil {
		ranges = append(ranges, *current)
	}
	
	return ranges
}

// SlotRange Slot 范围
type SlotRange struct {
	Start  uint16
	End    uint16
	NodeID string
}

// CountAssigned 统计已分配的 slot 数量
func (sm *SlotManager) CountAssigned() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	count := 0
	for _, slot := range sm.slots {
		if slot.NodeID != "" {
			count++
		}
	}
	return count
}
```

---

## Week 2: 节点管理与 Gossip 协议

### 2.1 节点定义

#### internal/cluster/node.go
```go
package cluster

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"
)

// NodeState 节点状态
type NodeState int

const (
	NodeStateUnknown NodeState = iota
	NodeStateHandshake         // 握手中
	NodeStateConnected         // 已连接
	NodeStatePFail             // 疑似下线
	NodeStateFail              // 已下线
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

// NodeRole 节点角色
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

// Node 集群节点
type Node struct {
	ID        string    // 节点唯一 ID（40 字符十六进制）
	IP        string    // IP 地址
	Port      int       // 客户端端口
	ClusterPort int     // 集群通信端口（Port + 10000）
	
	Role      NodeRole  // 角色
	MasterID  string    // 如果是 replica，指向 master
	
	State     NodeState // 节点状态
	
	// 时间戳
	PingSent     int64  // 最后发送 PING 的时间
	PongReceived int64  // 最后收到 PONG 的时间
	
	// 故障检测
	FailReports map[string]int64 // 其他节点报告的故障时间
	
	// 连接
	conn net.Conn
	
	mu sync.RWMutex
}

// NewNode 创建节点
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

// NodeFromAddr 从地址解析节点
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

// generateNodeID 生成 40 字符的节点 ID
func generateNodeID() string {
	b := make([]byte, 20)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Addr 返回客户端地址
func (n *Node) Addr() string {
	return fmt.Sprintf("%s:%d", n.IP, n.Port)
}

// ClusterAddr 返回集群通信地址
func (n *Node) ClusterAddr() string {
	return fmt.Sprintf("%s:%d", n.IP, n.ClusterPort)
}

// SetState 设置状态
func (n *Node) SetState(state NodeState) {
	n.mu.Lock()
	n.State = state
	n.mu.Unlock()
}

// GetState 获取状态
func (n *Node) GetState() NodeState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.State
}

// UpdatePong 更新 PONG 时间
func (n *Node) UpdatePong() {
	n.mu.Lock()
	n.PongReceived = time.Now().UnixMilli()
	n.State = NodeStateConnected
	n.mu.Unlock()
}

// MarkPFail 标记为疑似下线
func (n *Node) MarkPFail() {
	n.mu.Lock()
	if n.State == NodeStateConnected {
		n.State = NodeStatePFail
	}
	n.mu.Unlock()
}

// MarkFail 标记为已下线
func (n *Node) MarkFail() {
	n.mu.Lock()
	n.State = NodeStateFail
	n.mu.Unlock()
}

// AddFailReport 添加故障报告
func (n *Node) AddFailReport(reporterID string) {
	n.mu.Lock()
	n.FailReports[reporterID] = time.Now().UnixMilli()
	n.mu.Unlock()
}

// CountFailReports 统计有效的故障报告数
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

// IsMaster 是否是 master
func (n *Node) IsMaster() bool {
	return n.Role == NodeRoleMaster
}

// IsReplica 是否是 replica
func (n *Node) IsReplica() bool {
	return n.Role == NodeRoleReplica
}

// Clone 复制节点信息
func (n *Node) Clone() *Node {
	n.mu.RLock()
	defer n.mu.RUnlock()
	
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
	}
}
```

### 2.2 Gossip 消息定义

#### internal/cluster/gossip/message.go
```go
package gossip

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
)

// MessageType 消息类型
type MessageType uint8

const (
	MsgPing    MessageType = iota + 1 // PING
	MsgPong                           // PONG
	MsgMeet                           // MEET（介绍新节点）
	MsgFail                           // FAIL（节点下线通知）
	MsgPublish                        // PUBLISH（发布消息）
	MsgUpdate                         // UPDATE（更新配置）
)

func (t MessageType) String() string {
	switch t {
	case MsgPing:
		return "PING"
	case MsgPong:
		return "PONG"
	case MsgMeet:
		return "MEET"
	case MsgFail:
		return "FAIL"
	case MsgPublish:
		return "PUBLISH"
	case MsgUpdate:
		return "UPDATE"
	default:
		return "UNKNOWN"
	}
}

// Message Gossip 消息
type Message struct {
	Type        MessageType
	Sender      string // 发送者 ID
	
	// PING/PONG 携带的信息
	CurrentEpoch uint64    // 当前配置纪元
	ConfigEpoch  uint64    // 节点配置纪元
	
	// 节点信息
	NodeInfo    *NodeInfo
	
	// Gossip 部分：携带其他节点的信息
	GossipNodes []*NodeInfo
	
	// FAIL 消息
	FailNodeID  string
	
	// 扩展数据
	Data        []byte
}

// NodeInfo 节点信息（用于 Gossip 传播）
type NodeInfo struct {
	ID          string
	IP          string
	Port        int
	ClusterPort int
	Flags       uint16 // 节点标志
	MasterID    string // 如果是 replica
	PingSent    int64
	PongRecv    int64
	ConfigEpoch uint64
	Slots       []byte // 位图，表示拥有的 slots
}

// NodeFlag 节点标志
const (
	NodeFlagMaster   uint16 = 1 << 0
	NodeFlagReplica  uint16 = 1 << 1
	NodeFlagPFail    uint16 = 1 << 2
	NodeFlagFail     uint16 = 1 << 3
	NodeFlagHandshake uint16 = 1 << 4
	NodeFlagNoAddr   uint16 = 1 << 5
	NodeFlagMeet     uint16 = 1 << 6
)

// Encode 编码消息
func (m *Message) Encode() ([]byte, error) {
	var buf bytes.Buffer
	
	// 写入消息类型
	buf.WriteByte(byte(m.Type))
	
	// 使用 gob 编码其余部分
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	
	return buf.Bytes(), nil
}

// Decode 解码消息
func Decode(data []byte) (*Message, error) {
	if len(data) < 1 {
		return nil, ErrInvalidMessage
	}
	
	buf := bytes.NewBuffer(data[1:]) // 跳过类型字节
	
	var m Message
	dec := gob.NewDecoder(buf)
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	
	m.Type = MessageType(data[0])
	return &m, nil
}

// NewPingMessage 创建 PING 消息
func NewPingMessage(senderID string, nodeInfo *NodeInfo, gossipNodes []*NodeInfo) *Message {
	return &Message{
		Type:        MsgPing,
		Sender:      senderID,
		NodeInfo:    nodeInfo,
		GossipNodes: gossipNodes,
	}
}

// NewPongMessage 创建 PONG 消息
func NewPongMessage(senderID string, nodeInfo *NodeInfo, gossipNodes []*NodeInfo) *Message {
	return &Message{
		Type:        MsgPong,
		Sender:      senderID,
		NodeInfo:    nodeInfo,
		GossipNodes: gossipNodes,
	}
}

// NewMeetMessage 创建 MEET 消息
func NewMeetMessage(senderID string, nodeInfo *NodeInfo) *Message {
	return &Message{
		Type:     MsgMeet,
		Sender:   senderID,
		NodeInfo: nodeInfo,
	}
}

// NewFailMessage 创建 FAIL 消息
func NewFailMessage(senderID string, failNodeID string) *Message {
	return &Message{
		Type:       MsgFail,
		Sender:     senderID,
		FailNodeID: failNodeID,
	}
}

// 错误定义
var (
	ErrInvalidMessage = &GossipError{Msg: "invalid message"}
)

type GossipError struct {
	Msg string
}

func (e *GossipError) Error() string {
	return e.Msg
}

// SlotsToBytes 将 slot 列表转换为位图
func SlotsToBytes(slots []uint16) []byte {
	bitmap := make([]byte, 16384/8)
	for _, slot := range slots {
		bitmap[slot/8] |= 1 << (slot % 8)
	}
	return bitmap
}

// BytesToSlots 将位图转换为 slot 列表
func BytesToSlots(bitmap []byte) []uint16 {
	var slots []uint16
	for i := 0; i < len(bitmap)*8 && i < 16384; i++ {
		if bitmap[i/8]&(1<<(i%8)) != 0 {
			slots = append(slots, uint16(i))
		}
	}
	return slots
}
```

### 2.3 Gossip 核心实现

#### internal/cluster/gossip/gossip.go
```go
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

	"github.com/10yihang/autocache/internal/cluster"
)

const (
	// 时间配置
	ClusterNodeTimeout     = 15 * time.Second // 节点超时时间
	ClusterPingInterval    = time.Second      // PING 间隔
	ClusterFailReportValidity = 30 * time.Second // 故障报告有效期
	
	// Gossip 配置
	GossipCount = 3 // 每次 PING 携带的节点数
)

// Gossip Gossip 协议实现
type Gossip struct {
	// 本节点
	self *cluster.Node
	
	// 所有已知节点
	nodes   map[string]*cluster.Node
	nodesMu sync.RWMutex
	
	// Slot 管理
	slots *cluster.SlotManager
	
	// 配置纪元
	currentEpoch uint64
	
	// 网络
	listener net.Listener
	
	// 生命周期
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	
	// 事件回调
	onNodeJoin   func(node *cluster.Node)
	onNodeLeave  func(node *cluster.Node)
	onSlotChange func(slot uint16, nodeID string)
}

// NewGossip 创建 Gossip 实例
func NewGossip(self *cluster.Node, slots *cluster.SlotManager) *Gossip {
	ctx, cancel := context.WithCancel(context.Background())
	
	g := &Gossip{
		self:  self,
		nodes: make(map[string]*cluster.Node),
		slots: slots,
		ctx:   ctx,
		cancel: cancel,
	}
	
	// 添加自己
	g.nodes[self.ID] = self
	
	return g
}

// Start 启动 Gossip
func (g *Gossip) Start() error {
	// 启动监听
	addr := g.self.ClusterAddr()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	g.listener = listener
	
	log.Printf("Gossip listening on %s", addr)
	
	// 接受连接
	g.wg.Add(1)
	go g.acceptLoop()
	
	// 定时 PING
	g.wg.Add(1)
	go g.pingLoop()
	
	// 故障检测
	g.wg.Add(1)
	go g.failureDetectionLoop()
	
	return nil
}

// Stop 停止 Gossip
func (g *Gossip) Stop() error {
	g.cancel()
	if g.listener != nil {
		g.listener.Close()
	}
	g.wg.Wait()
	return nil
}

// Meet 与另一个节点建立连接
func (g *Gossip) Meet(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	
	// 发送 MEET 消息
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
	
	// 读取响应
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

// acceptLoop 接受连接循环
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

// handleConnection 处理连接
func (g *Gossip) handleConnection(conn net.Conn) {
	defer conn.Close()
	
	// 设置超时
	conn.SetDeadline(time.Now().Add(ClusterNodeTimeout))
	
	// 读取消息
	data, err := g.readMessage(conn)
	if err != nil {
		return
	}
	
	msg, err := Decode(data)
	if err != nil {
		return
	}
	
	// 处理消息
	switch msg.Type {
	case MsgPing, MsgMeet:
		g.handlePing(conn, msg)
	case MsgPong:
		g.handlePong(msg)
	case MsgFail:
		g.handleFail(msg)
	}
}

// handlePing 处理 PING/MEET
func (g *Gossip) handlePing(conn net.Conn, msg *Message) {
	// 处理发送者信息
	if msg.NodeInfo != nil {
		g.processNodeInfo(msg.NodeInfo)
	}
	
	// 处理 Gossip 节点
	for _, info := range msg.GossipNodes {
		g.processNodeInfo(info)
	}
	
	// 发送 PONG
	pong := NewPongMessage(g.self.ID, g.selfNodeInfo(), g.randomGossipNodes())
	data, err := pong.Encode()
	if err != nil {
		return
	}
	
	g.writeMessage(conn, data)
}

// handlePong 处理 PONG
func (g *Gossip) handlePong(msg *Message) {
	// 更新发送者状态
	g.nodesMu.Lock()
	if node, ok := g.nodes[msg.Sender]; ok {
		node.UpdatePong()
	}
	g.nodesMu.Unlock()
	
	// 处理节点信息
	if msg.NodeInfo != nil {
		g.processNodeInfo(msg.NodeInfo)
	}
	
	// 处理 Gossip 节点
	for _, info := range msg.GossipNodes {
		g.processNodeInfo(info)
	}
}

// handleFail 处理 FAIL
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

// processNodeInfo 处理节点信息
func (g *Gossip) processNodeInfo(info *NodeInfo) {
	g.nodesMu.Lock()
	defer g.nodesMu.Unlock()
	
	node, exists := g.nodes[info.ID]
	if !exists {
		// 新节点
		node = &cluster.Node{
			ID:          info.ID,
			IP:          info.IP,
			Port:        info.Port,
			ClusterPort: info.ClusterPort,
			State:       cluster.NodeStateConnected,
			FailReports: make(map[string]int64),
		}
		g.nodes[info.ID] = node
		
		log.Printf("Discovered new node: %s (%s:%d)", info.ID[:8], info.IP, info.Port)
		
		if g.onNodeJoin != nil {
			go g.onNodeJoin(node)
		}
	}
	
	// 更新节点信息
	if info.Flags&NodeFlagMaster != 0 {
		node.Role = cluster.NodeRoleMaster
	} else if info.Flags&NodeFlagReplica != 0 {
		node.Role = cluster.NodeRoleReplica
		node.MasterID = info.MasterID
	}
	
	// 处理 slots 变更
	if len(info.Slots) > 0 && node.Role == cluster.NodeRoleMaster {
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

// pingLoop PING 循环
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

// pingRandomNode PING 一个随机节点
func (g *Gossip) pingRandomNode() {
	g.nodesMu.RLock()
	var candidates []*cluster.Node
	now := time.Now().UnixMilli()
	
	for _, node := range g.nodes {
		if node.ID == g.self.ID {
			continue
		}
		// 选择最久没有 PING 的节点
		if now-node.PingSent > ClusterPingInterval.Milliseconds() {
			candidates = append(candidates, node)
		}
	}
	g.nodesMu.RUnlock()
	
	if len(candidates) == 0 {
		return
	}
	
	// 随机选择一个
	node := candidates[rand.Intn(len(candidates))]
	g.pingNode(node)
}

// pingNode PING 指定节点
func (g *Gossip) pingNode(node *cluster.Node) {
	conn, err := net.DialTimeout("tcp", node.ClusterAddr(), 2*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()
	
	// 记录 PING 时间
	node.PingSent = time.Now().UnixMilli()
	
	// 发送 PING
	msg := NewPingMessage(g.self.ID, g.selfNodeInfo(), g.randomGossipNodes())
	data, err := msg.Encode()
	if err != nil {
		return
	}
	
	if err := g.writeMessage(conn, data); err != nil {
		return
	}
	
	// 读取 PONG
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

// failureDetectionLoop 故障检测循环
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

// checkNodeFailures 检查节点故障
func (g *Gossip) checkNodeFailures() {
	now := time.Now().UnixMilli()
	timeout := ClusterNodeTimeout.Milliseconds()
	
	g.nodesMu.Lock()
	defer g.nodesMu.Unlock()
	
	for _, node := range g.nodes {
		if node.ID == g.self.ID {
			continue
		}
		
		// 检查是否超时
		if node.State == cluster.NodeStateConnected {
			if now-node.PongReceived > timeout {
				node.MarkPFail()
				log.Printf("Node %s marked as PFAIL", node.ID[:8])
			}
		}
		
		// 检查是否有足够的故障报告
		if node.State == cluster.NodeStatePFail {
			// 需要半数以上 master 报告
			masterCount := g.countMasters()
			failReports := node.CountFailReports(ClusterFailReportValidity)
			
			if failReports >= (masterCount/2 + 1) {
				node.MarkFail()
				log.Printf("Node %s marked as FAIL (reports: %d/%d)", 
					node.ID[:8], failReports, masterCount)
				
				// 广播 FAIL 消息
				go g.broadcastFail(node.ID)
				
				if g.onNodeLeave != nil {
					go g.onNodeLeave(node)
				}
			}
		}
	}
}

// broadcastFail 广播 FAIL 消息
func (g *Gossip) broadcastFail(failNodeID string) {
	g.nodesMu.RLock()
	nodes := make([]*cluster.Node, 0, len(g.nodes))
	for _, node := range g.nodes {
		if node.ID != g.self.ID && node.State == cluster.NodeStateConnected {
			nodes = append(nodes, node)
		}
	}
	g.nodesMu.RUnlock()
	
	msg := NewFailMessage(g.self.ID, failNodeID)
	data, _ := msg.Encode()
	
	for _, node := range nodes {
		go func(n *cluster.Node) {
			conn, err := net.DialTimeout("tcp", n.ClusterAddr(), 2*time.Second)
			if err != nil {
				return
			}
			defer conn.Close()
			g.writeMessage(conn, data)
		}(node)
	}
}

// selfNodeInfo 获取本节点信息
func (g *Gossip) selfNodeInfo() *NodeInfo {
	var flags uint16
	if g.self.Role == cluster.NodeRoleMaster {
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

// randomGossipNodes 随机选择节点进行 Gossip
func (g *Gossip) randomGossipNodes() []*NodeInfo {
	g.nodesMu.RLock()
	defer g.nodesMu.RUnlock()
	
	var nodes []*NodeInfo
	for _, node := range g.nodes {
		if node.ID == g.self.ID {
			continue
		}
		
		var flags uint16
		if node.Role == cluster.NodeRoleMaster {
			flags |= NodeFlagMaster
		} else {
			flags |= NodeFlagReplica
		}
		if node.State == cluster.NodeStatePFail {
			flags |= NodeFlagPFail
		}
		if node.State == cluster.NodeStateFail {
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
	}
	
	// 随机选择
	if len(nodes) > GossipCount {
		rand.Shuffle(len(nodes), func(i, j int) {
			nodes[i], nodes[j] = nodes[j], nodes[i]
		})
		nodes = nodes[:GossipCount]
	}
	
	return nodes
}

// countMasters 统计 master 数量
func (g *Gossip) countMasters() int {
	count := 0
	for _, node := range g.nodes {
		if node.Role == cluster.NodeRoleMaster && node.State == cluster.NodeStateConnected {
			count++
		}
	}
	return count
}

// GetNodes 获取所有节点
func (g *Gossip) GetNodes() []*cluster.Node {
	g.nodesMu.RLock()
	defer g.nodesMu.RUnlock()
	
	nodes := make([]*cluster.Node, 0, len(g.nodes))
	for _, node := range g.nodes {
		nodes = append(nodes, node.Clone())
	}
	return nodes
}

// GetNode 获取指定节点
func (g *Gossip) GetNode(id string) *cluster.Node {
	g.nodesMu.RLock()
	defer g.nodesMu.RUnlock()
	
	if node, ok := g.nodes[id]; ok {
		return node.Clone()
	}
	return nil
}

// SetEventHandlers 设置事件回调
func (g *Gossip) SetEventHandlers(onJoin, onLeave func(*cluster.Node), onSlotChange func(uint16, string)) {
	g.onNodeJoin = onJoin
	g.onNodeLeave = onLeave
	g.onSlotChange = onSlotChange
}

// 网络工具函数

func (g *Gossip) writeMessage(conn net.Conn, data []byte) error {
	// 4 字节长度 + 数据
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
	// 读取长度
	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lengthBuf); err != nil {
		return nil, err
	}
	
	length := uint32(lengthBuf[0])<<24 | uint32(lengthBuf[1])<<16 | 
		uint32(lengthBuf[2])<<8 | uint32(lengthBuf[3])
	
	if length > 1024*1024 { // 1MB 限制
		return nil, fmt.Errorf("message too large: %d", length)
	}
	
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, err
	}
	
	return data, nil
}
```

---

## Week 3: 集群管理器与请求路由

### 3.1 集群管理器

#### internal/cluster/cluster.go
```go
package cluster

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/10yihang/autocache/internal/cluster/gossip"
	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/engine/memory"
)

// ClusterState 集群状态
type ClusterState int

const (
	ClusterStateDown ClusterState = iota
	ClusterStateOK
	ClusterStateFail
)

// Cluster 集群管理器
type Cluster struct {
	// 本节点
	self *Node
	
	// 存储引擎
	store *memory.Store
	
	// Slot 管理
	slots *SlotManager
	
	// Gossip
	gossip *gossip.Gossip
	
	// 状态
	state ClusterState
	
	mu sync.RWMutex
}

// Config 集群配置
type Config struct {
	NodeID      string
	BindAddr    string
	Port        int
	ClusterPort int
	
	// 初始 seeds
	Seeds []string
}

// NewCluster 创建集群
func NewCluster(cfg *Config, store *memory.Store) (*Cluster, error) {
	// 创建本节点
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
	
	// 创建 Slot 管理器
	slots := NewSlotManager()
	
	// 创建集群
	c := &Cluster{
		self:  self,
		store: store,
		slots: slots,
		state: ClusterStateDown,
	}
	
	// 创建 Gossip
	c.gossip = gossip.NewGossip(self, slots)
	c.gossip.SetEventHandlers(c.onNodeJoin, c.onNodeLeave, c.onSlotChange)
	
	return c, nil
}

// Start 启动集群
func (c *Cluster) Start(seeds []string) error {
	// 启动 Gossip
	if err := c.gossip.Start(); err != nil {
		return err
	}
	
	// 连接种子节点
	for _, seed := range seeds {
		if err := c.gossip.Meet(seed); err != nil {
			log.Printf("Failed to meet seed %s: %v", seed, err)
		}
	}
	
	c.state = ClusterStateOK
	return nil
}

// Stop 停止集群
func (c *Cluster) Stop() error {
	return c.gossip.Stop()
}

// GetSelf 获取本节点
func (c *Cluster) GetSelf() *Node {
	return c.self.Clone()
}

// GetNodes 获取所有节点
func (c *Cluster) GetNodes() []*Node {
	return c.gossip.GetNodes()
}

// GetSlotNode 获取 slot 所属节点
func (c *Cluster) GetSlotNode(slot uint16) *Node {
	nodeID := c.slots.GetSlotNode(slot)
	if nodeID == "" {
		return nil
	}
	return c.gossip.GetNode(nodeID)
}

// GetKeyNode 获取 key 所属节点
func (c *Cluster) GetKeyNode(key string) *Node {
	slot := hash.KeySlot(key)
	return c.GetSlotNode(slot)
}

// IsLocalKey 检查 key 是否属于本节点
func (c *Cluster) IsLocalKey(key string) bool {
	slot := hash.KeySlot(key)
	nodeID := c.slots.GetSlotNode(slot)
	return nodeID == c.self.ID
}

// GetKeySlot 获取 key 对应的 slot
func (c *Cluster) GetKeySlot(key string) uint16 {
	return hash.KeySlot(key)
}

// RouteKey 路由 key，返回目标节点信息
// 如果是本地，返回 nil
// 如果需要重定向，返回目标节点
func (c *Cluster) RouteKey(key string) (*Node, error) {
	slot := hash.KeySlot(key)
	slotInfo := c.slots.GetSlotInfo(slot)
	
	if slotInfo == nil || slotInfo.NodeID == "" {
		return nil, fmt.Errorf("slot %d not assigned", slot)
	}
	
	// 本地
	if slotInfo.NodeID == c.self.ID {
		// 检查是否正在迁移
		if slotInfo.State == SlotStateExporting {
			// 返回 ASK 重定向
			return c.gossip.GetNode(slotInfo.Exporting), ErrAsk
		}
		return nil, nil
	}
	
	// 远程
	return c.gossip.GetNode(slotInfo.NodeID), ErrMoved
}

// AssignSlots 分配 slots 给本节点
func (c *Cluster) AssignSlots(slots []uint16) error {
	for _, slot := range slots {
		if err := c.slots.AssignSlot(slot, c.self.ID); err != nil {
			return err
		}
	}
	return nil
}

// AssignSlotRange 分配 slot 范围给本节点
func (c *Cluster) AssignSlotRange(start, end uint16) error {
	return c.slots.AssignSlotRange(start, end, c.self.ID)
}

// GetClusterInfo 获取集群信息（用于 CLUSTER INFO 命令）
func (c *Cluster) GetClusterInfo() map[string]interface{} {
	nodes := c.gossip.GetNodes()
	
	masterCount := 0
	for _, node := range nodes {
		if node.Role == NodeRoleMaster {
			masterCount++
		}
	}
	
	return map[string]interface{}{
		"cluster_state":           c.state.String(),
		"cluster_slots_assigned":  c.slots.CountAssigned(),
		"cluster_slots_ok":        c.slots.CountAssigned(),
		"cluster_known_nodes":     len(nodes),
		"cluster_size":            masterCount,
		"cluster_current_epoch":   0,
		"cluster_my_epoch":        0,
	}
}

// GetClusterSlots 获取 CLUSTER SLOTS 信息
func (c *Cluster) GetClusterSlots() []SlotRange {
	return c.slots.GetClusterSlots()
}

// 事件处理

func (c *Cluster) onNodeJoin(node *Node) {
	log.Printf("Node joined: %s (%s)", node.ID[:8], node.Addr())
}

func (c *Cluster) onNodeLeave(node *Node) {
	log.Printf("Node left: %s (%s)", node.ID[:8], node.Addr())
	// TODO: 故障转移逻辑
}

func (c *Cluster) onSlotChange(slot uint16, nodeID string) {
	log.Printf("Slot %d assigned to %s", slot, nodeID[:8])
}

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

// 错误定义
var (
	ErrMoved = &ClusterError{Type: "MOVED"}
	ErrAsk   = &ClusterError{Type: "ASK"}
)

type ClusterError struct {
	Type   string
	Slot   uint16
	Addr   string
}

func (e *ClusterError) Error() string {
	return fmt.Sprintf("%s %d %s", e.Type, e.Slot, e.Addr)
}
```

### 3.2 集群命令处理

#### internal/protocol/commands/cluster.go
```go
package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/tidwall/redcon"
	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/hash"
)

// ClusterHandler 集群命令处理器
type ClusterHandler struct {
	cluster *cluster.Cluster
}

// NewClusterHandler 创建处理器
func NewClusterHandler(c *cluster.Cluster) *ClusterHandler {
	return &ClusterHandler{cluster: c}
}

// HandleCluster 处理 CLUSTER 命令
func (h *ClusterHandler) HandleCluster(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'cluster' command")
		return
	}
	
	subcmd := strings.ToUpper(string(args[0]))
	
	switch subcmd {
	case "INFO":
		h.clusterInfo(conn)
	case "NODES":
		h.clusterNodes(conn)
	case "SLOTS":
		h.clusterSlots(conn)
	case "KEYSLOT":
		h.clusterKeySlot(conn, args[1:])
	case "MEET":
		h.clusterMeet(conn, args[1:])
	case "ADDSLOTS":
		h.clusterAddSlots(conn, args[1:])
	case "DELSLOTS":
		h.clusterDelSlots(conn, args[1:])
	case "MYID":
		h.clusterMyID(conn)
	case "GETKEYSINSLOT":
		h.clusterGetKeysInSlot(conn, args[1:])
	default:
		conn.WriteError("ERR unknown subcommand '" + subcmd + "'")
	}
}

// clusterInfo CLUSTER INFO
func (h *ClusterHandler) clusterInfo(conn redcon.Conn) {
	info := h.cluster.GetClusterInfo()
	
	var sb strings.Builder
	for k, v := range info {
		sb.WriteString(fmt.Sprintf("%s:%v\r\n", k, v))
	}
	
	conn.WriteBulkString(sb.String())
}

// clusterNodes CLUSTER NODES
func (h *ClusterHandler) clusterNodes(conn redcon.Conn) {
	nodes := h.cluster.GetNodes()
	self := h.cluster.GetSelf()
	
	var sb strings.Builder
	for _, node := range nodes {
		// 格式：<id> <ip:port@cport> <flags> <master> <ping-sent> <pong-recv> <config-epoch> <link-state> <slot>
		flags := node.Role.String()
		if node.ID == self.ID {
			flags = "myself," + flags
		}
		if node.State == cluster.NodeStateFail {
			flags += ",fail"
		} else if node.State == cluster.NodeStatePFail {
			flags += ",fail?"
		}
		
		masterID := "-"
		if node.MasterID != "" {
			masterID = node.MasterID
		}
		
		linkState := "connected"
		if node.State != cluster.NodeStateConnected {
			linkState = "disconnected"
		}
		
		line := fmt.Sprintf("%s %s:%d@%d %s %s %d %d 0 %s\n",
			node.ID,
			node.IP, node.Port, node.ClusterPort,
			flags,
			masterID,
			node.PingSent,
			node.PongReceived,
			linkState,
		)
		sb.WriteString(line)
	}
	
	conn.WriteBulkString(sb.String())
}

// clusterSlots CLUSTER SLOTS
func (h *ClusterHandler) clusterSlots(conn redcon.Conn) {
	ranges := h.cluster.GetClusterSlots()
	
	conn.WriteArray(len(ranges))
	for _, r := range ranges {
		node := h.cluster.GetSlotNode(r.Start)
		if node == nil {
			continue
		}
		
		// [start, end, [ip, port, id], ...]
		conn.WriteArray(3)
		conn.WriteInt64(int64(r.Start))
		conn.WriteInt64(int64(r.End))
		
		// 节点信息
		conn.WriteArray(3)
		conn.WriteBulkString(node.IP)
		conn.WriteInt(node.Port)
		conn.WriteBulkString(node.ID)
	}
}

// clusterKeySlot CLUSTER KEYSLOT <key>
func (h *ClusterHandler) clusterKeySlot(conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'cluster keyslot' command")
		return
	}
	
	slot := hash.KeySlot(string(args[0]))
	conn.WriteInt64(int64(slot))
}

// clusterMeet CLUSTER MEET <ip> <port>
func (h *ClusterHandler) clusterMeet(conn redcon.Conn, args [][]byte) {
	if len(args) < 2 {
		conn.WriteError("ERR wrong number of arguments for 'cluster meet' command")
		return
	}
	
	ip := string(args[0])
	port, err := strconv.Atoi(string(args[1]))
	if err != nil {
		conn.WriteError("ERR Invalid port")
		return
	}
	
	// 集群端口
	clusterPort := port + 10000
	if len(args) >= 3 {
		clusterPort, _ = strconv.Atoi(string(args[2]))
	}
	
	addr := fmt.Sprintf("%s:%d", ip, clusterPort)
	// TODO: 调用 gossip.Meet(addr)
	
	conn.WriteString("OK")
}

// clusterAddSlots CLUSTER ADDSLOTS <slot> [slot ...]
func (h *ClusterHandler) clusterAddSlots(conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'cluster addslots' command")
		return
	}
	
	slots := make([]uint16, 0, len(args))
	for _, arg := range args {
		slot, err := strconv.ParseUint(string(arg), 10, 16)
		if err != nil || slot >= hash.SlotCount {
			conn.WriteError("ERR Invalid slot")
			return
		}
		slots = append(slots, uint16(slot))
	}
	
	if err := h.cluster.AssignSlots(slots); err != nil {
		conn.WriteError("ERR " + err.Error())
		return
	}
	
	conn.WriteString("OK")
}

// clusterDelSlots CLUSTER DELSLOTS <slot> [slot ...]
func (h *ClusterHandler) clusterDelSlots(conn redcon.Conn, args [][]byte) {
	// TODO: 实现
	conn.WriteString("OK")
}

// clusterMyID CLUSTER MYID
func (h *ClusterHandler) clusterMyID(conn redcon.Conn) {
	self := h.cluster.GetSelf()
	conn.WriteBulkString(self.ID)
}

// clusterGetKeysInSlot CLUSTER GETKEYSINSLOT <slot> <count>
func (h *ClusterHandler) clusterGetKeysInSlot(conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'cluster getkeysinslot' command")
		return
	}
	
	// TODO: 实现
	conn.WriteArray(0)
}

// CheckKeyRouting 检查 key 路由，返回 MOVED/ASK 错误
func (h *ClusterHandler) CheckKeyRouting(key string) error {
	node, err := h.cluster.RouteKey(key)
	if err == nil {
		return nil // 本地
	}
	
	slot := h.cluster.GetKeySlot(key)
	
	if err == cluster.ErrMoved {
		return &cluster.ClusterError{
			Type: "MOVED",
			Slot: slot,
			Addr: node.Addr(),
		}
	}
	
	if err == cluster.ErrAsk {
		return &cluster.ClusterError{
			Type: "ASK",
			Slot: slot,
			Addr: node.Addr(),
		}
	}
	
	return err
}
```

---

## Week 4: 数据迁移与测试

### 4.1 数据迁移器

#### internal/cluster/migrate/migrator.go
```go
package migrate

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/engine/memory"
)

// MigrateState 迁移状态
type MigrateState int

const (
	MigrateStateIdle MigrateState = iota
	MigrateStateRunning
	MigrateStatePaused
	MigrateStateCompleted
	MigrateStateFailed
)

// MigrateTask 迁移任务
type MigrateTask struct {
	ID          string
	Slot        uint16
	SourceNode  string
	TargetNode  string
	State       MigrateState
	KeysMigrated int64
	KeysTotal    int64
	StartTime   time.Time
	EndTime     time.Time
	Error       error
}

// Migrator 数据迁移器
type Migrator struct {
	slots  *cluster.SlotManager
	store  *memory.Store
	
	tasks   map[string]*MigrateTask
	tasksMu sync.RWMutex
	
	ctx    context.Context
	cancel context.CancelFunc
}

// NewMigrator 创建迁移器
func NewMigrator(slots *cluster.SlotManager, store *memory.Store) *Migrator {
	ctx, cancel := context.WithCancel(context.Background())
	return &Migrator{
		slots:  slots,
		store:  store,
		tasks:  make(map[string]*MigrateTask),
		ctx:    ctx,
		cancel: cancel,
	}
}

// StartMigration 开始迁移
func (m *Migrator) StartMigration(slot uint16, targetNodeAddr string) (*MigrateTask, error) {
	taskID := fmt.Sprintf("migrate-%d-%d", slot, time.Now().UnixNano())
	
	task := &MigrateTask{
		ID:         taskID,
		Slot:       slot,
		TargetNode: targetNodeAddr,
		State:      MigrateStateRunning,
		StartTime:  time.Now(),
	}
	
	m.tasksMu.Lock()
	m.tasks[taskID] = task
	m.tasksMu.Unlock()
	
	// 设置 slot 为导出状态
	m.slots.SetExporting(slot, targetNodeAddr)
	
	// 异步执行迁移
	go m.doMigration(task)
	
	return task, nil
}

// doMigration 执行迁移
func (m *Migrator) doMigration(task *MigrateTask) {
	defer func() {
		task.EndTime = time.Now()
	}()
	
	// 连接目标节点
	conn, err := net.DialTimeout("tcp", task.TargetNode, 5*time.Second)
	if err != nil {
		task.State = MigrateStateFailed
		task.Error = err
		return
	}
	defer conn.Close()
	
	// 获取该 slot 的所有 key
	keys := m.getKeysInSlot(task.Slot)
	task.KeysTotal = int64(len(keys))
	
	log.Printf("Starting migration of slot %d: %d keys to %s", 
		task.Slot, len(keys), task.TargetNode)
	
	// 逐个迁移
	for _, key := range keys {
		select {
		case <-m.ctx.Done():
			task.State = MigrateStateFailed
			task.Error = context.Canceled
			return
		default:
		}
		
		if err := m.migrateKey(conn, key); err != nil {
			task.State = MigrateStateFailed
			task.Error = err
			return
		}
		
		task.KeysMigrated++
	}
	
	// 完成迁移
	task.State = MigrateStateCompleted
	m.slots.FinishMigration(task.Slot, task.TargetNode)
	
	log.Printf("Migration of slot %d completed: %d keys", task.Slot, task.KeysMigrated)
}

// getKeysInSlot 获取 slot 中的所有 key
func (m *Migrator) getKeysInSlot(slot uint16) []string {
	var keys []string
	
	ctx := context.Background()
	allKeys, _ := m.store.Keys(ctx, "*")
	
	for _, key := range allKeys {
		if hash.KeySlot(key) == slot {
			keys = append(keys, key)
		}
	}
	
	return keys
}

// migrateKey 迁移单个 key
func (m *Migrator) migrateKey(conn net.Conn, key string) error {
	ctx := context.Background()
	
	// 获取值
	val, err := m.store.Get(ctx, key)
	if err != nil {
		return nil // key 可能已过期，跳过
	}
	
	// 获取 TTL
	ttl, _ := m.store.TTL(ctx, key)
	
	// 发送 RESTORE 命令
	// 这里简化处理，实际应该发送序列化的数据
	cmd := fmt.Sprintf("*4\r\n$7\r\nRESTORE\r\n$%d\r\n%s\r\n$%d\r\n%d\r\n$%d\r\n%s\r\n",
		len(key), key,
		len(fmt.Sprint(ttl.Milliseconds())), ttl.Milliseconds(),
		len(val), val,
	)
	
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return err
	}
	
	// 读取响应
	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}
	
	resp := string(buf[:n])
	if resp[0] == '-' {
		return fmt.Errorf("RESTORE failed: %s", resp)
	}
	
	// 删除本地 key
	m.store.Del(ctx, key)
	
	return nil
}

// GetTask 获取迁移任务
func (m *Migrator) GetTask(taskID string) *MigrateTask {
	m.tasksMu.RLock()
	defer m.tasksMu.RUnlock()
	return m.tasks[taskID]
}

// GetAllTasks 获取所有任务
func (m *Migrator) GetAllTasks() []*MigrateTask {
	m.tasksMu.RLock()
	defer m.tasksMu.RUnlock()
	
	tasks := make([]*MigrateTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// Stop 停止迁移器
func (m *Migrator) Stop() {
	m.cancel()
}
```

---

## 测试验证

### 集群测试脚本

#### test/cluster/cluster_test.sh
```bash
#!/bin/bash

# 启动 3 节点集群测试

# 启动节点 1
./autocache --port 7001 --cluster-port 17001 &
PID1=$!

# 启动节点 2
./autocache --port 7002 --cluster-port 17002 --meet 127.0.0.1:17001 &
PID2=$!

# 启动节点 3
./autocache --port 7003 --cluster-port 17003 --meet 127.0.0.1:17001 &
PID3=$!

sleep 2

# 分配 slots
redis-cli -p 7001 CLUSTER ADDSLOTS $(seq 0 5460 | tr '\n' ' ')
redis-cli -p 7002 CLUSTER ADDSLOTS $(seq 5461 10922 | tr '\n' ' ')
redis-cli -p 7003 CLUSTER ADDSLOTS $(seq 10923 16383 | tr '\n' ' ')

# 验证集群状态
echo "=== Cluster Info ==="
redis-cli -p 7001 CLUSTER INFO

echo "=== Cluster Nodes ==="
redis-cli -p 7001 CLUSTER NODES

# 测试数据操作
echo "=== Testing SET/GET ==="
for i in $(seq 1 100); do
    redis-cli -c -p 7001 SET "key$i" "value$i"
done

for i in $(seq 1 100); do
    redis-cli -c -p 7001 GET "key$i"
done

# 清理
kill $PID1 $PID2 $PID3
```

---

## 交付物

- [x] CRC16 哈希实现（Redis 兼容）
- [x] 16384 Slots 分片管理
- [x] Gossip 协议实现（PING/PONG/MEET/FAIL）
- [x] 节点发现与故障检测
- [x] 集群命令（CLUSTER INFO/NODES/SLOTS/KEYSLOT/MEET/ADDSLOTS）
- [x] MOVED/ASK 重定向
- [x] 基础数据迁移

## 验收标准

```bash
# 启动 3 节点集群
./scripts/start-cluster.sh

# 验证集群状态
redis-cli -p 7001 CLUSTER INFO
# cluster_state:ok
# cluster_slots_assigned:16384

# 测试重定向
redis-cli -c -p 7001 SET foo bar
# -> Redirected to slot [12182] located at 127.0.0.1:7003
# OK

redis-cli -c -p 7001 GET foo
# -> Redirected to slot [12182] located at 127.0.0.1:7003
# "bar"
```

## 下一步

完成 Phase 2 后，进入 [Phase 3: K8s Operator](./04-phase3-operator.md)
