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

// replicaConn is a persistent write-only connection to a replica.
type replicaConn struct {
	conn   net.Conn
	closer io.Closer // optional: DetachedConn for proper cleanup
}

// MasterConnRegistry holds persistent TCP connections from replicas.
// The master writes REPLAPPLY ops through these connections (Redis-style).
type MasterConnRegistry struct {
	mu   sync.RWMutex
	conns map[string]*replicaConn // replicaID -> conn
}

func NewMasterConnRegistry() *MasterConnRegistry {
	return &MasterConnRegistry{conns: make(map[string]*replicaConn)}
}

// Register adds a replica connection. Called when a replica connects and identifies itself.
func (r *MasterConnRegistry) Register(replicaID string, conn net.Conn, closer io.Closer) {
	r.mu.Lock()
	if old, ok := r.conns[replicaID]; ok {
		old.conn.Close()
	}
	r.conns[replicaID] = &replicaConn{conn: conn, closer: closer}
	r.mu.Unlock()
	log.Printf("Replica %s registered (addr=%s)", replicaID, conn.RemoteAddr())
}

// Unregister removes a replica connection.
func (r *MasterConnRegistry) Unregister(replicaID string) {
	r.mu.Lock()
	rc, ok := r.conns[replicaID]
	if ok {
		delete(r.conns, replicaID)
	}
	r.mu.Unlock()
	if ok {
		if rc.closer != nil {
			rc.closer.Close()
		} else {
			rc.conn.Close()
		}
		log.Printf("Replica %s unregistered", replicaID)
	}
}

// Broadcast sends op to all registered replicas. Returns count of successful sends.
func (r *MasterConnRegistry) Broadcast(op Op) int {
	r.mu.RLock()
	// Snapshot to avoid holding lock during IO
	snapshot := make(map[string]*replicaConn, len(r.conns))
	for id, rc := range r.conns {
		snapshot[id] = rc
	}
	r.mu.RUnlock()

	data := encodeReplicationApplyCommand(op)
	sent := 0
	// log.Printf("Broadcast op slot=%d lsn=%d to %d replicas (data=%d bytes)", op.Slot, op.LSN, len(snapshot), len(data))
	for id, rc := range snapshot {
		if err := writeReplicationOp(rc.conn, data); err != nil {
			log.Printf("Failed to send op to replica %s: %v", id, err)
			r.Unregister(id)
			continue
		}
		sent++
	}
	// log.Printf("Broadcast: sent to %d/%d replicas", sent, len(snapshot))
	return sent
}

// Count returns the number of registered replicas.
func (r *MasterConnRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.conns)
}

func writeReplicationOp(conn net.Conn, data []byte) error {
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := conn.Write(data)
	return err
}

// ReplicaClient maintains a persistent connection to the master for receiving replication ops.
type ReplicaClient struct {
	masterAddr string
	replicaID  string
	conn       net.Conn
	mu         sync.Mutex
	stopCh     chan struct{}
	onOp       func(op Op) error
	reconnect  time.Duration
}

func NewReplicaClient(masterAddr, replicaID string, onOp func(op Op) error) *ReplicaClient {
	return &ReplicaClient{
		masterAddr: masterAddr,
		replicaID:  replicaID,
		stopCh:     make(chan struct{}),
		onOp:       onOp,
		reconnect:  3 * time.Second,
	}
}

// Start connects to the master and begins receiving replication ops.
func (rc *ReplicaClient) Start() {
	go rc.run()
}

func (rc *ReplicaClient) run() {
	for {
		select {
		case <-rc.stopCh:
			return
		default:
		}
		if err := rc.connectAndReceive(); err != nil {
			log.Printf("ReplicaClient %s: connection to %s lost: %v, reconnecting in %v",
				rc.replicaID, rc.masterAddr, err, rc.reconnect)
		}
		select {
		case <-rc.stopCh:
			return
		case <-time.After(rc.reconnect):
		}
	}
}

func (rc *ReplicaClient) connectAndReceive() error {
	conn, err := net.DialTimeout("tcp", rc.masterAddr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	rc.mu.Lock()
	if rc.conn != nil {
		rc.conn.Close()
	}
	rc.conn = conn
	rc.mu.Unlock()

	// Send registration: REGREPLICA <replica-id>
	regCmd := fmt.Sprintf("*2\r\n$10\r\nREGREPLICA\r\n$%d\r\n%s\r\n", len(rc.replicaID), rc.replicaID)
	if _, err := conn.Write([]byte(regCmd)); err != nil {
		conn.Close()
		return fmt.Errorf("register: %w", err)
	}

	reader := bufio.NewReader(conn)
	// Wait for +OK
	line, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return fmt.Errorf("read register response: %w", err)
	}
	if line != "+OK\r\n" {
		conn.Close()
		return fmt.Errorf("unexpected register response: %q", line)
	}

	log.Printf("ReplicaClient %s: registered with master at %s", rc.replicaID, rc.masterAddr)

	log.Printf("ReplicaClient %s: entering receive loop", rc.replicaID)
	// Main receive loop: read REPLAPPLY commands
	return rc.receiveLoop(reader)
}

func (rc *ReplicaClient) receiveLoop(reader *bufio.Reader) error {
	conn := rc.conn
	for {
		select {
		case <-rc.stopCh:
			return nil
		default:
		}

		// Set a read deadline so we can detect dead connections
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		op, err := readReplicationOp(reader)
		if err != nil {
			log.Printf("ReplicaClient %s: read op failed: %v", rc.replicaID, err)
			return err
		}
		if err := rc.onOp(op); err != nil {
			log.Printf("ReplicaClient %s: apply op failed: %v", rc.replicaID, err)
		}
	}
}

func readReplicationOp(reader *bufio.Reader) (Op, error) {
	// Read RESP array header: *8\r\n (REPLAPPLY + 7 data fields)
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
		return Op{}, fmt.Errorf("expected at least 8 fields, got %d", count)
	}

	// Helper: read one bulk string field, return the string value.
	readField := func() (string, error) {
		lenLine, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("read bulk len: %w", err)
		}
		blen := 0
		fmt.Sscanf(lenLine, "$%d\r\n", &blen)
		data := make([]byte, blen+2) // +2 for trailing \r\n
		if _, err := reader.Read(data); err != nil {
			return "", fmt.Errorf("read bulk data: %w", err)
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
		case 0: // "REPLAPPLY" marker - skip
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

// Stop shuts down the replica client.
func (rc *ReplicaClient) Stop() {
	close(rc.stopCh)
	rc.mu.Lock()
	if rc.conn != nil {
		rc.conn.Close()
	}
	rc.mu.Unlock()
}
