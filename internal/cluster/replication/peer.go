package replication

import (
	"context"
	"sync"
)

type PeerTarget struct {
	NodeID string
	Addr   string
}

type peerSender func(ctx context.Context, addr string, op Op) error

type peer struct {
	nodeID string
	addr   string
	queue  chan Op
	stopCh chan struct{}
	wg     sync.WaitGroup
	send   peerSender
	onSent func(nodeID string, op Op)
}

func newPeer(target PeerTarget, queueSize int, send peerSender, onSent func(nodeID string, op Op)) *peer {
	if queueSize <= 0 {
		queueSize = 1024
	}
	p := &peer{
		nodeID: target.NodeID,
		addr:   target.Addr,
		queue:  make(chan Op, queueSize),
		stopCh: make(chan struct{}),
		send:   send,
		onSent: onSent,
	}
	p.wg.Add(1)
	go p.loop()
	return p
}

func (p *peer) enqueue(op Op) bool {
	select {
	case p.queue <- op:
		return true
	default:
		return false
	}
}

func (p *peer) close() {
	close(p.stopCh)
	p.wg.Wait()
}

func (p *peer) loop() {
	defer p.wg.Done()
	for {
		select {
		case <-p.stopCh:
			return
		case op := <-p.queue:
			if err := p.send(context.Background(), p.addr, op); err == nil && p.onSent != nil {
				p.onSent(p.nodeID, op)
			}
		}
	}
}
