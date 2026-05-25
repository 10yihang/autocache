package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:6379", "server address")
	clients := flag.Int("c", 50, "concurrent clients")
	requests := flag.Int("n", 100000, "total requests per test")
	dsize := flag.Int("d", 3, "data size")
	flag.Parse()

	val := make([]byte, *dsize)
	for i := range val {
		val[i] = 'x'
	}

	tests := []struct {
		name string
		req  func(clientID, seq int) string
	}{
		{"PING", func(_, _ int) string {
			return "*1\r\n$4\r\nPING\r\n"
		}},
		{"SET", func(cid, seq int) string {
			return fmt.Sprintf("*3\r\n$3\r\nSET\r\n$16\r\nkey:%d:%08d\r\n$%d\r\n%s\r\n",
				cid, seq, len(val), val)
		}},
		{"GET", func(cid, seq int) string {
			return fmt.Sprintf("*2\r\n$3\r\nGET\r\n$16\r\nkey:%d:%08d\r\n",
				cid, seq)
		}},
		{"INCR", func(cid, seq int) string {
			return fmt.Sprintf("*2\r\n$4\r\nINCR\r\n$16\r\ncntr:%d:%08d\r\n",
				cid, seq)
		}},
	}

	for _, test := range tests {
		rps := runTest(test.name, test.req, *addr, *clients, *requests)
		fmt.Printf("  %-6s: %10.0f requests/sec\n", test.name, rps)
	}
}

func runTest(name string, reqFn func(int, int) string, addr string, clients, totalReqs int) float64 {
	reqsPerClient := totalReqs / clients
	var totalOps atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()

	for c := 0; c < clients; c++ {
		wg.Add(1)
		go func(cid int) {
			defer wg.Done()
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "dial error: %v\n", err)
				return
			}
			defer conn.Close()

			reader := bufio.NewReaderSize(conn, 65536)
			buf := make([]byte, 0, 256)

			for seq := 0; seq < reqsPerClient; seq++ {
				req := reqFn(cid, seq)
				if _, err := conn.Write([]byte(req)); err != nil {
					return
				}

				// Read response line
				line, err := reader.ReadBytes('\n')
				if err != nil {
					return
				}
				buf = append(buf[:0], line...)

				// Handle bulk strings (read content line)
				if len(line) > 0 && line[0] == '$' {
					_, err = reader.ReadBytes('\n')
					if err != nil {
						return
					}
				}

				totalOps.Add(1)
			}
		}(c)
	}

	wg.Wait()
	return float64(totalOps.Load()) / time.Since(start).Seconds()
}
