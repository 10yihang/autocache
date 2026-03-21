package replication

func (m *Manager) IncrementalOps(slot uint16, appliedLSN uint64) ([]Op, bool) {
	if m == nil || m.logs == nil {
		return nil, false
	}
	if !m.logs.CanReplayFromLSN(slot, appliedLSN) {
		return nil, false
	}
	return m.logs.ListAfter(slot, appliedLSN), true
}
