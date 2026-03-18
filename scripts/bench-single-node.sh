#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/bench-common.sh"

# Check if redis-benchmark is installed
require_cmd redis-benchmark
require_cmd kubectl
require_cmd nc

# Get the cluster name from the sample yaml
CLUSTER_NAME=$(sample_cluster_name)

echo -e "${GREEN}[1/3] Setting up Port Forwarding (${CLUSTER_NAME}-0)...${NC}"
# Kill any existing port-forward on 6379
pkill -f "kubectl port-forward" || true

# Start port-forward to a specific pod to ensure stable connection
kubectl port-forward pod/${CLUSTER_NAME}-0 6379:6379 >/dev/null 2>&1 &
PF_PID=$!

# Wait for port to be open
echo "Waiting for port 6379..."
for i in {1..10}; do
    if nc -z localhost 6379; then
        break
    fi
    sleep 1
done

if ! nc -z localhost 6379; then
    echo -e "${RED}Failed to establish port forwarding.${NC}"
    kill $PF_PID
    exit 1
fi

echo -e "${GREEN}[2/3] Running Redis Benchmark...${NC}"
echo -e "${YELLOW}Testing PING (Latency)...${NC}"
redis-benchmark -p 6379 -t ping -n 10000 -q

echo -e "${YELLOW}Testing SET/GET (Throughput)...${NC}"
TAG=$(same_slot_tag single)
echo -e "${YELLOW}Using hash tag ${TAG} to route all keys to same slot (avoids MOVED errors)${NC}"
redis-benchmark -p 6379 -n 100000 -q \
    -c 50 \
    SET "${TAG}:__rand_int__" __data__
redis-benchmark -p 6379 -n 100000 -q \
    -c 50 \
    GET "${TAG}:__rand_int__"

echo ""
echo -e "${YELLOW}Testing MSET/MGET (Multi-key, same slot)...${NC}"
redis-benchmark -p 6379 -n 10000 -q \
    MSET "${TAG}:a" v1 "${TAG}:b" v2 "${TAG}:c" v3
redis-benchmark -p 6379 -n 10000 -q \
    MGET "${TAG}:a" "${TAG}:b" "${TAG}:c"

echo ""
echo -e "${YELLOW}Testing INCR (Counter operations)...${NC}"
redis-benchmark -p 6379 -n 100000 -q \
    -c 50 \
    INCR "${TAG}:counter"

echo -e "${GREEN}[3/3] Cleanup...${NC}"
kill $PF_PID 2>/dev/null || true
echo "Done."
