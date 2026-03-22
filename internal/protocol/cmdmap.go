package protocol

import (
	"context"

	"github.com/tidwall/redcon"
)

// CommandHandler is the function signature for command handlers.
type CommandHandler func(ctx context.Context, conn redcon.Conn, args [][]byte)

// cmdEntry holds a command name and its handler for the lookup table.
type cmdEntry struct {
	name    []byte
	handler CommandHandler
}

// cmdMap is a hash-based command lookup table.
// Uses a simple open-addressing hash table for fast lookups.
type cmdMap struct {
	buckets [128]cmdEntry // Power of 2 for fast modulo
	h       *Handler
}

// newCmdMap creates a new command map from a Handler.
func newCmdMap(h *Handler) *cmdMap {
	cm := &cmdMap{h: h}
	cm.registerAll()
	return cm
}

func (cm *cmdMap) registerAll() {
	// Core commands
	cm.register([]byte("PING"), cm.h.cmdPing)
	cm.register([]byte("ECHO"), cm.h.cmdEcho)
	cm.register([]byte("QUIT"), cm.h.cmdQuit)
	cm.register([]byte("COMMAND"), cm.h.cmdCommand)
	cm.register([]byte("INFO"), cm.h.cmdInfo)

	// String commands
	cm.register([]byte("GET"), cm.h.cmdGet)
	cm.register([]byte("SET"), cm.h.cmdSet)
	cm.register([]byte("SETNX"), cm.h.cmdSetNX)
	cm.register([]byte("SETEX"), cm.h.cmdSetEX)
	cm.register([]byte("PSETEX"), cm.h.cmdPSetEX)
	cm.register([]byte("MGET"), cm.h.cmdMGet)
	cm.register([]byte("MSET"), cm.h.cmdMSet)
	cm.register([]byte("HGET"), cm.h.cmdHGet)
	cm.register([]byte("HSET"), cm.h.cmdHSet)
	cm.register([]byte("HDEL"), cm.h.cmdHDel)
	cm.register([]byte("HEXISTS"), cm.h.cmdHExists)
	cm.register([]byte("HGETALL"), cm.h.cmdHGetAll)
	cm.register([]byte("HKEYS"), cm.h.cmdHKeys)
	cm.register([]byte("HVALS"), cm.h.cmdHVals)
	cm.register([]byte("HLEN"), cm.h.cmdHLen)
	cm.register([]byte("LPUSH"), cm.h.cmdLPush)
	cm.register([]byte("RPUSH"), cm.h.cmdRPush)
	cm.register([]byte("LPOP"), cm.h.cmdLPop)
	cm.register([]byte("RPOP"), cm.h.cmdRPop)
	cm.register([]byte("LRANGE"), cm.h.cmdLRange)
	cm.register([]byte("LLEN"), cm.h.cmdLLen)
	cm.register([]byte("LINDEX"), cm.h.cmdLIndex)
	cm.register([]byte("LSET"), cm.h.cmdLSet)
	cm.register([]byte("LTRIM"), cm.h.cmdLTrim)
	cm.register([]byte("SADD"), cm.h.cmdSAdd)
	cm.register([]byte("SREM"), cm.h.cmdSRem)
	cm.register([]byte("SMEMBERS"), cm.h.cmdSMembers)
	cm.register([]byte("SISMEMBER"), cm.h.cmdSIsMember)
	cm.register([]byte("SCARD"), cm.h.cmdSCard)
	cm.register([]byte("ZADD"), cm.h.cmdZAdd)
	cm.register([]byte("ZREM"), cm.h.cmdZRem)
	cm.register([]byte("ZRANGE"), cm.h.cmdZRange)
	cm.register([]byte("ZSCORE"), cm.h.cmdZScore)
	cm.register([]byte("ZRANK"), cm.h.cmdZRank)
	cm.register([]byte("ZCARD"), cm.h.cmdZCard)
	cm.register([]byte("INCR"), cm.h.cmdIncr)
	cm.register([]byte("INCRBY"), cm.h.cmdIncrBy)
	cm.register([]byte("DECR"), cm.h.cmdDecr)
	cm.register([]byte("DECRBY"), cm.h.cmdDecrBy)
	cm.register([]byte("APPEND"), cm.h.cmdAppend)
	cm.register([]byte("STRLEN"), cm.h.cmdStrlen)
	cm.register([]byte("GETSET"), cm.h.cmdGetSet)

	// Key commands
	cm.register([]byte("DEL"), cm.h.cmdDel)
	cm.register([]byte("EXISTS"), cm.h.cmdExists)
	cm.register([]byte("KEYS"), cm.h.cmdKeys)
	cm.register([]byte("TYPE"), cm.h.cmdType)
	cm.register([]byte("RENAME"), cm.h.cmdRename)
	cm.register([]byte("DBSIZE"), cm.h.cmdDBSize)
	cm.register([]byte("FLUSHDB"), cm.h.cmdFlushDB)
	cm.register([]byte("FLUSHALL"), cm.h.cmdFlushDB)

	// TTL commands
	cm.register([]byte("EXPIRE"), cm.h.cmdExpire)
	cm.register([]byte("EXPIREAT"), cm.h.cmdExpireAt)
	cm.register([]byte("PEXPIRE"), cm.h.cmdPExpire)
	cm.register([]byte("TTL"), cm.h.cmdTTL)
	cm.register([]byte("PTTL"), cm.h.cmdPTTL)
	cm.register([]byte("PERSIST"), cm.h.cmdPersist)

	// Admin commands
	cm.register([]byte("DEBUG"), cm.h.cmdDebug)
	cm.register([]byte("CONFIG"), cm.h.cmdConfig)
	cm.register([]byte("CLIENT"), cm.h.cmdClient)
	cm.register([]byte("ASKING"), cm.h.cmdAsking)
	cm.register([]byte("RESTORE"), cm.h.cmdRestore)
	cm.register([]byte("MIGRATE"), cm.h.cmdMigrate)
	cm.register([]byte("REPLAPPLY"), cm.h.cmdReplApply)
	cm.register([]byte("WAIT"), cm.h.cmdWait)
}

func (cm *cmdMap) register(name []byte, handler CommandHandler) {
	hash := HashBytes(name)
	idx := hash & 127 // len(buckets) - 1

	// Linear probing for collision resolution
	for i := 0; i < 128; i++ {
		pos := (idx + uint32(i)) & 127
		if cm.buckets[pos].name == nil {
			cm.buckets[pos] = cmdEntry{name: name, handler: handler}
			return
		}
	}
	// Should never happen with 64 buckets and ~35 commands
	panic("cmdMap overflow")
}

// Lookup finds a command handler by name.
// The name should already be uppercase.
// Returns nil if command not found.
func (cm *cmdMap) Lookup(name []byte) CommandHandler {
	hash := HashBytes(name)
	idx := hash & 127

	// Linear probing
	for i := 0; i < 128; i++ {
		pos := (idx + uint32(i)) & 127
		entry := &cm.buckets[pos]
		if entry.name == nil {
			return nil
		}
		if BytesEqual(entry.name, name) {
			return entry.handler
		}
	}
	return nil
}

// Common command byte slices for fast comparison
var (
	cmdGET     = []byte("GET")
	cmdSET     = []byte("SET")
	cmdDEL     = []byte("DEL")
	cmdPING    = []byte("PING")
	cmdINCR    = []byte("INCR")
	cmdCLUSTER = []byte("CLUSTER")
)

// IsClusterCmd checks if command bytes equal "CLUSTER"
func IsClusterCmd(cmd []byte) bool {
	return len(cmd) == 7 &&
		upperTable[cmd[0]] == 'C' &&
		upperTable[cmd[1]] == 'L' &&
		upperTable[cmd[2]] == 'U' &&
		upperTable[cmd[3]] == 'S' &&
		upperTable[cmd[4]] == 'T' &&
		upperTable[cmd[5]] == 'E' &&
		upperTable[cmd[6]] == 'R'
}
