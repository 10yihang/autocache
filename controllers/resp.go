package controllers

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func parseRESP(reader *bufio.Reader) (any, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}

	switch prefix {
	case '*':
		line, err := readRESPLine(reader)
		if err != nil {
			return nil, err
		}
		count, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("resp array length: %w", err)
		}
		if count == -1 {
			return nil, nil
		}
		if count < -1 {
			return nil, fmt.Errorf("resp array length negative: %d", count)
		}
		items := make([]any, 0, count)
		for range count {
			item, err := parseRESP(reader)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	case ':':
		line, err := readRESPLine(reader)
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("resp integer: %w", err)
		}
		return n, nil
	case '$':
		line, err := readRESPLine(reader)
		if err != nil {
			return nil, err
		}
		length, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("resp bulk length: %w", err)
		}
		if length == -1 {
			return nil, nil
		}
		if length < -1 {
			return nil, fmt.Errorf("resp bulk length negative: %d", length)
		}
		buf := make([]byte, length+2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return nil, err
		}
		if buf[length] != '\r' || buf[length+1] != '\n' {
			return nil, fmt.Errorf("resp bulk missing terminator")
		}
		return string(buf[:length]), nil
	case '+':
		line, err := readRESPLine(reader)
		if err != nil {
			return nil, err
		}
		return line, nil
	case '-':
		line, err := readRESPLine(reader)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("resp error: %s", line)
	default:
		return nil, fmt.Errorf("resp: unexpected prefix %q", prefix)
	}
}

func readRESPLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(line, "\r\n") {
		return "", fmt.Errorf("resp: invalid line ending")
	}
	return strings.TrimSuffix(line, "\r\n"), nil
}
