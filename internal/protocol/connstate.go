// Package protocol provides Redis protocol handling and connection state management.
package protocol

import "github.com/tidwall/redcon"

// ConnState holds per-connection state for cluster operations.
type ConnState struct {
	// AskingFlag indicates this connection sent ASKING command.
	// Used during slot migration to allow importing node to serve requests.
	// Cleared automatically after each command execution.
	AskingFlag bool
}

// getConnState retrieves or creates connection state from redcon.Conn context.
// Uses redcon.Conn.Context() interface{} for storage.
func getConnState(conn redcon.Conn) *ConnState {
	if ctx := conn.Context(); ctx != nil {
		if state, ok := ctx.(*ConnState); ok {
			return state
		}
	}
	state := &ConnState{}
	conn.SetContext(state)
	return state
}

// clearAskingFlag resets the ASKING flag after command execution.
// Per Redis spec, ASKING is valid for exactly one command.
func clearAskingFlag(conn redcon.Conn) {
	if ctx := conn.Context(); ctx != nil {
		if state, ok := ctx.(*ConnState); ok {
			state.AskingFlag = false
		}
	}
}
