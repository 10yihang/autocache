package admin

import (
	"net/http"
	"time"
)

func (h *HTTPHandler) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	h.audit.Record(AuditEntry{
		Timestamp:  time.Now(),
		RemoteAddr: r.RemoteAddr,
		User:       basicAuthUser(r),
		Action:     "COMMAND_EXEC",
		Target:     "/api/v1/command",
		Result:     "501: blocked — protocol.Handler.Execute requires redcon.Conn (connection-bound dispatch)",
	})

	jsonError(w, http.StatusNotImplemented, "ERR_NOT_IMPLEMENTED",
		"command execution is not available via the admin API; "+
			"protocol.Handler.Execute/ExecuteBytes write directly to a redcon.Conn and "+
			"cannot be invoked in-process without a wire connection; "+
			"use redis-cli or a RESP client instead")
}
