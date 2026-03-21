package replication

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

type Stream struct {
	mu        sync.Mutex
	peers     map[string]*peer
	queueSize int
	send      peerSender
	onSent    func(nodeID string, op Op)
}

func NewStream(queueSize int) *Stream {
	return &Stream{
		peers:     make(map[string]*peer),
		queueSize: queueSize,
		send:      sendOpOverTCP,
	}
}

func (s *Stream) SetSender(send peerSender) {
	if s == nil || send == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.send = send
}

func (s *Stream) SetOnSent(onSent func(nodeID string, op Op)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onSent = onSent
}

func (s *Stream) Enqueue(target PeerTarget, op Op) {
	if s == nil || target.NodeID == "" || target.Addr == "" {
		return
	}
	s.mu.Lock()
	p := s.peers[target.NodeID]
	if p == nil || p.addr != target.Addr {
		if p != nil {
			p.close()
		}
		p = newPeer(target, s.queueSize, s.send, s.onSent)
		s.peers[target.NodeID] = p
	}
	s.mu.Unlock()
	_ = p.enqueue(op)
}

func (s *Stream) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for nodeID, p := range s.peers {
		p.close()
		delete(s.peers, nodeID)
	}
}

func sendOpOverTCP(ctx context.Context, addr string, op Op) error {
	deadline := time.Now().Add(2 * time.Second)
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}

	if _, err := conn.Write(encodeReplicationApplyCommand(op)); err != nil {
		return err
	}

	resp, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return err
	}
	if resp != "+OK\r\n" {
		return fmt.Errorf("replication apply response: %s", resp)
	}
	return nil
}

func encodeReplicationApplyCommand(op Op) []byte {
	fields := [][]byte{
		[]byte("REPLAPPLY"),
		[]byte(strconv.FormatUint(uint64(op.Slot), 10)),
		[]byte(strconv.FormatUint(op.Epoch, 10)),
		[]byte(strconv.FormatUint(op.LSN, 10)),
		[]byte(op.OpType),
		[]byte(op.Key),
		[]byte(strconv.FormatInt(op.ExpireAtNs, 10)),
		op.Payload,
	}

	var buf bytes.Buffer
	buf.WriteString("*")
	buf.WriteString(strconv.Itoa(len(fields)))
	buf.WriteString("\r\n")
	for _, field := range fields {
		buf.WriteString("$")
		buf.WriteString(strconv.Itoa(len(field)))
		buf.WriteString("\r\n")
		buf.Write(field)
		buf.WriteString("\r\n")
	}
	return buf.Bytes()
}
