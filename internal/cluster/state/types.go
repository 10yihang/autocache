package state

// CurrentStateVersion is the schema version for persistent state
const CurrentStateVersion = 1

// PersistentState is the JSON-serializable cluster state
type PersistentState struct {
	Version        int                       `json:"version"`
	NodeID         string                    `json:"node_id"`
	Nodes          []NodeInfo                `json:"nodes"`
	SlotMap        [16384]string             `json:"slot_map"`
	MigratingSlots map[uint16]MigrationState `json:"migrating_slots,omitempty"`
	CurrentEpoch   uint64                    `json:"current_epoch"`
	MyEpoch        uint64                    `json:"my_epoch"`
}

// NodeInfo stores persistent node metadata
type NodeInfo struct {
	ID          string `json:"id"`
	Addr        string `json:"addr"`
	ClusterPort int    `json:"cluster_port"`
	Role        string `json:"role"`
	MasterID    string `json:"master_id,omitempty"`
}

// MigrationState tracks slot migration progress
type MigrationState struct {
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
	State        string `json:"state"`
}
