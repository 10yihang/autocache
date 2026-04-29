package admin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"net/http"
)

func (h *HTTPHandler) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "ERR_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	var req struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "ERR_BAD_REQUEST", "invalid JSON body")
		return
	}

	if strings.EqualFold(req.Command, "FLUSHALL") || strings.EqualFold(req.Command, "FLUSHDB") {
		if !h.cfg.AllowDangerous {
			jsonError(w, http.StatusForbidden, "ERR_FORBIDDEN",
				"dangerous commands require --admin-allow-dangerous")
			return
		}
	}

	result, err := h.dispatch(req.Command, req.Args)
	if err != nil {
		h.audit.Record(AuditEntry{
			Timestamp:  time.Now(),
			RemoteAddr: r.RemoteAddr,
			User:       basicAuthUser(r),
			Action:     "COMMAND_EXEC",
			Target:     req.Command,
			Result:     err.Error(),
		})
		jsonError(w, http.StatusInternalServerError, "ERR_DISPATCH", err.Error())
		return
	}

	h.audit.Record(AuditEntry{
		Timestamp:  time.Now(),
		RemoteAddr: r.RemoteAddr,
		User:       basicAuthUser(r),
		Action:     "COMMAND_EXEC",
		Target:     req.Command,
		Result:     "ok",
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"command": req.Command,
		"result":  result,
	})
}

func (h *HTTPHandler) dispatch(cmd string, args []string) (string, error) {
	// Encode as RESP.
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("*%d\r\n", len(args)+1))
	buf.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(cmd), cmd))
	for _, arg := range args {
		buf.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg))
	}

	conn, err := net.DialTimeout("tcp", h.cfg.RedisAddr, 3*time.Second)
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := conn.Write([]byte(buf.String())); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}

	return readRESPResponse(conn)
}

func readRESPResponse(conn net.Conn) (string, error) {
	reader := bufio.NewReader(conn)
	return readRESP(reader, conn)
}

func readRESP(reader *bufio.Reader, conn net.Conn) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return "", fmt.Errorf("empty response")
	}

	switch line[0] {
	case '+':
		return line[1:], nil
	case '-':
		return "", fmt.Errorf("%s", line[1:])
	case ':':
		return line[1:], nil
	case '$':
		length, err := strconv.Atoi(line[1:])
		if err != nil {
			return "", fmt.Errorf("bad bulk string length: %s", line)
		}
		if length < 0 {
			return "(nil)", nil
		}
		data := make([]byte, length+2)
		if _, err := io.ReadFull(reader, data); err != nil {
			return "", fmt.Errorf("read bulk data: %w", err)
		}
		return string(data[:length]), nil
	case '*':
		count, err := strconv.Atoi(line[1:])
		if err != nil {
			return "", fmt.Errorf("bad array length: %s", line)
		}
		if count < 0 {
			return "(nil)", nil
		}
		var parts []string
		for i := 0; i < count; i++ {
			part, err := readRESP(reader, conn)
			if err != nil {
				return "", err
			}
			parts = append(parts, part)
		}
		if len(parts) == 0 {
			return "(empty array)", nil
		}
		return strings.Join(parts, "\n"), nil
	default:
		return line, nil
	}
}
