package protocol

import (
	"github.com/tidwall/redcon"
)

// Static RESP responses for common operations.
// These are pre-built byte slices that can be written directly
// without any allocation.
var (
	// Simple string responses
	RespOK   = []byte("+OK\r\n")
	RespPONG = []byte("+PONG\r\n")
	RespNone = []byte("+NONE\r\n")

	// Null responses
	RespNil      = []byte("$-1\r\n")    // Null bulk string
	RespNilArray = []byte("*-1\r\n")    // Null array
	RespEmpty    = []byte("$0\r\n\r\n") // Empty bulk string
	RespEmptyArr = []byte("*0\r\n")     // Empty array

	// Common integer responses
	RespZero   = []byte(":0\r\n")
	RespOne    = []byte(":1\r\n")
	RespNegOne = []byte(":-1\r\n")
	RespNegTwo = []byte(":-2\r\n")

	// Common error prefixes
	ErrPrefix    = []byte("-ERR ")
	ErrWrongType = []byte("-WRONGTYPE Operation against a key holding the wrong kind of value\r\n")
	ErrSyntax    = []byte("-ERR syntax error\r\n")
	ErrNoKey     = []byte("-ERR no such key\r\n")

	// CRLF terminator
	CRLF = []byte("\r\n")
)

// intCache caches RESP-formatted integers from -1 to 100
var intCache [102][]byte

func init() {
	for i := -1; i <= 100; i++ {
		intCache[i+1] = formatIntToBytes(int64(i))
	}
}

// formatIntToBytes formats an integer as RESP integer response
func formatIntToBytes(n int64) []byte {
	// Use manual formatting to avoid strconv allocation
	buf := make([]byte, 0, 24)
	buf = append(buf, ':')
	buf = appendInt(buf, n)
	buf = append(buf, '\r', '\n')
	return buf
}

// appendInt appends an integer to a byte slice without allocation
func appendInt(buf []byte, n int64) []byte {
	if n < 0 {
		buf = append(buf, '-')
		n = -n
	}
	if n == 0 {
		return append(buf, '0')
	}

	// Write digits in reverse
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte(n%10) + '0'
		n /= 10
	}
	return append(buf, digits[i:]...)
}

// WriteOK writes a static OK response
func WriteOK(conn redcon.Conn) {
	conn.WriteRaw(RespOK)
}

// WritePONG writes a static PONG response
func WritePONG(conn redcon.Conn) {
	conn.WriteRaw(RespPONG)
}

// WriteNull writes a null bulk string response
func WriteNull(conn redcon.Conn) {
	conn.WriteRaw(RespNil)
}

// WriteNullArray writes a null array response
func WriteNullArray(conn redcon.Conn) {
	conn.WriteRaw(RespNilArray)
}

// WriteEmptyArray writes an empty array response
func WriteEmptyArray(conn redcon.Conn) {
	conn.WriteRaw(RespEmptyArr)
}

// WriteZero writes a :0 integer response
func WriteZero(conn redcon.Conn) {
	conn.WriteRaw(RespZero)
}

// WriteOne writes a :1 integer response
func WriteOne(conn redcon.Conn) {
	conn.WriteRaw(RespOne)
}

// WriteNegOne writes a :-1 integer response
func WriteNegOne(conn redcon.Conn) {
	conn.WriteRaw(RespNegOne)
}

// WriteNegTwo writes a :-2 integer response
func WriteNegTwo(conn redcon.Conn) {
	conn.WriteRaw(RespNegTwo)
}

// WriteCachedInt writes a cached integer if in range [-1, 100],
// otherwise falls back to conn.WriteInt64.
// Returns true if cached response was used.
func WriteCachedInt(conn redcon.Conn, n int64) bool {
	if n >= -1 && n <= 100 {
		conn.WriteRaw(intCache[n+1])
		return true
	}
	conn.WriteInt64(n)
	return false
}

// WriteError writes an error response with the given message
func WriteError(conn redcon.Conn, msg string) {
	conn.WriteError("ERR " + msg)
}

// WriteSyntaxError writes a syntax error response
func WriteSyntaxError(conn redcon.Conn) {
	conn.WriteRaw(ErrSyntax)
}

// WriteWrongType writes a WRONGTYPE error response
func WriteWrongType(conn redcon.Conn) {
	conn.WriteRaw(ErrWrongType)
}

// WriteNoSuchKey writes an error for non-existent key
func WriteNoSuchKey(conn redcon.Conn) {
	conn.WriteRaw(ErrNoKey)
}
