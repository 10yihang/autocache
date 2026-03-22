package replication

func (m *Manager) ReplicaProgress(slot uint16, nodeID string) uint64 {
	if m == nil || m.acks == nil {
		return 0
	}
	return m.acks.Get(slot, nodeID)
}

func (m *Manager) ReplicaProgressSnapshot(slot uint16) []ReplicaAck {
	if m == nil || m.acks == nil {
		return nil
	}
	return m.acks.Snapshot(slot)
}
