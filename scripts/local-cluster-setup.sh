#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/bench-common.sh"

# ── Usage ────────────────────────────────────────────────────────────────────
usage() {
    echo "Usage: $0 <n>"
    echo ""
    echo "  n  Number of cluster nodes to start (1-16)"
    echo ""
    echo "Examples:"
    echo "  $0 3    # Start a 3-node cluster on ports 7001-7003"
    echo "  $0 6    # Start a 6-node cluster on ports 7001-7006"
    exit 1
}

if [ $# -lt 1 ] || ! [[ "$1" =~ ^[0-9]+$ ]] || [ "$1" -lt 1 ] || [ "$1" -gt 16 ]; then
    usage
fi

N=$1
BASE_PORT=7001
BASE_CLUSTER_PORT=17001
BASE_METRICS_PORT=19001
DATA_PREFIX="/tmp/autocache-cluster"

# Build arrays
AC_PORTS=()
AC_CLUSTER_PORTS=()
AC_METRICS_PORTS=()
AC_DATA_DIRS=()
for i in $(seq 0 $((N - 1))); do
    AC_PORTS+=($((BASE_PORT + i)))
    AC_CLUSTER_PORTS+=($((BASE_CLUSTER_PORT + i)))
    AC_METRICS_PORTS+=($((BASE_METRICS_PORT + i)))
    AC_DATA_DIRS+=("${DATA_PREFIX}-node$((i + 1))")
done

echo -e "${CYAN}${BOLD}"
echo "╔══════════════════════════════════════════════════════════════════════╗"
echo "║              AutoCache Local Cluster Setup (${N} nodes)                ║"
echo "╚══════════════════════════════════════════════════════════════════════╝"
echo -e "${NC}"
echo "  Nodes:         ${N}"
echo "  RESP ports:    ${AC_PORTS[*]}"
echo "  Cluster ports: ${AC_CLUSTER_PORTS[*]}"
echo "  Metrics ports: ${AC_METRICS_PORTS[*]}"
echo ""

AC_PIDS=()

SETUP_OK=false

cleanup() {
    echo ""
    echo -e "${YELLOW}Shutting down cluster...${NC}"
    for pid in "${AC_PIDS[@]+"${AC_PIDS[@]}"}"; do
        kill "$pid" 2>/dev/null || true
    done
    sleep 1
    for dir in "${AC_DATA_DIRS[@]}"; do
        rm -rf "$dir"
    done
    if [ "${SETUP_OK}" = true ]; then
        rm -f /tmp/autocache-cluster-node*.log
    else
        echo -e "${YELLOW}Logs preserved at /tmp/autocache-cluster-node*.log${NC}"
    fi
    echo -e "${GREEN}Cleanup complete.${NC}"
}
trap cleanup EXIT

cd "$(dirname "$0")/.."

# ── [1/4] Build ──────────────────────────────────────────────────────────────
echo -e "${YELLOW}[1/4] Building AutoCache...${NC}"
go build -o bin/autocache ./cmd/server 2>&1 | tail -3 || true
if [ ! -f bin/autocache ]; then
    echo -e "${RED}Failed to build AutoCache${NC}"; exit 1
fi
echo -e "${GREEN}  Build successful${NC}"
echo ""

# ── [2/4] Start nodes ───────────────────────────────────────────────────────
echo -e "${YELLOW}[2/4] Starting ${N} nodes...${NC}"

# Node 0 starts first (no seeds)
mkdir -p "${AC_DATA_DIRS[0]}"
./bin/autocache \
    -addr ":${AC_PORTS[0]}" \
    -cluster-enabled \
    -cluster-port ${AC_CLUSTER_PORTS[0]} \
    -node-id "local-node-0001" \
    -bind "127.0.0.1" \
    -data-dir "${AC_DATA_DIRS[0]}" \
    -metrics-addr ":${AC_METRICS_PORTS[0]}" \
    -quiet-connections \
    > /tmp/autocache-cluster-node1.log 2>&1 &
AC_PIDS+=($!)
sleep 1

# Remaining nodes join via seeds pointing at node 0's cluster port
for i in $(seq 1 $((N - 1))); do
    mkdir -p "${AC_DATA_DIRS[$i]}"
    ./bin/autocache \
        -addr ":${AC_PORTS[$i]}" \
        -cluster-enabled \
        -cluster-port ${AC_CLUSTER_PORTS[$i]} \
        -node-id "$(printf 'local-node-%04d' $((i + 1)))" \
        -bind "127.0.0.1" \
        -seeds "127.0.0.1:${AC_CLUSTER_PORTS[0]}" \
        -data-dir "${AC_DATA_DIRS[$i]}" \
        -metrics-addr ":${AC_METRICS_PORTS[$i]}" \
        -quiet-connections \
        > /tmp/autocache-cluster-node$((i + 1)).log 2>&1 &
    AC_PIDS+=($!)
    sleep 0.5
done

# Wait for all nodes to accept connections
echo "  Waiting for nodes to start..."
for i in $(seq 0 $((N - 1))); do
    port=${AC_PORTS[$i]}
    ok=false
    for attempt in $(seq 1 10); do
        if redis-cli -p ${port} ping >/dev/null 2>&1; then
            ok=true
            break
        fi
        sleep 0.5
    done
    if [ "${ok}" = true ]; then
        echo -e "    Node $((i + 1)) (port ${port}): ${GREEN}OK${NC}"
    else
        echo -e "    Node $((i + 1)) (port ${port}): ${RED}FAILED${NC}"
        echo "    Log: /tmp/autocache-cluster-node$((i + 1)).log"
        exit 1
    fi
done
echo ""

# ── [3/4] Assign slots ──────────────────────────────────────────────────────
echo -e "${YELLOW}[3/4] Assigning 16384 slots across ${N} nodes...${NC}"

TOTAL_SLOTS=16384
SLOTS_PER_NODE=$((TOTAL_SLOTS / N))

for i in $(seq 0 $((N - 1))); do
    start=$((i * SLOTS_PER_NODE))
    if [ $i -eq $((N - 1)) ]; then
        # Last node gets all remaining slots
        end=$((TOTAL_SLOTS - 1))
    else
        end=$(((i + 1) * SLOTS_PER_NODE - 1))
    fi
    port=${AC_PORTS[$i]}
    redis-cli -p ${port} cluster addslots $(seq ${start} ${end} | tr '\n' ' ') >/dev/null 2>&1
    echo -e "    Node $((i + 1)) (port ${port}): slots ${GREEN}${start}-${end}${NC} ($((end - start + 1)) slots)"
done

sleep 1
echo ""

# ── [4/4] Status ─────────────────────────────────────────────────────────────
echo -e "${YELLOW}[4/4] Cluster status${NC}"
echo ""
redis-cli -p ${AC_PORTS[0]} cluster info 2>/dev/null | grep -E "cluster_state|cluster_slots_assigned|cluster_known_nodes" | tr -d '\r'
echo ""
redis-cli -p ${AC_PORTS[0]} cluster nodes 2>/dev/null | tr -d '\r'
echo ""

echo -e "${CYAN}${BOLD}"
echo "╔══════════════════════════════════════════════════════════════════════╗"
echo "║                     Cluster is ready!                              ║"
echo "╚══════════════════════════════════════════════════════════════════════╝"
echo -e "${NC}"
echo "Connect with:"
for i in $(seq 0 $((N - 1))); do
    echo "  redis-cli -p ${AC_PORTS[$i]}"
done
echo ""
echo "Quick test:"
echo "  redis-cli -p ${AC_PORTS[0]} set hello world"
echo "  redis-cli -p ${AC_PORTS[0]} get hello"
echo ""
echo "Logs:"
for i in $(seq 0 $((N - 1))); do
    echo "  Node $((i + 1)): /tmp/autocache-cluster-node$((i + 1)).log"
done
echo ""
SETUP_OK=true
echo -e "${YELLOW}Press Ctrl+C to stop all nodes and clean up.${NC}"

# Keep script alive so trap fires on Ctrl+C
wait
