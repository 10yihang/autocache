package gossip

import (
	"bytes"
	"encoding/gob"
	"errors"
)

type MessageType uint8

const (
	MsgPing MessageType = iota + 1
	MsgPong
	MsgMeet
	MsgFail
	MsgPublish
	MsgUpdate
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

type Message struct {
	Type         MessageType
	Sender       string
	CurrentEpoch uint64
	ConfigEpoch  uint64
	NodeInfo     *NodeInfo
	GossipNodes  []*NodeInfo
	FailNodeID   string
	Data         []byte
}

type NodeInfo struct {
	ID          string
	IP          string
	Port        int
	ClusterPort int
	Flags       uint16
	MasterID    string
	PingSent    int64
	PongRecv    int64
	ConfigEpoch uint64
	Slots       []byte
}

const (
	NodeFlagMaster    uint16 = 1 << 0
	NodeFlagReplica   uint16 = 1 << 1
	NodeFlagPFail     uint16 = 1 << 2
	NodeFlagFail      uint16 = 1 << 3
	NodeFlagHandshake uint16 = 1 << 4
	NodeFlagNoAddr    uint16 = 1 << 5
	NodeFlagMeet      uint16 = 1 << 6
)

var ErrInvalidMessage = errors.New("invalid message")

func (m *Message) Encode() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(byte(m.Type))

	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func Decode(data []byte) (*Message, error) {
	if len(data) < 1 {
		return nil, ErrInvalidMessage
	}

	buf := bytes.NewBuffer(data[1:])
	var m Message
	dec := gob.NewDecoder(buf)
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}

	m.Type = MessageType(data[0])
	return &m, nil
}

func NewPingMessage(senderID string, nodeInfo *NodeInfo, gossipNodes []*NodeInfo) *Message {
	return &Message{
		Type:        MsgPing,
		Sender:      senderID,
		NodeInfo:    nodeInfo,
		GossipNodes: gossipNodes,
	}
}

func NewPongMessage(senderID string, nodeInfo *NodeInfo, gossipNodes []*NodeInfo) *Message {
	return &Message{
		Type:        MsgPong,
		Sender:      senderID,
		NodeInfo:    nodeInfo,
		GossipNodes: gossipNodes,
	}
}

func NewMeetMessage(senderID string, nodeInfo *NodeInfo) *Message {
	return &Message{
		Type:     MsgMeet,
		Sender:   senderID,
		NodeInfo: nodeInfo,
	}
}

func NewFailMessage(senderID string, failNodeID string) *Message {
	return &Message{
		Type:       MsgFail,
		Sender:     senderID,
		FailNodeID: failNodeID,
	}
}

func SlotsToBytes(slots []uint16) []byte {
	bitmap := make([]byte, 16384/8)
	for _, slot := range slots {
		bitmap[slot/8] |= 1 << (slot % 8)
	}
	return bitmap
}

func BytesToSlots(bitmap []byte) []uint16 {
	var slots []uint16
	for i := 0; i < len(bitmap)*8 && i < 16384; i++ {
		if bitmap[i/8]&(1<<(i%8)) != 0 {
			slots = append(slots, uint16(i))
		}
	}
	return slots
}
