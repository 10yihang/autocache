package protocol

import (
	stdbytes "bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/redcon"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/internal/cluster/hotspot"
	"github.com/10yihang/autocache/internal/cluster/replication"
	"github.com/10yihang/autocache/internal/cluster/router"
	"github.com/10yihang/autocache/internal/engine"
	"github.com/10yihang/autocache/internal/engine/memory"
	metrics2 "github.com/10yihang/autocache/internal/metrics"
	"github.com/10yihang/autocache/internal/protocol/commands"
	"github.com/10yihang/autocache/pkg/bytes"
	pkgerrors "github.com/10yihang/autocache/pkg/errors"
	"github.com/10yihang/autocache/pkg/protocolbuf"
)

type CommandFunc func(ctx context.Context, conn redcon.Conn, args [][]byte)

type trackedConn struct {
	redcon.Conn
	hadError bool
}

func (c *trackedConn) WriteError(msg string) {
	c.hadError = true
	c.Conn.WriteError(msg)
}

type Handler struct {
	engine         ProtocolEngine
	commands       map[string]CommandFunc
	cmdMap         *cmdMap
	clusterHandler *commands.ClusterHandler
	clusterEnabled bool
	router         router.Router
	applier        *replication.ReplicaApplier

	hotspotDetector *hotspot.Detector

	writeStateMu  sync.RWMutex
	lastWriteSlot uint16
	lastWriteLSN  uint64
}

func NewHandler(engine ProtocolEngine) *Handler {
	h := &Handler{
		engine:   engine,
		commands: make(map[string]CommandFunc),
		applier:  replication.NewReplicaApplier(),
	}
	h.registerCommands()
	h.cmdMap = newCmdMap(h)
	return h
}

func (h *Handler) SetHotspotDetector(d *hotspot.Detector) {
	h.hotspotDetector = d
}

func (h *Handler) SetCluster(c *cluster.Cluster) {
	h.clusterHandler = commands.NewClusterHandler(c, h.engine)
	h.router = router.NewClusterRouter(c)
	h.clusterEnabled = true
}

func (h *Handler) registerCommands() {
	h.commands["PING"] = h.cmdPing
	h.commands["ECHO"] = h.cmdEcho
	h.commands["QUIT"] = h.cmdQuit
	h.commands["COMMAND"] = h.cmdCommand
	h.commands["INFO"] = h.cmdInfo

	h.commands["GET"] = h.cmdGet
	h.commands["SET"] = h.cmdSet
	h.commands["SETNX"] = h.cmdSetNX
	h.commands["SETEX"] = h.cmdSetEX
	h.commands["PSETEX"] = h.cmdPSetEX
	h.commands["MGET"] = h.cmdMGet
	h.commands["MSET"] = h.cmdMSet
	h.commands["HGET"] = h.cmdHGet
	h.commands["HSET"] = h.cmdHSet
	h.commands["HDEL"] = h.cmdHDel
	h.commands["HEXISTS"] = h.cmdHExists
	h.commands["HGETALL"] = h.cmdHGetAll
	h.commands["HKEYS"] = h.cmdHKeys
	h.commands["HVALS"] = h.cmdHVals
	h.commands["HLEN"] = h.cmdHLen
	h.commands["LPUSH"] = h.cmdLPush
	h.commands["RPUSH"] = h.cmdRPush
	h.commands["LPOP"] = h.cmdLPop
	h.commands["RPOP"] = h.cmdRPop
	h.commands["LRANGE"] = h.cmdLRange
	h.commands["LLEN"] = h.cmdLLen
	h.commands["LINDEX"] = h.cmdLIndex
	h.commands["LSET"] = h.cmdLSet
	h.commands["LTRIM"] = h.cmdLTrim
	h.commands["SADD"] = h.cmdSAdd
	h.commands["SREM"] = h.cmdSRem
	h.commands["SMEMBERS"] = h.cmdSMembers
	h.commands["SISMEMBER"] = h.cmdSIsMember
	h.commands["SCARD"] = h.cmdSCard
	h.commands["ZADD"] = h.cmdZAdd
	h.commands["ZREM"] = h.cmdZRem
	h.commands["ZRANGE"] = h.cmdZRange
	h.commands["ZSCORE"] = h.cmdZScore
	h.commands["ZRANK"] = h.cmdZRank
	h.commands["ZCARD"] = h.cmdZCard
	h.commands["INCR"] = h.cmdIncr
	h.commands["INCRBY"] = h.cmdIncrBy
	h.commands["DECR"] = h.cmdDecr
	h.commands["DECRBY"] = h.cmdDecrBy
	h.commands["APPEND"] = h.cmdAppend
	h.commands["STRLEN"] = h.cmdStrlen
	h.commands["GETSET"] = h.cmdGetSet

	h.commands["DEL"] = h.cmdDel
	h.commands["EXISTS"] = h.cmdExists
	h.commands["KEYS"] = h.cmdKeys
	h.commands["TYPE"] = h.cmdType
	h.commands["RENAME"] = h.cmdRename
	h.commands["DBSIZE"] = h.cmdDBSize
	h.commands["FLUSHDB"] = h.cmdFlushDB
	h.commands["FLUSHALL"] = h.cmdFlushDB

	h.commands["EXPIRE"] = h.cmdExpire
	h.commands["EXPIREAT"] = h.cmdExpireAt
	h.commands["PEXPIRE"] = h.cmdPExpire
	h.commands["TTL"] = h.cmdTTL
	h.commands["PTTL"] = h.cmdPTTL
	h.commands["PERSIST"] = h.cmdPersist

	h.commands["DEBUG"] = h.cmdDebug
	h.commands["CONFIG"] = h.cmdConfig
	h.commands["CLIENT"] = h.cmdClient

	h.commands["ASKING"] = h.cmdAsking
	h.commands["RESTORE"] = h.cmdRestore
	h.commands["MIGRATE"] = h.cmdMigrate
	h.commands["REPLAPPLY"] = h.cmdReplApply
	h.commands["WAIT"] = h.cmdWait
}

func (h *Handler) ExecuteBytes(ctx context.Context, conn redcon.Conn, cmdBytes []byte, args [][]byte) {
	ToUpperInPlace(cmdBytes)
	cmdName := bytes.BytesToString(cmdBytes)
	start := time.Now()
	record := func(success bool) {
		metrics2.RecordCommand(cmdName, time.Since(start), success)
	}

	if IsClusterCmd(cmdBytes) {
		if h.clusterHandler == nil {
			conn.WriteError("ERR This instance has cluster support disabled")
			record(false)
			return
		}
		h.clusterHandler.HandleCluster(conn, args)
		record(true)
		return
	}

	fn := h.cmdMap.Lookup(cmdBytes)
	if fn == nil {
		conn.WriteError("ERR unknown command '" + bytes.BytesToString(cmdBytes) + "'")
		record(false)
		return
	}

	if h.clusterEnabled && h.router != nil && len(args) > 0 {
		cmdStr := bytes.BytesToString(cmdBytes)

		if isMultiKeyCommandBytes(cmdBytes) {
			keys := getKeysForCrossSlotCheck(cmdStr, args)
			if len(keys) > 1 {
				result := h.router.RouteMulti(ctx, keys)
				if !h.handleRouteResult(conn, result) {
					clearAskingFlag(conn)
					return
				}
			} else if len(keys) == 1 {
				state := getConnState(conn)
				result := h.router.Route(ctx, keys[0], state.AskingFlag)
				if !h.handleRouteResult(conn, result) {
					clearAskingFlag(conn)
					return
				}
			}
		} else if h.isKeyCommandBytes(cmdBytes) {
			state := getConnState(conn)
			result := h.router.Route(ctx, args[0], state.AskingFlag)
			if !h.handleRouteResult(conn, result) {
				clearAskingFlag(conn)
				return
			}
		}
	}

	if h.clusterEnabled && h.clusterHandler != nil && len(args) > 0 {
		if h.isWriteCommand(cmdName) && !h.canAcceptWrite(cmdName, args) {
			conn.WriteError("READONLY slot is not writable on this node")
			record(false)
			clearAskingFlag(conn)
			return
		}
	}

	tracked := &trackedConn{Conn: conn}
	fn(ctx, tracked, args)
	record(!tracked.hadError)
	h.recordHotspot(args)
	clearAskingFlag(conn)
}

func (h *Handler) Execute(ctx context.Context, conn redcon.Conn, cmd string, args [][]byte) {
	cmd = strings.ToUpper(cmd)
	start := time.Now()
	record := func(success bool) {
		metrics2.RecordCommand(cmd, time.Since(start), success)
	}

	if cmd == "CLUSTER" {
		if h.clusterHandler == nil {
			conn.WriteError("ERR This instance has cluster support disabled")
			record(false)
			return
		}
		h.clusterHandler.HandleCluster(conn, args)
		record(true)
		return
	}

	fn, ok := h.commands[cmd]
	if !ok {
		conn.WriteError("ERR unknown command '" + cmd + "'")
		record(false)
		return
	}

	if h.clusterEnabled && h.router != nil && len(args) > 0 {
		if isMultiKeyCommand(cmd) {
			keys := getKeysForCrossSlotCheck(cmd, args)
			if len(keys) > 1 {
				result := h.router.RouteMulti(ctx, keys)
				if !h.handleRouteResult(conn, result) {
					clearAskingFlag(conn)
					return
				}
			} else if len(keys) == 1 {
				state := getConnState(conn)
				result := h.router.Route(ctx, keys[0], state.AskingFlag)
				if !h.handleRouteResult(conn, result) {
					clearAskingFlag(conn)
					return
				}
			}
		} else if h.isKeyCommand(cmd) {
			state := getConnState(conn)
			result := h.router.Route(ctx, args[0], state.AskingFlag)
			if !h.handleRouteResult(conn, result) {
				clearAskingFlag(conn)
				return
			}
		}
	}

	if h.clusterEnabled && h.clusterHandler != nil && len(args) > 0 {
		if h.isWriteCommand(cmd) && !h.canAcceptWrite(cmd, args) {
			conn.WriteError("READONLY slot is not writable on this node")
			record(false)
			clearAskingFlag(conn)
			return
		}
	}

	tracked := &trackedConn{Conn: conn}
	fn(ctx, tracked, args)
	record(!tracked.hadError)
	h.recordHotspot(args)
	clearAskingFlag(conn)
}

var keyCommands = map[string]bool{
	"GET": true, "SET": true, "SETNX": true, "SETEX": true, "PSETEX": true, "GETSET": true,
	"HGET": true, "HSET": true, "HDEL": true, "HEXISTS": true, "HGETALL": true, "HKEYS": true, "HVALS": true, "HLEN": true,
	"LPUSH": true, "RPUSH": true, "LPOP": true, "RPOP": true, "LRANGE": true, "LLEN": true, "LINDEX": true, "LSET": true, "LTRIM": true,
	"SADD": true, "SREM": true, "SMEMBERS": true, "SISMEMBER": true, "SCARD": true,
	"ZADD": true, "ZREM": true, "ZRANGE": true, "ZSCORE": true, "ZRANK": true, "ZCARD": true,
	"INCR": true, "INCRBY": true, "DECR": true, "DECRBY": true, "APPEND": true, "STRLEN": true,
	"DEL": true, "EXISTS": true, "TYPE": true, "RENAME": true,
	"EXPIRE": true, "EXPIREAT": true, "PEXPIRE": true, "TTL": true, "PTTL": true, "PERSIST": true,
}

var writeCommands = map[string]bool{
	"SET": true, "SETNX": true, "SETEX": true, "PSETEX": true, "GETSET": true,
	"MSET": true, "HSET": true, "HDEL": true,
	"LPUSH": true, "RPUSH": true, "LPOP": true, "RPOP": true, "LSET": true, "LTRIM": true,
	"SADD": true, "SREM": true,
	"ZADD": true, "ZREM": true,
	"INCR": true, "INCRBY": true, "DECR": true, "DECRBY": true, "APPEND": true,
	"DEL": true, "RENAME": true,
	"EXPIRE": true, "EXPIREAT": true, "PEXPIRE": true, "PERSIST": true,
	"RESTORE": true, "MIGRATE": true,
}

var errReplicationNoop = errors.New("replication noop")

func (h *Handler) isKeyCommand(cmd string) bool {
	return keyCommands[cmd]
}

func (h *Handler) isKeyCommandBytes(cmd []byte) bool {
	return keyCommands[bytes.BytesToString(cmd)]
}

func (h *Handler) isWriteCommand(cmd string) bool {
	return writeCommands[cmd]
}

func (h *Handler) handleRouteResult(conn redcon.Conn, result router.RouteResult) bool {
	if result.CrossSlot {
		conn.WriteError("CROSSSLOT Keys in request don't hash to the same slot")
		return false
	}
	if result.Redirect != nil {
		r := result.Redirect
		switch r.Type {
		case router.RedirectMoved:
			conn.WriteError(fmt.Sprintf("MOVED %d %s", r.Slot, r.Addr))
		case router.RedirectAsk:
			conn.WriteError(fmt.Sprintf("ASK %d %s", r.Slot, r.Addr))
		}
		return false
	}
	return result.Local
}

func (h *Handler) canAcceptWrite(cmd string, args [][]byte) bool {
	if h.clusterHandler == nil || h.clusterHandler.GetCluster() == nil {
		return true
	}
	keys := getKeysForCrossSlotCheck(cmd, args)
	if len(keys) == 0 && len(args) > 0 && h.isKeyCommand(cmd) {
		keys = [][]byte{args[0]}
	}
	if len(keys) == 0 {
		return true
	}
	return h.clusterHandler.GetCluster().CanAcceptWrite(bytes.BytesToString(keys[0]))
}

func (h *Handler) recordHotspot(args [][]byte) {
	if h.hotspotDetector == nil || len(args) == 0 {
		return
	}
	key := bytes.BytesToString(args[0])
	slot := hash.KeySlot(key)

	size := 0
	for _, a := range args {
		size += len(a)
	}
	h.hotspotDetector.Record(slot, size)
}

func (h *Handler) applyReplicationWrite(ctx context.Context, key string, opType string, payload []byte, expireAtNs int64, apply func() error) error {
	if h.clusterHandler == nil || h.clusterHandler.GetCluster() == nil {
		return apply()
	}

	coordinator := h.clusterHandler.GetCluster().GetReplicationManager()
	if coordinator == nil {
		return apply()
	}

	slot := h.clusterHandler.GetCluster().GetKeySlot(key)
	op, err := coordinator.ApplyWrite(ctx, replication.WriteRequest{
		Slot:       slot,
		OpType:     opType,
		Key:        key,
		Payload:    payload,
		ExpireAtNs: expireAtNs,
		Apply:      apply,
	})
	if err == nil {
		h.writeStateMu.Lock()
		h.lastWriteSlot = slot
		h.lastWriteLSN = op.LSN
		h.writeStateMu.Unlock()
	}
	return err
}

func encodeReplicationPayload(args [][]byte) []byte {
	if len(args) == 0 {
		return nil
	}
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = bytes.BytesToString(arg)
	}
	return []byte(strings.Join(parts, "\x00"))
}

func decodeReplicationPayload(payload []byte) []string {
	if len(payload) == 0 {
		return nil
	}
	parts := stdbytes.Split(payload, []byte{0})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, string(part))
	}
	return out
}

func (h *Handler) applyIncomingReplicationOp(ctx context.Context, op replication.Op) error {
	if h.applier == nil {
		h.applier = replication.NewReplicaApplier()
	}
	skip, err := h.applier.Begin(op)
	if err != nil {
		return err
	}
	if skip {
		return nil
	}

	payload := decodeReplicationPayload(op.Payload)
	err = nil
	switch op.OpType {
	case "SET":
		if len(payload) == 0 {
			return fmt.Errorf("invalid SET payload")
		}
		value := payload[0]
		if len(payload) > 1 {
			value = payload[1]
		}
		var ttl time.Duration
		if op.ExpireAtNs > 0 {
			ttl = time.Until(time.Unix(0, op.ExpireAtNs))
			if ttl < 0 {
				ttl = 0
			}
		}
		err = h.engine.Set(ctx, op.Key, value, ttl)
	case "MSET":
		if len(payload) == 0 || len(payload)%2 != 0 {
			return fmt.Errorf("invalid MSET payload")
		}
		pairs := make([]interface{}, 0, len(payload))
		for _, part := range payload {
			pairs = append(pairs, part)
		}
		err = h.engine.MSet(ctx, pairs...)
	case "HSET":
		if len(payload) < 3 || len(payload)%2 == 0 {
			return fmt.Errorf("invalid HSET payload")
		}
		pairs := make([]interface{}, 0, len(payload)-1)
		for _, part := range payload[1:] {
			pairs = append(pairs, part)
		}
		_, err = h.engine.HSet(ctx, op.Key, pairs...)
	case "DEL":
		if len(payload) == 0 {
			return fmt.Errorf("invalid DEL payload")
		}
		_, err = h.engine.Del(ctx, payload...)
	case "RENAME":
		if len(payload) != 2 {
			return fmt.Errorf("invalid RENAME payload")
		}
		err = h.engine.Rename(ctx, payload[0], payload[1])
	case "EXPIRE", "PEXPIRE", "EXPIREAT":
		if op.ExpireAtNs <= 0 {
			return fmt.Errorf("invalid expire timestamp")
		}
		_, err = h.engine.ExpireAt(ctx, op.Key, time.Unix(0, op.ExpireAtNs))
	case "PERSIST":
		_, err = h.engine.Persist(ctx, op.Key)
	default:
		return fmt.Errorf("unsupported replication op type: %s", op.OpType)
	}
	if err != nil {
		return err
	}
	h.applier.Commit(op)
	return nil
}

func (h *Handler) cmdPing(_ context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteString("PONG")
	} else {
		conn.WriteBulk(args[0])
	}
}

func (h *Handler) cmdEcho(_ context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'echo' command")
		return
	}
	conn.WriteBulk(args[0])
}

func (h *Handler) cmdQuit(_ context.Context, conn redcon.Conn, _ [][]byte) {
	conn.WriteString("OK")
	conn.Close()
}

func (h *Handler) cmdCommand(_ context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) > 0 && strings.ToUpper(bytes.BytesToString(args[0])) == "DOCS" {
		conn.WriteArray(0)
		return
	}
	conn.WriteArray(0)
}

func (h *Handler) cmdInfo(ctx context.Context, conn redcon.Conn, _ [][]byte) {
	statsRaw := h.engine.GetStats()
	size, _ := h.engine.DBSize(ctx)

	var totalOps int64
	if stats, ok := statsRaw.(*memory.Stats); ok {
		totalOps = stats.GetOps.Load() + stats.SetOps.Load() + stats.DelOps.Load()
	}
	buf := protocolbuf.GetBuffer()
	defer protocolbuf.PutBuffer(buf)

	buf.WriteString("# Server\r\n")
	buf.WriteString("autocache_version:0.1.0\r\n")
	buf.WriteString("\r\n# Stats\r\n")
	buf.WriteString("total_connections_received:0\r\n")
	buf.WriteString("total_commands_processed:")
	buf.WriteString(strconv.FormatInt(totalOps, 10))
	buf.WriteString("\r\n")
	buf.WriteString("\r\n# Keyspace\r\n")
	buf.WriteString("db0:keys=")
	buf.WriteString(strconv.FormatInt(size, 10))
	buf.WriteString(",expires=0\r\n")

	if h.clusterEnabled {
		buf.WriteString("\r\n# Cluster\r\n")
		buf.WriteString("cluster_enabled:1\r\n")
	}

	conn.WriteBulk(buf.Bytes())
}

func (h *Handler) cmdGet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'get' command")
		return
	}

	key := bytes.BytesToString(args[0])
	val, err := h.engine.GetBytes(ctx, key)
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteBulk(val)
}

func (h *Handler) cmdSet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) < 2 {
		conn.WriteError("ERR wrong number of arguments for 'set' command")
		return
	}

	key := bytes.BytesToString(args[0])
	value := bytes.BytesToString(args[1])
	var ttl time.Duration
	var nx, xx bool

	for i := 2; i < len(args); i++ {
		opt := strings.ToUpper(bytes.BytesToString(args[i]))
		switch opt {
		case "EX":
			if i+1 >= len(args) {
				conn.WriteError("ERR syntax error")
				return
			}
			secs, err := strconv.ParseInt(bytes.BytesToString(args[i+1]), 10, 64)
			if err != nil {
				conn.WriteError("ERR value is not an integer or out of range")
				return
			}
			ttl = time.Duration(secs) * time.Second
			i++
		case "PX":
			if i+1 >= len(args) {
				conn.WriteError("ERR syntax error")
				return
			}
			ms, err := strconv.ParseInt(bytes.BytesToString(args[i+1]), 10, 64)
			if err != nil {
				conn.WriteError("ERR value is not an integer or out of range")
				return
			}
			ttl = time.Duration(ms) * time.Millisecond
			i++
		case "NX":
			nx = true
		case "XX":
			xx = true
		}
	}

	if nx && xx {
		conn.WriteError("ERR XX and NX options at the same time are not compatible")
		return
	}

	expireAtNs := int64(0)
	if ttl > 0 {
		expireAtNs = time.Now().Add(ttl).UnixNano()
	}
	err := h.applyReplicationWrite(ctx, key, "SET", encodeReplicationPayload(args), expireAtNs, func() error {
		if nx {
			ok, setErr := h.engine.SetNX(ctx, key, value, ttl)
			if setErr != nil {
				return setErr
			}
			if !ok {
				return errReplicationNoop
			}
			return nil
		}
		if xx {
			ok, setErr := h.engine.SetXX(ctx, key, value, ttl)
			if setErr != nil {
				return setErr
			}
			if !ok {
				return errReplicationNoop
			}
			return nil
		}
		return h.engine.Set(ctx, key, value, ttl)
	})
	if errors.Is(err, errReplicationNoop) {
		conn.WriteNull()
		return
	}
	if err != nil {
		h.writeDataError(conn, err)
		return
	}

	conn.WriteString("OK")
}

func (h *Handler) cmdSetNX(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'setnx' command")
		return
	}

	ok, _ := h.engine.SetNX(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[1]), 0)
	if ok {
		conn.WriteInt(1)
	} else {
		conn.WriteInt(0)
	}
}

func (h *Handler) cmdSetEX(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 3 {
		conn.WriteError("ERR wrong number of arguments for 'setex' command")
		return
	}

	secs, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}

	h.engine.Set(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[2]), time.Duration(secs)*time.Second)
	conn.WriteString("OK")
}

func (h *Handler) cmdPSetEX(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 3 {
		conn.WriteError("ERR wrong number of arguments for 'psetex' command")
		return
	}

	ms, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}

	h.engine.Set(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[2]), time.Duration(ms)*time.Millisecond)
	conn.WriteString("OK")
}

func (h *Handler) cmdMGet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'mget' command")
		return
	}

	keys := make([]string, len(args))
	for i, arg := range args {
		keys[i] = bytes.BytesToString(arg)
	}

	results, _ := h.engine.MGetBytes(ctx, keys...)
	conn.WriteArray(len(results))
	for _, v := range results {
		if v == nil {
			conn.WriteNull()
		} else {
			conn.WriteBulk(v)
		}
	}
}

func (h *Handler) cmdMSet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) < 2 || len(args)%2 != 0 {
		conn.WriteError("ERR wrong number of arguments for 'mset' command")
		return
	}

	pairs := make([]any, len(args))
	for i, arg := range args {
		pairs[i] = bytes.BytesToString(arg)
	}

	if err := h.applyReplicationWrite(ctx, bytes.BytesToString(args[0]), "MSET", encodeReplicationPayload(args), 0, func() error {
		return h.engine.MSet(ctx, pairs...)
	}); err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteString("OK")
}

func (h *Handler) cmdHGet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'hget' command")
		return
	}

	value, err := h.engine.HGet(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[1]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteBulkString(value)
}

func (h *Handler) cmdHSet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) < 3 || len(args)%2 == 0 {
		conn.WriteError("ERR wrong number of arguments for 'hset' command")
		return
	}

	pairs := make([]interface{}, 0, len(args)-1)
	for _, arg := range args[1:] {
		pairs = append(pairs, bytes.BytesToString(arg))
	}

	var added int64
	err := h.applyReplicationWrite(ctx, bytes.BytesToString(args[0]), "HSET", encodeReplicationPayload(args), 0, func() error {
		var applyErr error
		added, applyErr = h.engine.HSet(ctx, bytes.BytesToString(args[0]), pairs...)
		return applyErr
	})
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(added)
}

func (h *Handler) cmdHDel(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) < 2 {
		conn.WriteError("ERR wrong number of arguments for 'hdel' command")
		return
	}

	fields := make([]string, 0, len(args)-1)
	for _, arg := range args[1:] {
		fields = append(fields, bytes.BytesToString(arg))
	}

	deleted, err := h.engine.HDel(ctx, bytes.BytesToString(args[0]), fields...)
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(deleted)
}

func (h *Handler) cmdHExists(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'hexists' command")
		return
	}

	exists, err := h.engine.HExists(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[1]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	if exists {
		conn.WriteInt(1)
		return
	}
	conn.WriteInt(0)
}

func (h *Handler) cmdHGetAll(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'hgetall' command")
		return
	}

	values, err := h.engine.HGetAll(ctx, bytes.BytesToString(args[0]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}

	keys := make([]string, 0, len(values))
	for field := range values {
		keys = append(keys, field)
	}
	sort.Strings(keys)

	conn.WriteArray(len(keys) * 2)
	for _, field := range keys {
		conn.WriteBulkString(field)
		conn.WriteBulkString(values[field])
	}
}

func (h *Handler) cmdHKeys(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'hkeys' command")
		return
	}

	keys, err := h.engine.HKeys(ctx, bytes.BytesToString(args[0]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteArray(len(keys))
	for _, field := range keys {
		conn.WriteBulkString(field)
	}
}

func (h *Handler) cmdHVals(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'hvals' command")
		return
	}

	values, err := h.engine.HVals(ctx, bytes.BytesToString(args[0]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteArray(len(values))
	for _, value := range values {
		conn.WriteBulkString(value)
	}
}

func (h *Handler) cmdHLen(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'hlen' command")
		return
	}

	length, err := h.engine.HLen(ctx, bytes.BytesToString(args[0]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(length)
}

func (h *Handler) cmdLPush(ctx context.Context, conn redcon.Conn, args [][]byte) {
	h.cmdPushList(ctx, conn, args, true)
}

func (h *Handler) cmdRPush(ctx context.Context, conn redcon.Conn, args [][]byte) {
	h.cmdPushList(ctx, conn, args, false)
}

func (h *Handler) cmdLPop(ctx context.Context, conn redcon.Conn, args [][]byte) {
	h.cmdPopList(ctx, conn, args, true)
}

func (h *Handler) cmdRPop(ctx context.Context, conn redcon.Conn, args [][]byte) {
	h.cmdPopList(ctx, conn, args, false)
}

func (h *Handler) cmdLRange(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 3 {
		conn.WriteError("ERR wrong number of arguments for 'lrange' command")
		return
	}

	start, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}
	stop, err := strconv.ParseInt(bytes.BytesToString(args[2]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}

	values, err := h.engine.LRange(ctx, bytes.BytesToString(args[0]), start, stop)
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteArray(len(values))
	for _, value := range values {
		conn.WriteBulkString(value)
	}
}

func (h *Handler) cmdLLen(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'llen' command")
		return
	}

	length, err := h.engine.LLen(ctx, bytes.BytesToString(args[0]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(length)
}

func (h *Handler) cmdLIndex(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'lindex' command")
		return
	}

	index, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}

	value, err := h.engine.LIndex(ctx, bytes.BytesToString(args[0]), index)
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteBulkString(value)
}

func (h *Handler) cmdLSet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 3 {
		conn.WriteError("ERR wrong number of arguments for 'lset' command")
		return
	}

	index, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}

	if err := h.engine.LSet(ctx, bytes.BytesToString(args[0]), index, bytes.BytesToString(args[2])); err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteString("OK")
}

func (h *Handler) cmdLTrim(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 3 {
		conn.WriteError("ERR wrong number of arguments for 'ltrim' command")
		return
	}

	start, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}
	stop, err := strconv.ParseInt(bytes.BytesToString(args[2]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}

	if err := h.engine.LTrim(ctx, bytes.BytesToString(args[0]), start, stop); err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteString("OK")
}

func (h *Handler) cmdSAdd(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) < 2 {
		conn.WriteError("ERR wrong number of arguments for 'sadd' command")
		return
	}
	members := make([]interface{}, 0, len(args)-1)
	for _, arg := range args[1:] {
		members = append(members, bytes.BytesToString(arg))
	}
	added, err := h.engine.SAdd(ctx, bytes.BytesToString(args[0]), members...)
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(added)
}

func (h *Handler) cmdSRem(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) < 2 {
		conn.WriteError("ERR wrong number of arguments for 'srem' command")
		return
	}
	members := make([]interface{}, 0, len(args)-1)
	for _, arg := range args[1:] {
		members = append(members, bytes.BytesToString(arg))
	}
	removed, err := h.engine.SRem(ctx, bytes.BytesToString(args[0]), members...)
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(removed)
}

func (h *Handler) cmdSMembers(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'smembers' command")
		return
	}
	members, err := h.engine.SMembers(ctx, bytes.BytesToString(args[0]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteArray(len(members))
	for _, member := range members {
		conn.WriteBulkString(member)
	}
}

func (h *Handler) cmdSIsMember(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'sismember' command")
		return
	}
	exists, err := h.engine.SIsMember(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[1]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	if exists {
		conn.WriteInt(1)
		return
	}
	conn.WriteInt(0)
}

func (h *Handler) cmdSCard(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'scard' command")
		return
	}
	count, err := h.engine.SCard(ctx, bytes.BytesToString(args[0]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(count)
}

func (h *Handler) cmdZAdd(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) < 3 || len(args)%2 == 0 {
		conn.WriteError("ERR wrong number of arguments for 'zadd' command")
		return
	}
	members := make([]engine.ZMember, 0, (len(args)-1)/2)
	for i := 1; i < len(args); i += 2 {
		score, err := strconv.ParseFloat(bytes.BytesToString(args[i]), 64)
		if err != nil {
			conn.WriteError("ERR value is not a valid float")
			return
		}
		members = append(members, engine.ZMember{Score: score, Member: bytes.BytesToString(args[i+1])})
	}
	added, err := h.engine.ZAdd(ctx, bytes.BytesToString(args[0]), members...)
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(added)
}

func (h *Handler) cmdZRem(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) < 2 {
		conn.WriteError("ERR wrong number of arguments for 'zrem' command")
		return
	}
	members := make([]string, 0, len(args)-1)
	for _, arg := range args[1:] {
		members = append(members, bytes.BytesToString(arg))
	}
	removed, err := h.engine.ZRem(ctx, bytes.BytesToString(args[0]), members...)
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(removed)
}

func (h *Handler) cmdZRange(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 3 {
		conn.WriteError("ERR wrong number of arguments for 'zrange' command")
		return
	}
	start, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}
	stop, err := strconv.ParseInt(bytes.BytesToString(args[2]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}
	values, err := h.engine.ZRange(ctx, bytes.BytesToString(args[0]), start, stop)
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteArray(len(values))
	for _, value := range values {
		conn.WriteBulkString(value)
	}
}

func (h *Handler) cmdZScore(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'zscore' command")
		return
	}
	score, err := h.engine.ZScore(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[1]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteBulkString(strconv.FormatFloat(score, 'f', -1, 64))
}

func (h *Handler) cmdZRank(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'zrank' command")
		return
	}
	rank, err := h.engine.ZRank(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[1]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(rank)
}

func (h *Handler) cmdZCard(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'zcard' command")
		return
	}
	count, err := h.engine.ZCard(ctx, bytes.BytesToString(args[0]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(count)
}

func (h *Handler) cmdIncr(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'incr' command")
		return
	}

	val, err := h.engine.Incr(ctx, bytes.BytesToString(args[0]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(val)
}

func (h *Handler) writeDataError(conn redcon.Conn, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, pkgerrors.ErrWrongType) {
		WriteWrongType(conn)
		return
	}
	if errors.Is(err, pkgerrors.ErrKeyNotFound) {
		conn.WriteNull()
		return
	}
	conn.WriteError("ERR " + err.Error())
}

func (h *Handler) cmdPushList(ctx context.Context, conn redcon.Conn, args [][]byte, left bool) {
	if len(args) < 2 {
		conn.WriteError("ERR wrong number of arguments for list push command")
		return
	}
	values := make([]interface{}, 0, len(args)-1)
	for _, arg := range args[1:] {
		values = append(values, bytes.BytesToString(arg))
	}

	var (
		length int64
		err    error
	)
	if left {
		length, err = h.engine.LPush(ctx, bytes.BytesToString(args[0]), values...)
	} else {
		length, err = h.engine.RPush(ctx, bytes.BytesToString(args[0]), values...)
	}
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(length)
}

func (h *Handler) cmdPopList(ctx context.Context, conn redcon.Conn, args [][]byte, left bool) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for list pop command")
		return
	}

	var (
		value string
		err   error
	)
	if left {
		value, err = h.engine.LPop(ctx, bytes.BytesToString(args[0]))
	} else {
		value, err = h.engine.RPop(ctx, bytes.BytesToString(args[0]))
	}
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteBulkString(value)
}

func (h *Handler) cmdIncrBy(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'incrby' command")
		return
	}

	delta, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}

	val, err := h.engine.IncrBy(ctx, bytes.BytesToString(args[0]), delta)
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(val)
}

func (h *Handler) cmdDecr(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'decr' command")
		return
	}

	val, err := h.engine.Decr(ctx, bytes.BytesToString(args[0]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(val)
}

func (h *Handler) cmdDecrBy(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'decrby' command")
		return
	}

	delta, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}

	val, err := h.engine.DecrBy(ctx, bytes.BytesToString(args[0]), delta)
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(val)
}

func (h *Handler) cmdAppend(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'append' command")
		return
	}

	length, err := h.engine.Append(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[1]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(length)
}

func (h *Handler) cmdStrlen(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'strlen' command")
		return
	}

	length, err := h.engine.Strlen(ctx, bytes.BytesToString(args[0]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(length)
}

func (h *Handler) cmdGetSet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'getset' command")
		return
	}

	old, err := h.engine.GetSet(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[1]))
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	if old == "" {
		conn.WriteNull()
		return
	}
	conn.WriteBulkString(old)
}

func (h *Handler) cmdDel(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'del' command")
		return
	}

	keys := make([]string, len(args))
	for i, arg := range args {
		keys[i] = bytes.BytesToString(arg)
	}

	var count int64
	err := h.applyReplicationWrite(ctx, keys[0], "DEL", encodeReplicationPayload(args), 0, func() error {
		var applyErr error
		count, applyErr = h.engine.Del(ctx, keys...)
		return applyErr
	})
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	conn.WriteInt64(count)
}

func (h *Handler) cmdExists(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'exists' command")
		return
	}

	keys := make([]string, len(args))
	for i, arg := range args {
		keys[i] = bytes.BytesToString(arg)
	}

	count, _ := h.engine.Exists(ctx, keys...)
	conn.WriteInt64(count)
}

func (h *Handler) cmdKeys(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'keys' command")
		return
	}

	keys, _ := h.engine.Keys(ctx, bytes.BytesToString(args[0]))
	conn.WriteArray(len(keys))
	for _, key := range keys {
		conn.WriteBulkString(key)
	}
}

func (h *Handler) cmdType(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'type' command")
		return
	}

	t, _ := h.engine.Type(ctx, bytes.BytesToString(args[0]))
	conn.WriteString(t)
}

func (h *Handler) cmdRename(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'rename' command")
		return
	}

	err := h.applyReplicationWrite(ctx, bytes.BytesToString(args[0]), "RENAME", encodeReplicationPayload(args), 0, func() error {
		return h.engine.Rename(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[1]))
	})
	if err != nil {
		conn.WriteError("ERR " + err.Error())
		return
	}
	conn.WriteString("OK")
}

func (h *Handler) cmdDBSize(ctx context.Context, conn redcon.Conn, _ [][]byte) {
	size, _ := h.engine.DBSize(ctx)
	conn.WriteInt64(size)
}

func (h *Handler) cmdFlushDB(ctx context.Context, conn redcon.Conn, _ [][]byte) {
	h.engine.FlushDB(ctx)
	conn.WriteString("OK")
}

func (h *Handler) cmdExpire(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'expire' command")
		return
	}

	secs, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}

	var ok bool
	err = h.applyReplicationWrite(ctx, bytes.BytesToString(args[0]), "EXPIRE", encodeReplicationPayload(args), time.Now().Add(time.Duration(secs)*time.Second).UnixNano(), func() error {
		var applyErr error
		ok, applyErr = h.engine.Expire(ctx, bytes.BytesToString(args[0]), time.Duration(secs)*time.Second)
		return applyErr
	})
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	if ok {
		conn.WriteInt(1)
	} else {
		conn.WriteInt(0)
	}
}

func (h *Handler) cmdExpireAt(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'expireat' command")
		return
	}

	ts, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}

	var ok bool
	err = h.applyReplicationWrite(ctx, bytes.BytesToString(args[0]), "EXPIREAT", encodeReplicationPayload(args), time.Unix(ts, 0).UnixNano(), func() error {
		var applyErr error
		ok, applyErr = h.engine.ExpireAt(ctx, bytes.BytesToString(args[0]), time.Unix(ts, 0))
		return applyErr
	})
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	if ok {
		conn.WriteInt(1)
	} else {
		conn.WriteInt(0)
	}
}

func (h *Handler) cmdPExpire(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'pexpire' command")
		return
	}

	ms, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR value is not an integer or out of range")
		return
	}

	var ok bool
	err = h.applyReplicationWrite(ctx, bytes.BytesToString(args[0]), "PEXPIRE", encodeReplicationPayload(args), time.Now().Add(time.Duration(ms)*time.Millisecond).UnixNano(), func() error {
		var applyErr error
		ok, applyErr = h.engine.Expire(ctx, bytes.BytesToString(args[0]), time.Duration(ms)*time.Millisecond)
		return applyErr
	})
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	if ok {
		conn.WriteInt(1)
	} else {
		conn.WriteInt(0)
	}
}

func (h *Handler) cmdTTL(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'ttl' command")
		return
	}

	ttl, _ := h.engine.TTL(ctx, bytes.BytesToString(args[0]))
	conn.WriteInt64(int64(ttl / time.Second))
}

func (h *Handler) cmdPTTL(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'pttl' command")
		return
	}

	ttl, _ := h.engine.TTL(ctx, bytes.BytesToString(args[0]))
	conn.WriteInt64(int64(ttl / time.Millisecond))
}

func (h *Handler) cmdPersist(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'persist' command")
		return
	}

	var ok bool
	err := h.applyReplicationWrite(ctx, bytes.BytesToString(args[0]), "PERSIST", encodeReplicationPayload(args), 0, func() error {
		var applyErr error
		ok, applyErr = h.engine.Persist(ctx, bytes.BytesToString(args[0]))
		return applyErr
	})
	if err != nil {
		h.writeDataError(conn, err)
		return
	}
	if ok {
		conn.WriteInt(1)
	} else {
		conn.WriteInt(0)
	}
}

func (h *Handler) cmdDebug(_ context.Context, conn redcon.Conn, _ [][]byte) {
	conn.WriteString("OK")
}

func (h *Handler) cmdConfig(_ context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'config' command")
		return
	}

	subcmd := strings.ToUpper(string(args[0]))
	switch subcmd {
	case "GET":
		conn.WriteArray(0)
	case "SET":
		conn.WriteString("OK")
	default:
		conn.WriteError("ERR unknown subcommand")
	}
}

func (h *Handler) cmdClient(_ context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'client' command")
		return
	}

	subcmd := strings.ToUpper(string(args[0]))
	switch subcmd {
	case "SETNAME":
		conn.WriteString("OK")
	case "GETNAME":
		conn.WriteNull()
	case "LIST":
		conn.WriteBulkString("")
	default:
		conn.WriteString("OK")
	}
}

func (h *Handler) cmdAsking(_ context.Context, conn redcon.Conn, _ [][]byte) {
	state := getConnState(conn)
	state.AskingFlag = true
	conn.WriteString("OK")
}

func (h *Handler) cmdRestore(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) < 3 {
		conn.WriteError("ERR wrong number of arguments for 'restore' command")
		return
	}

	key := bytes.BytesToString(args[0])

	ttlMs, err := strconv.ParseInt(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR invalid TTL value")
		return
	}

	serialized := args[2]

	replace := false
	for i := 3; i < len(args); i++ {
		opt := strings.ToUpper(bytes.BytesToString(args[i]))
		if opt == "REPLACE" {
			replace = true
		}
	}

	if !replace {
		exists, err := h.engine.Exists(ctx, key)
		if err == nil && exists > 0 {
			conn.WriteError("BUSYKEY Target key name already exists.")
			return
		}
	}

	if len(serialized) < 1 {
		conn.WriteError("ERR invalid serialized value")
		return
	}

	valueType := engine.ValueType(serialized[0])
	valueData := serialized[1:]

	var ttl time.Duration
	if ttlMs > 0 {
		ttl = time.Duration(ttlMs) * time.Millisecond
	}

	if err := h.engine.RestoreEntry(ctx, key, valueType, valueData, ttl); err != nil {
		conn.WriteError("ERR " + err.Error())
		return
	}
	conn.WriteString("OK")
}

func (h *Handler) cmdMigrate(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) < 5 {
		conn.WriteError("ERR wrong number of arguments for 'migrate' command")
		return
	}

	host := bytes.BytesToString(args[0])
	port := bytes.BytesToString(args[1])
	key := bytes.BytesToString(args[2])

	db, err := strconv.ParseInt(bytes.BytesToString(args[3]), 10, 64)
	if err != nil {
		conn.WriteError("ERR invalid DB index")
		return
	}

	if h.clusterEnabled && db != 0 {
		conn.WriteError("ERR Target DB is not 0 in cluster mode")
		return
	}

	timeout, err := strconv.ParseInt(bytes.BytesToString(args[4]), 10, 64)
	if err != nil {
		conn.WriteError("ERR invalid timeout")
		return
	}

	copyKey := false
	replace := false
	keysMode := false
	keys := make([]string, 0)
	for i := 5; i < len(args); i++ {
		if keysMode {
			keys = append(keys, bytes.BytesToString(args[i]))
			continue
		}

		opt := strings.ToUpper(bytes.BytesToString(args[i]))
		switch opt {
		case "COPY":
			copyKey = true
		case "REPLACE":
			replace = true
		case "KEYS":
			keysMode = true
		}
	}

	if keysMode {
		if key != "" {
			conn.WriteError("ERR key argument must be empty when using KEYS")
			return
		}
		if len(keys) == 0 {
			conn.WriteError("ERR wrong number of arguments for 'migrate' command")
			return
		}
	} else {
		keys = append(keys, key)
	}

	type migrateItem struct {
		key        string
		ttlMs      int64
		serialized []byte
	}

	items := make([]migrateItem, 0, len(keys))
	for _, migrateKey := range keys {
		entry, err := h.engine.GetEntry(ctx, migrateKey)
		if err != nil {
			if keysMode {
				continue
			}
			conn.WriteError("ERR no such key")
			return
		}

		var ttlMs int64
		if !entry.ExpireAt.IsZero() {
			ttlMs = time.Until(entry.ExpireAt).Milliseconds()
			if ttlMs < 0 {
				if keysMode {
					continue
				}
				conn.WriteString("NOKEY")
				return
			}
		}

		serialized, err := memory.SerializeEntryValue(entry)
		if err != nil {
			conn.WriteError("ERR unsupported value type for migration")
			return
		}

		items = append(items, migrateItem{
			key:        migrateKey,
			ttlMs:      ttlMs,
			serialized: serialized,
		})
	}

	if keysMode && len(items) == 0 {
		conn.WriteString("NOKEY")
		return
	}

	targetAddr := net.JoinHostPort(host, port)
	timeoutDuration := time.Duration(timeout) * time.Millisecond
	backoffs := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}

	migrateOnce := func(item migrateItem) (string, bool) {
		targetConn, err := net.DialTimeout("tcp", targetAddr, timeoutDuration)
		if err != nil {
			return fmt.Sprintf("IOERR error or timeout connecting to the target instance: %v", err), true
		}
		defer targetConn.Close()

		if err := targetConn.SetDeadline(time.Now().Add(timeoutDuration)); err != nil {
			return fmt.Sprintf("IOERR error setting target deadline: %v", err), true
		}

		_, err = targetConn.Write([]byte("*1\r\n$6\r\nASKING\r\n"))
		if err != nil {
			return fmt.Sprintf("IOERR error writing to target: %v", err), true
		}

		buf := make([]byte, 1024)
		n, err := targetConn.Read(buf)
		if err != nil {
			return fmt.Sprintf("IOERR error reading from target: %v", err), true
		}
		if string(buf[:n]) != "+OK\r\n" {
			return fmt.Sprintf("ERR target node returned error on ASKING: %s", string(buf[:n])), false
		}

		var restoreCmd string
		keyLen := len(item.key)
		ttlStr := strconv.FormatInt(item.ttlMs, 10)
		serializedLen := len(item.serialized)

		if replace {
			restoreCmd = fmt.Sprintf("*5\r\n$7\r\nRESTORE\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$7\r\nREPLACE\r\n",
				keyLen, item.key, len(ttlStr), ttlStr, serializedLen, item.serialized)
		} else {
			restoreCmd = fmt.Sprintf("*4\r\n$7\r\nRESTORE\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n",
				keyLen, item.key, len(ttlStr), ttlStr, serializedLen, item.serialized)
		}

		_, err = targetConn.Write([]byte(restoreCmd))
		if err != nil {
			return fmt.Sprintf("IOERR error writing RESTORE to target: %v", err), true
		}

		n, err = targetConn.Read(buf)
		if err != nil {
			return fmt.Sprintf("IOERR error reading RESTORE response: %v", err), true
		}

		response := string(buf[:n])
		if response != "+OK\r\n" {
			return fmt.Sprintf("ERR target node returned error: %s", strings.TrimSpace(response)), false
		}

		return "", false
	}

	migrateWithRetry := func(item migrateItem) error {
		for attempt := 0; attempt <= len(backoffs); attempt++ {
			errMsg, retry := migrateOnce(item)
			if errMsg == "" {
				return nil
			}
			if !retry || attempt == len(backoffs) {
				return errors.New(errMsg)
			}
			time.Sleep(backoffs[attempt])
		}
		return nil
	}

	for _, item := range items {
		if err := migrateWithRetry(item); err != nil {
			conn.WriteError(err.Error())
			return
		}

		if !copyKey {
			h.engine.Del(ctx, item.key)
		}
	}

	conn.WriteString("OK")
}

func (h *Handler) cmdReplApply(_ context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 7 {
		conn.WriteError("ERR wrong number of arguments for 'replapply' command")
		return
	}
	if h.clusterHandler == nil || h.clusterHandler.GetCluster() == nil {
		conn.WriteError("ERR cluster is required for replapply")
		return
	}

	slot, err := strconv.ParseUint(bytes.BytesToString(args[0]), 10, 16)
	if err != nil {
		conn.WriteError("ERR invalid slot")
		return
	}
	epoch, err := strconv.ParseUint(bytes.BytesToString(args[1]), 10, 64)
	if err != nil {
		conn.WriteError("ERR invalid epoch")
		return
	}
	lsn, err := strconv.ParseUint(bytes.BytesToString(args[2]), 10, 64)
	if err != nil {
		conn.WriteError("ERR invalid lsn")
		return
	}
	expireAtNs, err := strconv.ParseInt(bytes.BytesToString(args[5]), 10, 64)
	if err != nil {
		conn.WriteError("ERR invalid expireAtNs")
		return
	}

	op := replication.Op{
		Slot:       uint16(slot),
		Epoch:      epoch,
		LSN:        lsn,
		OpType:     bytes.BytesToString(args[3]),
		Key:        bytes.BytesToString(args[4]),
		ExpireAtNs: expireAtNs,
		Payload:    append([]byte(nil), args[6]...),
	}
	if err := h.applyIncomingReplicationOp(context.Background(), op); err != nil {
		conn.WriteError("ERR " + err.Error())
		return
	}
	h.clusterHandler.GetCluster().GetReplicationManager().ReceiveTransportOp(op)
	conn.WriteString("OK")
}

func (h *Handler) cmdWait(_ context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'wait' command")
		return
	}
	required, err := strconv.Atoi(bytes.BytesToString(args[0]))
	if err != nil || required < 0 {
		conn.WriteError("ERR invalid replica count")
		return
	}
	timeoutMs, err := strconv.Atoi(bytes.BytesToString(args[1]))
	if err != nil || timeoutMs < 0 {
		conn.WriteError("ERR invalid timeout")
		return
	}
	if required == 0 {
		conn.WriteInt(0)
		return
	}
	if h.clusterHandler == nil || h.clusterHandler.GetCluster() == nil {
		conn.WriteInt(0)
		return
	}
	mgr := h.clusterHandler.GetCluster().GetReplicationManager()
	if mgr == nil {
		conn.WriteInt(0)
		return
	}

	h.writeStateMu.RLock()
	slot := h.lastWriteSlot
	needLSN := h.lastWriteLSN
	h.writeStateMu.RUnlock()
	if needLSN == 0 {
		conn.WriteInt(0)
		return
	}

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for {
		snapshot := mgr.ReplicaProgressSnapshot(slot)
		ackCount := 0
		for _, ack := range snapshot {
			if ack.AppliedLSN >= needLSN {
				ackCount++
			}
		}
		if ackCount >= required || time.Now().After(deadline) {
			conn.WriteInt(ackCount)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
