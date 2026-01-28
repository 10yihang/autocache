package protocol

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/redcon"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/router"
	"github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/protocol/commands"
	"github.com/10yihang/autocache/pkg/bytes"
	"github.com/10yihang/autocache/pkg/protocolbuf"
)

type CommandFunc func(ctx context.Context, conn redcon.Conn, args [][]byte)

type Handler struct {
	engine         ProtocolEngine
	commands       map[string]CommandFunc
	cmdMap         *cmdMap
	clusterHandler *commands.ClusterHandler
	clusterEnabled bool
	router         router.Router
}

func NewHandler(engine ProtocolEngine) *Handler {
	h := &Handler{
		engine:   engine,
		commands: make(map[string]CommandFunc),
	}
	h.registerCommands()
	h.cmdMap = newCmdMap(h)
	return h
}

func (h *Handler) SetCluster(c *cluster.Cluster) {
	h.clusterHandler = commands.NewClusterHandler(c)
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
}

func (h *Handler) ExecuteBytes(ctx context.Context, conn redcon.Conn, cmdBytes []byte, args [][]byte) {
	ToUpperInPlace(cmdBytes)

	if IsClusterCmd(cmdBytes) {
		if h.clusterHandler == nil {
			conn.WriteError("ERR This instance has cluster support disabled")
			return
		}
		h.clusterHandler.HandleCluster(conn, args)
		return
	}

	fn := h.cmdMap.Lookup(cmdBytes)
	if fn == nil {
		conn.WriteError("ERR unknown command '" + bytes.BytesToString(cmdBytes) + "'")
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

	fn(ctx, conn, args)
	clearAskingFlag(conn)
}

func (h *Handler) Execute(ctx context.Context, conn redcon.Conn, cmd string, args [][]byte) {
	if cmd == "CLUSTER" {
		if h.clusterHandler == nil {
			conn.WriteError("ERR This instance has cluster support disabled")
			return
		}
		h.clusterHandler.HandleCluster(conn, args)
		return
	}

	fn, ok := h.commands[cmd]
	if !ok {
		conn.WriteError("ERR unknown command '" + cmd + "'")
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

	fn(ctx, conn, args)
	clearAskingFlag(conn)
}

var keyCommands = map[string]bool{
	"GET": true, "SET": true, "SETNX": true, "SETEX": true, "PSETEX": true, "GETSET": true,
	"INCR": true, "INCRBY": true, "DECR": true, "DECRBY": true, "APPEND": true, "STRLEN": true,
	"DEL": true, "EXISTS": true, "TYPE": true, "RENAME": true,
	"EXPIRE": true, "EXPIREAT": true, "PEXPIRE": true, "TTL": true, "PTTL": true, "PERSIST": true,
}

func (h *Handler) isKeyCommand(cmd string) bool {
	return keyCommands[cmd]
}

func (h *Handler) isKeyCommandBytes(cmd []byte) bool {
	return keyCommands[bytes.BytesToString(cmd)]
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
		conn.WriteNull()
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

	if nx {
		ok, _ := h.engine.SetNX(ctx, key, value, ttl)
		if !ok {
			conn.WriteNull()
			return
		}
	} else if xx {
		ok, _ := h.engine.SetXX(ctx, key, value, ttl)
		if !ok {
			conn.WriteNull()
			return
		}
	} else {
		h.engine.Set(ctx, key, value, ttl)
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

	h.engine.MSet(ctx, pairs...)
	conn.WriteString("OK")
}

func (h *Handler) cmdIncr(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'incr' command")
		return
	}

	val, err := h.engine.Incr(ctx, bytes.BytesToString(args[0]))
	if err != nil {
		conn.WriteError("ERR " + err.Error())
		return
	}
	conn.WriteInt64(val)
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
		conn.WriteError("ERR " + err.Error())
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
		conn.WriteError("ERR " + err.Error())
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
		conn.WriteError("ERR " + err.Error())
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
		conn.WriteError("ERR " + err.Error())
		return
	}
	conn.WriteInt64(length)
}

func (h *Handler) cmdStrlen(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'strlen' command")
		return
	}

	length, _ := h.engine.Strlen(ctx, bytes.BytesToString(args[0]))
	conn.WriteInt64(length)
}

func (h *Handler) cmdGetSet(ctx context.Context, conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'getset' command")
		return
	}

	old, err := h.engine.GetSet(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[1]))
	if err != nil || old == "" {
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

	count, _ := h.engine.Del(ctx, keys...)
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

	err := h.engine.Rename(ctx, bytes.BytesToString(args[0]), bytes.BytesToString(args[1]))
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

	ok, _ := h.engine.Expire(ctx, bytes.BytesToString(args[0]), time.Duration(secs)*time.Second)
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

	ok, _ := h.engine.ExpireAt(ctx, bytes.BytesToString(args[0]), time.Unix(ts, 0))
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

	ok, _ := h.engine.Expire(ctx, bytes.BytesToString(args[0]), time.Duration(ms)*time.Millisecond)
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

	ok, _ := h.engine.Persist(ctx, bytes.BytesToString(args[0]))
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
		_, err := h.engine.GetBytes(ctx, key)
		if err == nil {
			conn.WriteError("BUSYKEY Target key name already exists.")
			return
		}
	}

	if len(serialized) < 1 {
		conn.WriteError("ERR invalid serialized value")
		return
	}

	valueType := serialized[0]
	valueData := serialized[1:]

	if valueType != 0 {
		conn.WriteError("ERR unsupported value type in serialized data")
		return
	}

	var ttl time.Duration
	if ttlMs > 0 {
		ttl = time.Duration(ttlMs) * time.Millisecond
	}

	h.engine.Set(ctx, key, bytes.BytesToString(valueData), ttl)
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

		var valueBytes []byte
		switch v := entry.Value.(type) {
		case []byte:
			valueBytes = v
		case string:
			valueBytes = []byte(v)
		default:
			conn.WriteError("ERR unsupported value type for migration")
			return
		}

		serialized := make([]byte, 1+len(valueBytes))
		serialized[0] = 0
		copy(serialized[1:], valueBytes)

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
