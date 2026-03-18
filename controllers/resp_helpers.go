package controllers

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

func getClusterHealthAtAddr(ctx context.Context, addr string) (*clusterHealth, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := setConnDeadline(ctx, conn, 5*time.Second); err != nil {
		return nil, err
	}
	if _, err := conn.Write([]byte("*2\r\n$7\r\nCLUSTER\r\n$4\r\nINFO\r\n")); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	resp, err := parseRESP(reader)
	if err != nil {
		return nil, err
	}
	info, ok := resp.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected cluster info payload type %T", resp)
	}
	health := &clusterHealth{}
	for _, part := range strings.Split(strings.TrimSpace(info), "\r\n") {
		fields := strings.SplitN(part, ":", 2)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "cluster_state":
			health.Status = fields[1]
		case "cluster_known_nodes":
			health.KnownNodes, _ = strconv.Atoi(fields[1])
		case "cluster_slots_assigned":
			health.SlotsAssigned, _ = strconv.Atoi(fields[1])
		}
	}
	return health, nil
}

func getClusterNodesAtAddr(ctx context.Context, addr string) ([]clusterNode, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := setConnDeadline(ctx, conn, 5*time.Second); err != nil {
		return nil, err
	}
	if _, err := conn.Write([]byte("*2\r\n$7\r\nCLUSTER\r\n$5\r\nNODES\r\n")); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	resp, err := parseRESP(reader)
	if err != nil {
		return nil, err
	}
	raw, ok := resp.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected cluster nodes payload type %T", resp)
	}
	var nodes []clusterNode
	for _, line := range strings.Split(strings.TrimSpace(raw), "\r\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		nodes = append(nodes, clusterNode{ID: fields[0], Addr: fields[1], Flags: fields[2]})
	}
	return nodes, nil
}

func sendRESPCommand(ctx context.Context, addr string, command string) error {
	return sendRESPCommandArgs(ctx, addr, strings.Fields(command)...)
}

func sendRESPCommandArgs(ctx context.Context, addr string, args ...string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := setConnDeadline(ctx, conn, 5*time.Second); err != nil {
		return err
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("*%d\r\n", len(args)))
	for _, part := range args {
		builder.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(part), part))
	}
	if _, err := conn.Write([]byte(builder.String())); err != nil {
		return err
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if strings.HasPrefix(line, "-") {
		return fmt.Errorf("command error: %s", strings.TrimSpace(line))
	}
	return nil
}

func getNodeSlotsAtAddr(ctx context.Context, addr string) ([]int, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := setConnDeadline(ctx, conn, 5*time.Second); err != nil {
		return nil, err
	}
	if _, err := conn.Write([]byte("*2\r\n$7\r\nCLUSTER\r\n$5\r\nSLOTS\r\n")); err != nil {
		return nil, err
	}
	return parseNodeSlots(bufio.NewReader(conn))
}

func getNodeIDAtAddr(ctx context.Context, addr string) (string, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if err := setConnDeadline(ctx, conn, 5*time.Second); err != nil {
		return "", err
	}
	if _, err := conn.Write([]byte("*2\r\n$7\r\nCLUSTER\r\n$4\r\nMYID\r\n")); err != nil {
		return "", err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(line, "$") {
		idLine, _ := reader.ReadString('\n')
		return strings.TrimSpace(idLine), nil
	}
	if strings.HasPrefix(line, "+") {
		return strings.TrimPrefix(strings.TrimSpace(line), "+"), nil
	}
	return "", fmt.Errorf("unexpected response: %s", line)
}

func getKeysInSlotAtAddr(ctx context.Context, addr string, slot int, count int) ([]string, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := setConnDeadline(ctx, conn, 5*time.Second); err != nil {
		return nil, err
	}
	cmd := fmt.Sprintf("*4\r\n$7\r\nCLUSTER\r\n$13\r\nGETKEYSINSLOT\r\n$%d\r\n%d\r\n$%d\r\n%d\r\n", len(strconv.Itoa(slot)), slot, len(strconv.Itoa(count)), count)
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(line, "-") {
		return nil, fmt.Errorf("error: %s", strings.TrimSpace(line))
	}
	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("unexpected response: %s", line)
	}
	countStr := strings.TrimPrefix(strings.TrimSpace(line), "*")
	keyCount, _ := strconv.Atoi(countStr)
	keys := make([]string, 0, keyCount)
	for range keyCount {
		line, err := reader.ReadString('\n')
		if err != nil {
			return keys, err
		}
		if strings.HasPrefix(line, "$") {
			keyLine, _ := reader.ReadString('\n')
			keys = append(keys, strings.TrimSpace(keyLine))
		}
	}
	return keys, nil
}
