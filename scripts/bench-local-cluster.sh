#!/usr/bin/env bash
set -uo pipefail

source "$(dirname "$0")/lib/bench-common.sh"

REQUESTS=${1:-$(default_requests)}
CLIENTS=${2:-$(default_clients)}
DATA_SIZE=${3:-$(default_data_size)}

# AutoCache cluster: 3 nodes (7001/7002/7003)
AC_PORTS=(7001 7002 7003)
AC_CLUSTER_PORTS=(17001 17002 17003)
AC_DATA_DIRS=(/tmp/autocache-bench-node1 /tmp/autocache-bench-node2 /tmp/autocache-bench-node3)

# Redis cluster: 3 nodes (7010/7011/7012, Redis cluster 最少 3 主节点)
REDIS_PORTS=(7010 7011 7012)
REDIS_CLUSTER_PORTS=(17010 17011 17012)
REDIS_DATA_DIRS=(/tmp/redis-bench-7010 /tmp/redis-bench-7011 /tmp/redis-bench-7012)

require_cmd redis-benchmark
require_cmd redis-server
require_cmd redis-cli

echo -e "${CYAN}${BOLD}"
echo "╔══════════════════════════════════════════════════════════════════════╗"
echo "║    Local Cluster Benchmark: AutoCache (3-node) vs Redis (3-node)    ║"
echo "╚══════════════════════════════════════════════════════════════════════╝"
echo -e "${NC}"
print_bench_config "${REQUESTS}" "${CLIENTS}" "${DATA_SIZE}"
echo "  Redis cluster:     ports ${REDIS_PORTS[*]}"
echo "  AutoCache cluster: ports ${AC_PORTS[*]}"
echo ""

REDIS_PIDS=()
AC_PIDS=()

cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    for pid in "${REDIS_PIDS[@]+"${REDIS_PIDS[@]}"}"; do
        kill "$pid" 2>/dev/null || true
    done
    for pid in "${AC_PIDS[@]+"${AC_PIDS[@]}"}"; do
        kill "$pid" 2>/dev/null || true
    done
    sleep 1
    rm -rf "${AC_DATA_DIRS[@]}" "${REDIS_DATA_DIRS[@]}"
    rm -f /tmp/autocache-bench-node*.log /tmp/redis-bench-*.log
    echo "Done."
}
trap cleanup EXIT

cd "$(dirname "$0")/.."

# ── [1/5] Build AutoCache ────────────────────────────────────────────────────
echo -e "${YELLOW}[1/5] Building AutoCache...${NC}"
go build -o bin/autocache ./cmd/server 2>&1 | tail -3 || true
if [ ! -f bin/autocache ]; then
    echo -e "${RED}Failed to build AutoCache${NC}"; exit 1
fi
echo "  AutoCache built successfully"

# ── [2/5] Start clusters ─────────────────────────────────────────────────────
echo ""
echo -e "${YELLOW}[2/5] Starting clusters...${NC}"

# Redis cluster（3 节点）
echo "  Starting Redis cluster (3 nodes)..."
for i in 0 1 2; do
    port=${REDIS_PORTS[$i]}
    cport=${REDIS_CLUSTER_PORTS[$i]}
    dir=${REDIS_DATA_DIRS[$i]}
    mkdir -p "$dir"
    redis-server \
        --port $port \
        --cluster-enabled yes \
        --cluster-config-file "$dir/nodes.conf" \
        --cluster-node-timeout 5000 \
        --appendonly no --save "" \
        --loglevel warning --dir "$dir" \
        > /tmp/redis-bench-${port}.log 2>&1 &
    REDIS_PIDS+=($!)
done
sleep 2

# 检查 Redis 节点
for port in "${REDIS_PORTS[@]}"; do
    if ! redis-cli -p $port ping >/dev/null 2>&1; then
        echo -e "${RED}Failed to start Redis on port $port${NC}"
        cat /tmp/redis-bench-${port}.log
        exit 1
    fi
done

# 组建 Redis cluster
REDIS_NODES=$(for p in "${REDIS_PORTS[@]}"; do echo -n "127.0.0.1:$p "; done)
redis-cli --cluster create $REDIS_NODES --cluster-yes >/dev/null 2>&1
sleep 2
echo "  Redis cluster ready (ports: ${REDIS_PORTS[*]})"

# AutoCache cluster（3 节点）
echo "  Starting AutoCache cluster (3 nodes)..."
mkdir -p "${AC_DATA_DIRS[0]}"
./bin/autocache \
    -addr ":${AC_PORTS[0]}" \
    -cluster-enabled \
    -cluster-port ${AC_CLUSTER_PORTS[0]} \
    -node-id "bench-acnode1" \
    -bind "127.0.0.1" \
    -data-dir "${AC_DATA_DIRS[0]}" \
    -metrics-addr ":19001" \
    -quiet-connections \
    > /tmp/autocache-bench-node1.log 2>&1 &
AC_PIDS+=($!)

sleep 1
for i in 1 2; do
    mkdir -p "${AC_DATA_DIRS[$i]}"
    ./bin/autocache \
        -addr ":${AC_PORTS[$i]}" \
        -cluster-enabled \
        -cluster-port ${AC_CLUSTER_PORTS[$i]} \
        -node-id "bench-acnode$((i+1))" \
        -bind "127.0.0.1" \
        -seeds "127.0.0.1:${AC_CLUSTER_PORTS[0]}" \
        -data-dir "${AC_DATA_DIRS[$i]}" \
        -metrics-addr ":1900$((i+1))" \
        -quiet-connections \
        > /tmp/autocache-bench-node$((i+1)).log 2>&1 &
    AC_PIDS+=($!)
    sleep 1
done

sleep 2
for port in "${AC_PORTS[@]}"; do
    if ! redis-cli -p ${port} ping >/dev/null 2>&1; then
        echo -e "${RED}Failed to start AutoCache on port ${port}${NC}"; exit 1
    fi
done

# 分配 AutoCache cluster slots（三等分 0-16383）
redis-cli -p ${AC_PORTS[0]} cluster addslots $(seq 0    5460  | tr '\n' ' ') >/dev/null 2>&1
redis-cli -p ${AC_PORTS[1]} cluster addslots $(seq 5461 10922 | tr '\n' ' ') >/dev/null 2>&1
redis-cli -p ${AC_PORTS[2]} cluster addslots $(seq 10923 16383 | tr '\n' ' ') >/dev/null 2>&1
sleep 1
echo "  AutoCache cluster ready (ports: ${AC_PORTS[*]})"

# ── [3/5] Run benchmarks ─────────────────────────────────────────────────────
echo ""
echo -e "${CYAN}${BOLD}══════════════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}${BOLD}                    LOCAL CLUSTER BENCHMARK RESULTS                   ${NC}"
echo -e "${CYAN}${BOLD}══════════════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${YELLOW}[3/5] Running benchmarks...${NC}"
echo ""

run_bench_cluster() {
    local name=$1 port=$2
    echo -e "${GREEN}--- ${name} (entry: localhost:${port}) ---${NC}"
    safe_bench_output redis-benchmark -p ${port} --cluster \
        -n ${REQUESTS} -c ${CLIENTS} -d ${DATA_SIZE} -q \
        -t ping,set,get,incr | grep -E "PING_INLINE|SET:|GET:|INCR:" || true
    echo ""
}

run_bench_cluster "Redis cluster    (3-node)" ${REDIS_PORTS[0]}
run_bench_cluster "AutoCache cluster (3-node)" ${AC_PORTS[0]}

# ── [4/5] Detailed comparison ────────────────────────────────────────────────
echo -e "${YELLOW}[4/5] Detailed comparison...${NC}"
echo ""

get_rps_cluster() {
    local port=$1 cmd=$2
    local output
    output=$(safe_bench_output redis-benchmark -p ${port} --cluster \
        -n ${REQUESTS} -c ${CLIENTS} -t ${cmd} -q)
    printf '%s\n' "$output" | tr '\r' '\n' | grep 'requests per second' | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1
}

REDIS_PING=$(get_rps_cluster ${REDIS_PORTS[0]} ping)
REDIS_SET=$(get_rps_cluster  ${REDIS_PORTS[0]} set)
REDIS_GET=$(get_rps_cluster  ${REDIS_PORTS[0]} get)
REDIS_INCR=$(get_rps_cluster ${REDIS_PORTS[0]} incr)

AC_PING=$(get_rps_cluster ${AC_PORTS[0]} ping)
AC_SET=$(get_rps_cluster  ${AC_PORTS[0]} set)
AC_GET=$(get_rps_cluster  ${AC_PORTS[0]} get)
AC_INCR=$(get_rps_cluster ${AC_PORTS[0]} incr)

calc_ratio() {
    if [ -n "$1" ] && [ -n "$2" ] && [ "$1" != "0" ] && [ "$1" != "N/A" ] && [ "$2" != "N/A" ]; then
        python3 -c "print(f'{float($2)/float($1):.2f}')" 2>/dev/null || echo "N/A"
    else
        echo "N/A"
    fi
}

echo -e "${CYAN}╔═══════════════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║           PERFORMANCE SUMMARY (requests/sec)                             ║${NC}"
echo -e "${CYAN}╠═══════════╦═══════════════════╦═══════════════════╦═════════════════════╣${NC}"
echo -e "${CYAN}║ Command   ║ Redis cluster     ║ AutoCache cluster ║ Ratio (AC/Redis)    ║${NC}"
echo -e "${CYAN}╠═══════════╬═══════════════════╬═══════════════════╬═════════════════════╣${NC}"

print_row() {
    local cmd=$1 redis=$2 ac=$3
    local ratio
    ratio=$(calc_ratio "$redis" "$ac")
    local color="${NC}"
    if [ "$ratio" != "N/A" ]; then
        is_good=$(echo "$ratio >= 0.5" | bc 2>/dev/null || echo "0")
        [ "$is_good" = "1" ] && color="${GREEN}" || color="${RED}"
    fi
    printf "${CYAN}║${NC} %-9s ${CYAN}║${NC} %17s ${CYAN}║${NC} %17s ${CYAN}║${NC} ${color}%19s${NC} ${CYAN}║${NC}\n" \
        "$cmd" "${redis:-N/A}" "${ac:-N/A}" "${ratio}x"
}

print_row "PING"  "$REDIS_PING"  "$AC_PING"
print_row "SET"   "$REDIS_SET"   "$AC_SET"
print_row "GET"   "$REDIS_GET"   "$AC_GET"
print_row "INCR"  "$REDIS_INCR"  "$AC_INCR"

echo -e "${CYAN}╚═══════════╩═══════════════════╩═══════════════════╩═════════════════════╝${NC}"
echo ""

# ── [5/5] Notes ──────────────────────────────────────────────────────────────
echo -e "${YELLOW}[5/5] Notes:${NC}"
echo "  - Redis:     3-node cluster (ports ${REDIS_PORTS[*]}), entry: ${REDIS_PORTS[0]}"
echo "  - AutoCache: 3-node cluster (ports ${AC_PORTS[*]}), entry: ${AC_PORTS[0]}"
echo "  - Both use --cluster flag (auto MOVED routing)"
echo "  - Running natively on localhost, no containerization"
echo ""
