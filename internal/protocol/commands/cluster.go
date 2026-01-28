package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tidwall/redcon"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/hash"
	"github.com/10yihang/autocache/pkg/bytes"
	"github.com/10yihang/autocache/pkg/protocolbuf"
)

type ClusterHandler struct {
	cluster *cluster.Cluster
}

func NewClusterHandler(c *cluster.Cluster) *ClusterHandler {
	return &ClusterHandler{cluster: c}
}

func (h *ClusterHandler) HandleCluster(conn redcon.Conn, args [][]byte) {
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
	case "SETSLOT":
		h.clusterSetSlot(conn, args[1:])
	case "MYID":
		h.clusterMyID(conn)
	case "GETKEYSINSLOT":
		h.clusterGetKeysInSlot(conn, args[1:])
	case "COUNTKEYSINSLOT":
		h.clusterCountKeysInSlot(conn, args[1:])
	default:
		conn.WriteError("ERR unknown subcommand '" + subcmd + "'")
	}
}

func (h *ClusterHandler) clusterInfo(conn redcon.Conn) {
	info := h.cluster.GetClusterInfo()

	buf := protocolbuf.GetBuffer()
	defer protocolbuf.PutBuffer(buf)
	for k, v := range info {
		buf.WriteString(fmt.Sprintf("%s:%v\r\n", k, v))
	}

	conn.WriteBulk(buf.Bytes())
}

func (h *ClusterHandler) clusterNodes(conn redcon.Conn) {
	nodes := h.cluster.GetNodes()
	self := h.cluster.GetSelf()
	slotMgr := h.cluster.GetSlotManager()

	buf := protocolbuf.GetBuffer()
	defer protocolbuf.PutBuffer(buf)
	for _, node := range nodes {
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

		slotRanges := h.buildNodeSlotRanges(node.ID, slotMgr)

		line := fmt.Sprintf("%s %s:%d@%d %s %s %d %d 0 %s %s\n",
			node.ID,
			node.IP, node.Port, node.ClusterPort,
			flags,
			masterID,
			node.PingSent,
			node.PongReceived,
			linkState,
			slotRanges,
		)
		buf.WriteString(line)
	}

	conn.WriteBulk(buf.Bytes())
}

func (h *ClusterHandler) buildNodeSlotRanges(nodeID string, slotMgr *cluster.SlotManager) string {
	slots := slotMgr.GetNodeSlots(nodeID)
	if len(slots) == 0 {
		return ""
	}

	sortSlots(slots)

	var parts []string

	if len(slots) > 0 {
		start := slots[0]
		end := slots[0]
		for i := 1; i < len(slots); i++ {
			if slots[i] == end+1 {
				end = slots[i]
			} else {
				parts = append(parts, formatSlotRange(start, end))
				start = slots[i]
				end = slots[i]
			}
		}
		parts = append(parts, formatSlotRange(start, end))
	}

	for _, slot := range slots {
		info := slotMgr.GetSlotInfo(slot)
		if info == nil {
			continue
		}
		if info.State == cluster.SlotStateExporting && info.Exporting != "" {
			parts = append(parts, fmt.Sprintf("[%d->-%s]", slot, info.Exporting))
		} else if info.State == cluster.SlotStateImporting && info.Importing != "" {
			parts = append(parts, fmt.Sprintf("[%d-<-%s]", slot, info.Importing))
		}
	}

	return strings.Join(parts, " ")
}

func formatSlotRange(start, end uint16) string {
	if start == end {
		return strconv.FormatUint(uint64(start), 10)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

func sortSlots(slots []uint16) {
	for i := 0; i < len(slots)-1; i++ {
		for j := i + 1; j < len(slots); j++ {
			if slots[i] > slots[j] {
				slots[i], slots[j] = slots[j], slots[i]
			}
		}
	}
}

func (h *ClusterHandler) clusterSlots(conn redcon.Conn) {
	ranges := h.cluster.GetClusterSlots()

	conn.WriteArray(len(ranges))
	for _, r := range ranges {
		node := h.cluster.GetSlotNode(r.Start)
		if node == nil {
			continue
		}

		conn.WriteArray(3)
		conn.WriteInt64(int64(r.Start))
		conn.WriteInt64(int64(r.End))

		conn.WriteArray(3)
		conn.WriteBulkString(node.IP)
		conn.WriteInt(node.Port)
		conn.WriteBulkString(node.ID)
	}
}

func (h *ClusterHandler) clusterKeySlot(conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'cluster keyslot' command")
		return
	}

	slot := hash.KeySlot(bytes.BytesToString(args[0]))
	conn.WriteInt64(int64(slot))
}

func (h *ClusterHandler) clusterMeet(conn redcon.Conn, args [][]byte) {
	if len(args) < 2 {
		conn.WriteError("ERR wrong number of arguments for 'cluster meet' command")
		return
	}

	ip := bytes.BytesToString(args[0])
	port, err := strconv.Atoi(bytes.BytesToString(args[1]))
	if err != nil {
		conn.WriteError("ERR Invalid port")
		return
	}

	clusterPort := port + 10000
	if len(args) >= 3 {
		clusterPort, _ = strconv.Atoi(string(args[2]))
	}

	addr := fmt.Sprintf("%s:%d", ip, clusterPort)
	if err := h.cluster.Meet(addr); err != nil {
		conn.WriteError("ERR " + err.Error())
		return
	}

	conn.WriteString("OK")
}

func (h *ClusterHandler) clusterAddSlots(conn redcon.Conn, args [][]byte) {
	if len(args) == 0 {
		conn.WriteError("ERR wrong number of arguments for 'cluster addslots' command")
		return
	}

	slots := make([]uint16, 0, len(args))
	for _, arg := range args {
		slot, err := strconv.ParseUint(bytes.BytesToString(arg), 10, 16)
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

func (h *ClusterHandler) clusterDelSlots(conn redcon.Conn, args [][]byte) {
	conn.WriteString("OK")
}

func (h *ClusterHandler) clusterSetSlot(conn redcon.Conn, args [][]byte) {
	if len(args) < 2 {
		conn.WriteError("ERR wrong number of arguments for 'cluster setslot' command")
		return
	}

	slot, err := strconv.ParseUint(bytes.BytesToString(args[0]), 10, 16)
	if err != nil || slot >= hash.SlotCount {
		conn.WriteError("ERR Invalid slot")
		return
	}

	subcmd := strings.ToUpper(bytes.BytesToString(args[1]))
	slotMgr := h.cluster.GetSlotManager()

	switch subcmd {
	case "MIGRATING":
		if len(args) < 3 {
			conn.WriteError("ERR wrong number of arguments for 'cluster setslot migrating' command")
			return
		}
		targetNodeID := bytes.BytesToString(args[2])
		slotMgr.SetExporting(uint16(slot), targetNodeID)
		h.cluster.IncrementEpoch()
		conn.WriteString("OK")

	case "IMPORTING":
		if len(args) < 3 {
			conn.WriteError("ERR wrong number of arguments for 'cluster setslot importing' command")
			return
		}
		sourceNodeID := bytes.BytesToString(args[2])
		slotMgr.SetImporting(uint16(slot), sourceNodeID)
		h.cluster.IncrementEpoch()
		conn.WriteString("OK")

	case "NODE":
		if len(args) < 3 {
			conn.WriteError("ERR wrong number of arguments for 'cluster setslot node' command")
			return
		}
		newOwnerID := bytes.BytesToString(args[2])
		slotMgr.FinishMigration(uint16(slot), newOwnerID)
		h.cluster.IncrementEpoch()
		conn.WriteString("OK")

	case "STABLE":
		slotMgr.SetStable(uint16(slot))
		conn.WriteString("OK")

	default:
		conn.WriteError("ERR Invalid CLUSTER SETSLOT action or number of arguments")
	}
}

func (h *ClusterHandler) clusterMyID(conn redcon.Conn) {
	self := h.cluster.GetSelf()
	conn.WriteBulkString(self.ID)
}

func (h *ClusterHandler) clusterGetKeysInSlot(conn redcon.Conn, args [][]byte) {
	if len(args) != 2 {
		conn.WriteError("ERR wrong number of arguments for 'cluster getkeysinslot' command")
		return
	}
	conn.WriteArray(0)
}

func (h *ClusterHandler) clusterCountKeysInSlot(conn redcon.Conn, args [][]byte) {
	if len(args) != 1 {
		conn.WriteError("ERR wrong number of arguments for 'cluster countkeysinslot' command")
		return
	}
	conn.WriteInt(0)
}

func (h *ClusterHandler) CheckKeyRouting(key string) error {
	node, err := h.cluster.RouteKey(key)
	if err == nil {
		return nil
	}

	slot := h.cluster.GetKeySlot(key)

	if err == cluster.ErrMoved && node != nil {
		return &cluster.ClusterError{
			Type: "MOVED",
			Slot: slot,
			Addr: node.Addr(),
		}
	}

	if err == cluster.ErrAsk && node != nil {
		return &cluster.ClusterError{
			Type: "ASK",
			Slot: slot,
			Addr: node.Addr(),
		}
	}

	return err
}

func (h *ClusterHandler) GetCluster() *cluster.Cluster {
	return h.cluster
}
