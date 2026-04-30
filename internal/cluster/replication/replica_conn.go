package replication

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// ── Master side: dedicated TCP listener for replica connections ──────────────

// ReplListener accepts persistent TCP connections from replicas.
// Each connection is registered and used by the master to push REPLAPPLY ops.
type ReplListener struct {
	addr     string
	ln       net.Listener
	reg      *MasterConnRegistry
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewReplListener(addr string, reg *MasterConnRegistry) *ReplListener {
	return &ReplListener{
		addr:   addr,
		reg:    reg,
		stopCh: make(chan struct{}),
	}
}

func (l *ReplListener) Start() error {
	ln, err := net.Listen("tcp", l.addr)
	if err != nil {
		return fmt.Errorf("replication listener: %w", err)
	}
	l.ln = ln
	l.wg.Add(1)
	go l.acceptLoop()
	log.Printf("Replication listener started on %s", l.addr)
	return nil
}

func (l *ReplListener) Stop() {
	l.stopOnce.Do(func() {
		close(l.stopCh)
	})
	if l.ln != nil {
		l.ln.Close()
	}
	l.wg.Wait()
}

func (l *ReplListener) acceptLoop() {
	defer l.wg.Done()
	for {
		select {
		case <-l.stopCh:
			return
		default:
		}

		conn, err := l.ln.Accept()
		if err != nil {
			select {
			case <-l.stopCh:
				return
			default:
				log.Printf("Replication listener accept error: %v", err)
				continue
			}
		}
		l.wg.Add(1)
		go l.handleReplicaConn(conn)
	}
}

func (l *ReplListener) handleReplicaConn(conn net.Conn) {
	defer l.wg.Done()
	defer func() {
		// On disconnect, unregister and close.
		l.reg.RemoveByConn(conn)
		conn.Close()
	}()

	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Read handshake: REPL <replica-id>\n
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("Replication handshake read error from %s: %v", conn.RemoteAddr(), err)
		return
	}

	var replicaID string
	if _, err := fmt.Sscanf(line, "REPL %s", &replicaID); err != nil || replicaID == "" {
		log.Printf("Replication invalid handshake from %s: %q", conn.RemoteAddr(), line)
		return
	}

	// Send +OK\n
	if _, err := conn.Write([]byte("+OK\n")); err != nil {
		log.Printf("Replication handshake write error to %s: %v", replicaID, err)
		return
	}

	// Register this persistent connection for push.
	l.reg.Register(replicaID, conn)
	log.Printf("Replica %s connected (persistent, addr=%s)", replicaID, conn.RemoteAddr())

	// Read with periodic deadline so we can check for stop signal.
	buf := make([]byte, 1)
	for {
		select {
		case <-l.stopCh:
			return
		default:
		}
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, err := conn.Read(buf)
		if err != nil {
			// Connection closed or timeout — exit the goroutine.
			return
		}
	}
}

// ── Connection registry ─────────────────────────────────────────────────────

type replicaConn struct {
	id   string
	conn net.Conn
}

type MasterConnRegistry struct {
	mu    sync.RWMutex
	conns map[string]*replicaConn // replicaID -> conn
}

func NewMasterConnRegistry() *MasterConnRegistry {
	return &MasterConnRegistry{conns: make(map[string]*replicaConn)}
}

func (r *MasterConnRegistry) Register(replicaID string, conn net.Conn) {
	r.mu.Lock()
	if old, ok := r.conns[replicaID]; ok {
		old.conn.Close()
	}
	r.conns[replicaID] = &replicaConn{id: replicaID, conn: conn}
	r.mu.Unlock()
	log.Printf("Replica %s registered (persistent, addr=%s)", replicaID, conn.RemoteAddr())
}

func (r *MasterConnRegistry) RemoveByConn(target net.Conn) {
	r.mu.Lock()
	for id, rc := range r.conns {
		if rc.conn == target {
			delete(r.conns, id)
			log.Printf("Replica %s unregistered (disconnected)", id)
			break
		}
	}
	r.mu.Unlock()
}

// Broadcast sends op to all registered replicas through persistent connections.
func (r *MasterConnRegistry) Broadcast(op Op) int {
	r.mu.RLock()
	snapshot := make([]*replicaConn, 0, len(r.conns))
	for _, rc := range r.conns {
		snapshot = append(snapshot, rc)
	}
	r.mu.RUnlock()

	data := encodeReplicationApplyCommand(op)
	sent := 0
	for _, rc := range snapshot {
		rc.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if _, err := rc.conn.Write(data); err != nil {
			log.Printf("Failed to send op to replica %s: %v", rc.id, err)
			r.mu.Lock()
			delete(r.conns, rc.id)
			r.mu.Unlock()
			rc.conn.Close()
			continue
		}
		sent++
	}
	return sent
}

func (r *MasterConnRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.conns)
}

// ── Replica side: persistent client to master's replication port ────────────

type ReplicaClient struct {
	masterAddr     string
	replicaID      string
	masterID       string
	conn           net.Conn
	mu             sync.Mutex
	stopCh         chan struct{}
	onOp           func(op Op) error
	masterIsFailed func() bool // gossip check: is our master marked FAIL?
	onFailover     func()      // called when we decide to self-promote
	failoverDone   bool
}

func NewReplicaClient(masterAddr, replicaID, masterID string, onOp func(op Op) error) *ReplicaClient {
	return &ReplicaClient{
		masterAddr: masterAddr,
		replicaID:  replicaID,
		masterID:   masterID,
		stopCh:     make(chan struct{}),
		onOp:       onOp,
	}
}

// SetMasterFailureCheck sets a callback that checks via gossip whether the master is FAIL.
func (rc *ReplicaClient) SetMasterFailureCheck(fn func() bool) {
	rc.masterIsFailed = fn
}

// SetFailoverCallback sets a callback invoked when the replica decides to self-promote.
func (rc *ReplicaClient) SetFailoverCallback(fn func()) {
	rc.onFailover = fn
}

func (rc *ReplicaClient) Start() {
	go rc.run()
}

func (rc *ReplicaClient) run() {
	consecutiveFailures := 0
	for {
		select {
		case <-rc.stopCh:
			return
		default:
		}
		err := rc.connectAndReceive()
		if err == nil {
			consecutiveFailures = 0
		} else {
			consecutiveFailures++
		}

		// Redis-style failover: if master connection keeps failing and gossip
		// confirms the master is FAIL, self-promote instead of reconnecting.
		if consecutiveFailures >= 3 && rc.masterIsFailed != nil && rc.masterIsFailed() {
			log.Printf("ReplicaClient %s: master %s confirmed FAIL, initiating self-promotion",
				rc.replicaID, rc.masterID)
			rc.failoverDone = true
			if rc.onFailover != nil {
				rc.onFailover()
			}
			return
		}

		if rc.failoverDone {
			return
		}

		log.Printf("ReplicaClient %s: %v, reconnecting in 3s", rc.replicaID, err)
		select {
		case <-rc.stopCh:
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (rc *ReplicaClient) connectAndReceive() error {
	conn, err := net.DialTimeout("tcp", rc.masterAddr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", rc.masterAddr, err)
	}

	// Send handshake: REPL <replica-id>\n
	handshake := fmt.Sprintf("REPL %s\n", rc.replicaID)
	if _, err := conn.Write([]byte(handshake)); err != nil {
		conn.Close()
		return fmt.Errorf("handshake write: %w", err)
	}

	// Read +OK\n
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return fmt.Errorf("handshake read: %w", err)
	}
	if line != "+OK\n" {
		conn.Close()
		return fmt.Errorf("unexpected handshake response: %q", line)
	}

	rc.mu.Lock()
	if rc.conn != nil {
		rc.conn.Close()
	}
	rc.conn = conn
	rc.mu.Unlock()

	log.Printf("ReplicaClient %s: connected to master at %s", rc.replicaID, rc.masterAddr)

	return rc.receiveLoop(conn)
}

func (rc *ReplicaClient) receiveLoop(conn net.Conn) error {
	reader := bufio.NewReader(conn)
	for {
		select {
		case <-rc.stopCh:
			return nil
		default:
		}

		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		op, err := readReplicationOp(reader)
		if err != nil {
			return fmt.Errorf("read op: %w", err)
		}
		if err := rc.onOp(op); err != nil {
			log.Printf("ReplicaClient %s: apply op failed: %v", rc.replicaID, err)
		}
	}
}

func (rc *ReplicaClient) Stop() {
	close(rc.stopCh)
	rc.mu.Lock()
	if rc.conn != nil {
		rc.conn.Close()
	}
	rc.mu.Unlock()
}

// ── RESP parser for REPLAPPLY (shared) ───────────────────────────────────────

func readReplicationOp(reader *bufio.Reader) (Op, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return Op{}, fmt.Errorf("read array header: %w", err)
	}
	if line[0] != '*' {
		return Op{}, fmt.Errorf("expected array, got %q", line)
	}
	count := 0
	fmt.Sscanf(line, "*%d\r\n", &count)
	if count < 8 {
		return Op{}, fmt.Errorf("expected >=8 fields, got %d", count)
	}

	readField := func() (string, error) {
		lenLine, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		blen := 0
		fmt.Sscanf(lenLine, "$%d\r\n", &blen)
		data := make([]byte, blen+2)
		if _, err := io.ReadFull(reader, data); err != nil {
			return "", err
		}
		return string(data[:blen]), nil
	}

	var slot, epoch, lsn uint64
	var opType, key string
	var expireAtNs int64
	var payload []byte

	for i := 0; i < count; i++ {
		val, err := readField()
		if err != nil {
			return Op{}, err
		}
		switch i {
		case 0: // REPLAPPLY marker
		case 1: // slot
			fmt.Sscanf(val, "%d", &slot)
		case 2: // epoch
			fmt.Sscanf(val, "%d", &epoch)
		case 3: // lsn
			fmt.Sscanf(val, "%d", &lsn)
		case 4: // opType
			opType = val
		case 5: // key
			key = val
		case 6: // expireAtNs
			fmt.Sscanf(val, "%d", &expireAtNs)
		case 7: // payload
			payload = []byte(val)
		}
	}

	return Op{
		Slot:       uint16(slot),
		Epoch:      epoch,
		LSN:        lsn,
		OpType:     opType,
		Key:        key,
		ExpireAtNs: expireAtNs,
		Payload:    payload,
	}, nil
}
