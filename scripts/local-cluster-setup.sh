#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/bench-common.sh"

# ── Usage ────────────────────────────────────────────────────────────────────
usage() {
    echo "Usage: $0 [options]"
    echo ""
    echo "Options:"
    echo "  -m, --masters N        Number of master nodes (1-16, default: 3)"
    echo "  -r, --replicas N        Number of replicas per master (0-4, default: 0)"
    echo "  -a, --admin [PORT]      Enable built-in admin web UI on PORT (default: 8080)"
    echo "  -h, --help              Show this help"
    echo ""
    echo "Examples:"
    echo "  $0                              # 3 masters, 0 replicas (3 nodes total)"
    echo "  $0 -m 3 -r 1                    # 3 masters + 3 replicas (6 nodes total)"
    echo "  $0 -m 3 -r 2                    # 3 masters + 6 replicas (9 nodes total)"
    echo "  $0 -m 3 -r 1 -a                 # 3M+3R with admin UI on :8080"
    echo "  $0 -m 3 -r 1 --admin 9090       # 3M+3R with admin UI on :9090"
    exit 1
}

MASTERS=3
REPLICAS=0
ADMIN_ENABLED=false
ADMIN_PORT=8080

while [[ $# -gt 0 ]]; do
    case "$1" in
        -m|--masters)
            MASTERS="$2"
            shift 2
            ;;
        -r|--replicas)
            REPLICAS="$2"
            shift 2
            ;;
        -a|--admin)
            ADMIN_ENABLED=true
            # Optional port argument (only if next arg looks like a port number)
            if [[ "${2:-}" =~ ^[0-9]+$ ]] && [ "${2:-}" -ge 1 ] && [ "${2:-}" -le 65535 ]; then
                ADMIN_PORT="$2"
                shift
            fi
            shift
            ;;
        -h|--help)
            usage
            ;;
        *)
            # Backward compat: single numeric arg → masters count
            if [[ "$1" =~ ^[0-9]+$ ]] && [ $# -eq 1 ]; then
                MASTERS="$1"
                REPLICAS=0
                shift
            else
                echo -e "${RED}Unknown option: $1${NC}"
                usage
            fi
            ;;
    esac
done

if ! [[ "$MASTERS" =~ ^[0-9]+$ ]] || [ "$MASTERS" -lt 1 ] || [ "$MASTERS" -gt 16 ]; then
    echo -e "${RED}Masters must be 1-16, got: $MASTERS${NC}"
    usage
fi

if ! [[ "$REPLICAS" =~ ^[0-9]+$ ]] || [ "$REPLICAS" -gt 4 ]; then
    echo -e "${RED}Replicas per master must be 0-4, got: $REPLICAS${NC}"
    usage
fi

TOTAL_NODES=$((MASTERS + MASTERS * REPLICAS))
BASE_PORT=7001
BASE_CLUSTER_PORT=17001
BASE_METRICS_PORT=19001
DATA_PREFIX="/tmp/autocache-cluster"

# ── Build node arrays ────────────────────────────────────────────────────────
AC_PORTS=()
AC_CLUSTER_PORTS=()
AC_METRICS_PORTS=()
AC_DATA_DIRS=()
AC_ROLES=()       # "master" or "replica"
AC_MASTER_IDX=()  # for replicas: which master index (0-based) they belong to

idx=0
for m in $(seq 0 $((MASTERS - 1))); do
    AC_PORTS+=($((BASE_PORT + idx)))
    AC_CLUSTER_PORTS+=($((BASE_CLUSTER_PORT + idx)))
    AC_METRICS_PORTS+=($((BASE_METRICS_PORT + idx)))
    AC_DATA_DIRS+=("${DATA_PREFIX}-node$((idx + 1))")
    AC_ROLES+=("master")
    AC_MASTER_IDX+=("$m")
    idx=$((idx + 1))

    for r in $(seq 1 $REPLICAS); do
        AC_PORTS+=($((BASE_PORT + idx)))
        AC_CLUSTER_PORTS+=($((BASE_CLUSTER_PORT + idx)))
        AC_METRICS_PORTS+=($((BASE_METRICS_PORT + idx)))
        AC_DATA_DIRS+=("${DATA_PREFIX}-node$((idx + 1))")
        AC_ROLES+=("replica")
        AC_MASTER_IDX+=("$m")
        idx=$((idx + 1))
    done
done

MASTER_PORTS=()
MASTER_CLUSTER_PORTS=()
REPLICA_PORTS=()
REPLICA_CLUSTER_PORTS=()
REPLICA_MASTER_IDX=()

for i in $(seq 0 $((TOTAL_NODES - 1))); do
    if [ "${AC_ROLES[$i]}" = "master" ]; then
        MASTER_PORTS+=(${AC_PORTS[$i]})
        MASTER_CLUSTER_PORTS+=(${AC_CLUSTER_PORTS[$i]})
    else
        REPLICA_PORTS+=(${AC_PORTS[$i]})
        REPLICA_CLUSTER_PORTS+=(${AC_CLUSTER_PORTS[$i]})
        REPLICA_MASTER_IDX+=(${AC_MASTER_IDX[$i]})
    fi
done

# ── Display config ───────────────────────────────────────────────────────────
echo -e "${CYAN}${BOLD}"
echo "╔══════════════════════════════════════════════════════════════════════╗"
echo "║     AutoCache Local Cluster Setup (${MASTERS}M + ${REPLICAS}R = ${TOTAL_NODES} nodes)       ║"
echo "╚══════════════════════════════════════════════════════════════════════╝"
echo -e "${NC}"
echo "  Masters:       ${MASTERS}"
echo "  Replicas/m:    ${REPLICAS}"
echo "  Total nodes:   ${TOTAL_NODES}"
echo ""

echo -e "${CYAN}Masters:${NC}"
for m in $(seq 0 $((MASTERS - 1))); do
    echo "  Master ${m}: port ${MASTER_PORTS[$m]}  (cluster: ${MASTER_CLUSTER_PORTS[$m]}, metrics: $((BASE_METRICS_PORT + m)))"
done
if [ "$REPLICAS" -gt 0 ]; then
    echo -e "${CYAN}Replicas:${NC}"
    for r in $(seq 0 $((${#REPLICA_PORTS[@]} - 1))); do
        echo "  Replica ${r}: port ${REPLICA_PORTS[$r]} → Master ${REPLICA_MASTER_IDX[$r]}"
    done
fi
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

echo -e "${YELLOW}[1/6] Building AutoCache...${NC}"

if [ "$ADMIN_ENABLED" = true ]; then
    echo "  Building admin frontend assets..."
    npm --prefix admin/frontend install --silent 2>&1 | tail -1 || true
    npm --prefix admin/frontend run build 2>&1 | tail -3 || true
    echo -e "${GREEN}  Admin frontend built${NC}"
fi

go build -o bin/autocache ./cmd/server 2>&1 | tail -3 || true
if [ ! -f bin/autocache ]; then
    echo -e "${RED}Failed to build AutoCache${NC}"; exit 1
fi
echo -e "${GREEN}  Build successful${NC}"
echo ""

# ── [2/6] Pre-flight cleanup ─────────────────────────────────────────────────
echo -e "${YELLOW}[2/6] Cleaning up ports and leftover data...${NC}"

# Kill any processes lingering on our target ports
for i in $(seq 0 $((TOTAL_NODES - 1))); do
    port=${AC_PORTS[$i]}
    pid=$(lsof -ti :${port} 2>/dev/null || true)
    if [ -n "$pid" ]; then
        echo "  Killing stale process on port ${port} (pid ${pid})"
        kill -9 $pid 2>/dev/null || true
    fi
    cport=${AC_CLUSTER_PORTS[$i]}
    pid=$(lsof -ti :${cport} 2>/dev/null || true)
    if [ -n "$pid" ]; then
        echo "  Killing stale process on cluster port ${cport} (pid ${pid})"
        kill -9 $pid 2>/dev/null || true
    fi
    mport=${AC_METRICS_PORTS[$i]}
    pid=$(lsof -ti :${mport} 2>/dev/null || true)
    if [ -n "$pid" ]; then
        echo "  Killing stale process on metrics port ${mport} (pid ${pid})"
        kill -9 $pid 2>/dev/null || true
    fi
done

# Remove leftover data directories and log files
for dir in "${AC_DATA_DIRS[@]}"; do
    rm -rf "$dir"
done
rm -f /tmp/autocache-cluster-node*.log

if [ "$ADMIN_ENABLED" = true ]; then
    pid=$(lsof -ti :${ADMIN_PORT} 2>/dev/null || true)
    if [ -n "$pid" ]; then
        echo "  Killing stale process on admin port ${ADMIN_PORT} (pid ${pid})"
        kill -9 $pid 2>/dev/null || true
    fi
fi

echo -e "${GREEN}  Cleanup done${NC}"
echo ""

# ── [3/6] Start nodes ───────────────────────────────────────────────────────
echo -e "${YELLOW}[3/6] Starting ${TOTAL_NODES} nodes...${NC}"

# Node 0 (first master) starts first, no seeds
mkdir -p "${AC_DATA_DIRS[0]}"

ADMIN_ARGS=""
if [ "$ADMIN_ENABLED" = true ]; then
    ADMIN_ARGS="-admin-enabled -admin-addr 127.0.0.1:${ADMIN_PORT}"
fi
./bin/autocache \
    -addr ":${AC_PORTS[0]}" \
    -cluster-enabled \
    -cluster-port ${AC_CLUSTER_PORTS[0]} \
    -node-id "local-node-0001" \
    -bind "127.0.0.1" \
    -data-dir "${AC_DATA_DIRS[0]}" \
    -metrics-addr ":${AC_METRICS_PORTS[0]}" \
    -quiet-connections \
    ${ADMIN_ARGS} \
    > /tmp/autocache-cluster-node1.log 2>&1 &
AC_PIDS+=($!)
sleep 1

# Remaining nodes join via seeds pointing at node 0's cluster port
SEED_ADDR="127.0.0.1:${AC_CLUSTER_PORTS[0]}"
for i in $(seq 1 $((TOTAL_NODES - 1))); do
    mkdir -p "${AC_DATA_DIRS[$i]}"
    ./bin/autocache \
        -addr ":${AC_PORTS[$i]}" \
        -cluster-enabled \
        -cluster-port ${AC_CLUSTER_PORTS[$i]} \
        -node-id "$(printf 'local-node-%04d' $((i + 1)))" \
        -bind "127.0.0.1" \
        -seeds "${SEED_ADDR}" \
        -data-dir "${AC_DATA_DIRS[$i]}" \
        -metrics-addr ":${AC_METRICS_PORTS[$i]}" \
        -quiet-connections \
        > /tmp/autocache-cluster-node$((i + 1)).log 2>&1 &
    AC_PIDS+=($!)
    sleep 0.5
done

# Wait for all nodes to accept connections
echo "  Waiting for nodes to start..."
for i in $(seq 0 $((TOTAL_NODES - 1))); do
    port=${AC_PORTS[$i]}
    role=${AC_ROLES[$i]}
    ok=false
    for attempt in $(seq 1 15); do
        if redis-cli -p ${port} ping >/dev/null 2>&1; then
            ok=true
            break
        fi
        sleep 0.5
    done
    node_label="Node $((i + 1))"
    if [ "${ok}" = true ]; then
        echo -e "    ${node_label} (port ${port}, ${role}): ${GREEN}OK${NC}"
    else
        echo -e "    ${node_label} (port ${port}, ${role}): ${RED}FAILED${NC}"
        echo "    Log: /tmp/autocache-cluster-node$((i + 1)).log"
        exit 1
    fi
done
echo ""

# ── [4/6] Assign slots (masters only) ────────────────────────────────────────
echo -e "${YELLOW}[4/6] Assigning 16384 slots across ${MASTERS} masters...${NC}"

TOTAL_SLOTS=16384
SLOTS_PER_MASTER=$((TOTAL_SLOTS / MASTERS))

for m in $(seq 0 $((MASTERS - 1))); do
    start=$((m * SLOTS_PER_MASTER))
    if [ $m -eq $((MASTERS - 1)) ]; then
        end=$((TOTAL_SLOTS - 1))
    else
        end=$(((m + 1) * SLOTS_PER_MASTER - 1))
    fi
    port=${MASTER_PORTS[$m]}
    redis-cli -p ${port} cluster addslots $(seq ${start} ${end} | tr '\n' ' ') >/dev/null 2>&1
    echo -e "    Master ${m} (port ${port}): slots ${GREEN}${start}-${end}${NC} ($((end - start + 1)) slots)"
done

# Wait for gossip to propagate slot info
sleep 2
echo ""

if [ "$REPLICAS" -gt 0 ]; then
    echo -e "${YELLOW}[5/6] Configuring replication (${REPLICAS} replica(s) per master)...${NC}"

    # Resolve master node IDs
    MASTER_IDS=()
    for m in $(seq 0 $((MASTERS - 1))); do
        mid=$(redis-cli -p ${MASTER_PORTS[$m]} cluster myid 2>/dev/null | tr -d '\r\n' || echo "")
        if [ -z "$mid" ]; then
            echo -e "    ${RED}Failed to get node ID for Master ${m} (port ${MASTER_PORTS[$m]})${NC}"
            exit 1
        fi
        MASTER_IDS+=("$mid")
    done

    # Set up each replica
    for r in $(seq 0 $((${#REPLICA_PORTS[@]} - 1))); do
        rport=${REPLICA_PORTS[$r]}
        midx=${REPLICA_MASTER_IDX[$r]}
        mid=${MASTER_IDS[$midx]}

        result=$(redis-cli -p ${rport} cluster replicate "$mid" 2>&1 || true)
        if echo "$result" | grep -qi "OK"; then
            echo -e "    Replica (port ${rport}) → Master ${midx} (${mid}): ${GREEN}REPLICATED${NC}"
        else
            echo -e "    Replica (port ${rport}) → Master ${midx} (${mid}): ${RED}FAILED: ${result}${NC}"
        fi
        sleep 0.3
    done

    sleep 1
    echo ""
    STEP=6
else
    STEP=5
fi

echo -e "${YELLOW}[${STEP}/6] Cluster status${NC}"
echo ""

echo -e "${CYAN}── CLUSTER INFO ──${NC}"
redis-cli -p ${AC_PORTS[0]} cluster info 2>/dev/null | grep -E "cluster_state|cluster_slots_assigned|cluster_known_nodes|cluster_size" | tr -d '\r'
echo ""
echo -e "${CYAN}── CLUSTER NODES ──${NC}"
redis-cli -p ${AC_PORTS[0]} cluster nodes 2>/dev/null | tr -d '\r'
echo ""

echo -e "${CYAN}${BOLD}"
echo "╔══════════════════════════════════════════════════════════════════════╗"
echo "║                     Cluster is ready!                              ║"
echo "╚══════════════════════════════════════════════════════════════════════╝"
echo -e "${NC}"

echo -e "${CYAN}Connect to masters:${NC}"
for m in $(seq 0 $((MASTERS - 1))); do
    echo "  redis-cli -p ${MASTER_PORTS[$m]}"
done

if [ "$REPLICAS" -gt 0 ]; then
    echo ""
    echo -e "${CYAN}Connect to replicas (read-only):${NC}"
    for r in $(seq 0 $((${#REPLICA_PORTS[@]} - 1))); do
        echo "  redis-cli -p ${REPLICA_PORTS[$r]}"
    done
fi

echo ""
echo "Quick test:"
echo "  redis-cli -p ${AC_PORTS[0]} set hello world"
echo "  redis-cli -p ${AC_PORTS[0]} get hello"
if [ "$REPLICAS" -gt 0 ] && [ "${#REPLICA_PORTS[@]}" -gt 0 ]; then
    echo "  redis-cli -p ${REPLICA_PORTS[0]} get hello    # read from replica"
fi
echo "  redis-cli -p ${AC_PORTS[0]} cluster nodes     # view cluster topology"
if [ "$ADMIN_ENABLED" = true ]; then
    echo ""
    echo -e "${CYAN}Admin UI:${NC} http://localhost:${ADMIN_PORT}"
fi
echo ""
echo "Logs:"
for i in $(seq 0 $((TOTAL_NODES - 1))); do
    echo "  Node $((i + 1)) (${AC_ROLES[$i]}): /tmp/autocache-cluster-node$((i + 1)).log"
done
echo ""
SETUP_OK=true
echo -e "${YELLOW}Press Ctrl+C to stop all nodes and clean up.${NC}"

# Keep script alive so trap fires on Ctrl+C
wait
