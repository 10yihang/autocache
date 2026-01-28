package protocol

// Fast integer formatting utilities with zero allocations for common cases.

// smallIntStrings caches string representations of 0-99
var smallIntStrings [100][]byte

func init() {
	for i := 0; i < 100; i++ {
		if i < 10 {
			smallIntStrings[i] = []byte{byte('0' + i)}
		} else {
			smallIntStrings[i] = []byte{byte('0' + i/10), byte('0' + i%10)}
		}
	}
}

// FormatInt appends a decimal representation of n to buf and returns the extended buffer.
// This is optimized for RESP protocol integer formatting.
//
// For small positive integers [0, 99], uses a lookup table.
// For other integers, uses manual conversion without strconv.
func FormatInt(buf []byte, n int64) []byte {
	if n >= 0 && n < 100 {
		return append(buf, smallIntStrings[n]...)
	}

	if n < 0 {
		buf = append(buf, '-')
		n = -n
	}

	return appendUint(buf, uint64(n))
}

// appendUint appends unsigned integer to buffer
func appendUint(buf []byte, n uint64) []byte {
	if n == 0 {
		return append(buf, '0')
	}

	// Maximum digits for uint64 is 20
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte(n%10) + '0'
		n /= 10
	}
	return append(buf, digits[i:]...)
}

// FormatUint appends an unsigned integer to buf
func FormatUint(buf []byte, n uint64) []byte {
	if n < 100 {
		return append(buf, smallIntStrings[n]...)
	}
	return appendUint(buf, n)
}

// FormatIntRESP formats an integer as a complete RESP integer response (:N\r\n)
// and appends it to buf.
func FormatIntRESP(buf []byte, n int64) []byte {
	buf = append(buf, ':')
	buf = FormatInt(buf, n)
	buf = append(buf, '\r', '\n')
	return buf
}

// FormatBulkLen appends a RESP bulk string length prefix ($N\r\n) to buf
func FormatBulkLen(buf []byte, length int) []byte {
	buf = append(buf, '$')
	buf = FormatInt(buf, int64(length))
	buf = append(buf, '\r', '\n')
	return buf
}

// FormatArrayLen appends a RESP array length prefix (*N\r\n) to buf
func FormatArrayLen(buf []byte, length int) []byte {
	buf = append(buf, '*')
	buf = FormatInt(buf, int64(length))
	buf = append(buf, '\r', '\n')
	return buf
}

// FormatBulkString appends a complete RESP bulk string ($len\r\ndata\r\n) to buf
func FormatBulkString(buf []byte, s []byte) []byte {
	buf = FormatBulkLen(buf, len(s))
	buf = append(buf, s...)
	buf = append(buf, '\r', '\n')
	return buf
}

// FormatSimpleString appends a RESP simple string (+s\r\n) to buf
func FormatSimpleString(buf []byte, s string) []byte {
	buf = append(buf, '+')
	buf = append(buf, s...)
	buf = append(buf, '\r', '\n')
	return buf
}

// FormatErrorString appends a RESP error (-s\r\n) to buf
func FormatErrorString(buf []byte, s string) []byte {
	buf = append(buf, '-')
	buf = append(buf, s...)
	buf = append(buf, '\r', '\n')
	return buf
}
