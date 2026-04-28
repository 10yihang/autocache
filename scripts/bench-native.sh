#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/bench-common.sh"

REQUESTS=${1:-$(default_requests)}
CLIENTS=${2:-$(default_clients)}
DATA_SIZE=${3:-$(default_data_size)}

REDIS_PORT=6380
AUTOCACHE_PORT=6381

require_cmd redis-benchmark
require_cmd redis-server
require_cmd redis-cli

echo -e "${CYAN}${BOLD}"
echo "╔══════════════════════════════════════════════════════════════════════╗"
echo "║     Native Performance Comparison: AutoCache vs Redis (No K8s)      ║"
echo "╚══════════════════════════════════════════════════════════════════════╝"
echo -e "${NC}"
print_bench_config "${REQUESTS}" "${CLIENTS}" "${DATA_SIZE}"
echo "  Redis port: ${REDIS_PORT}"
echo "  AutoCache:  ${AUTOCACHE_PORT}"
echo ""

cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    pkill -f "redis-server.*${REDIS_PORT}" 2>/dev/null || true
    pkill -f "autocache.*${AUTOCACHE_PORT}" 2>/dev/null || true
    rm -f /tmp/redis-test-*.conf /tmp/autocache-test.log
    echo "Done."
}
trap cleanup EXIT

cd "$(dirname "$0")/.."

echo -e "${YELLOW}[1/4] Building AutoCache...${NC}"
go build -o bin/autocache ./cmd/server 2>&1 | tail -3 || true
if [ ! -f bin/autocache ]; then
    echo -e "${RED}Failed to build AutoCache${NC}"
    exit 1
fi
echo "  AutoCache built successfully"

echo ""
echo -e "${YELLOW}[2/4] Starting servers...${NC}"

echo "  Starting Redis on port ${REDIS_PORT}..."
redis-server --port ${REDIS_PORT} --daemonize yes --loglevel warning \
    --save "" --appendonly no \
    --pidfile /tmp/redis-test-${REDIS_PORT}.pid

sleep 1
if ! redis-cli -p ${REDIS_PORT} ping >/dev/null 2>&1; then
    echo -e "${RED}Failed to start Redis${NC}"
    exit 1
fi
echo "  Redis started"

echo "  Starting AutoCache on port ${AUTOCACHE_PORT}..."
./bin/autocache -addr ":${AUTOCACHE_PORT}" > /tmp/autocache-test.log 2>&1 &
AUTOCACHE_PID=$!

sleep 2
if ! redis-cli -p ${AUTOCACHE_PORT} ping >/dev/null 2>&1; then
    echo -e "${RED}Failed to start AutoCache${NC}"
    cat /tmp/autocache-test.log
    exit 1
fi
echo "  AutoCache started (PID: ${AUTOCACHE_PID})"

echo ""
echo -e "${CYAN}${BOLD}══════════════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}${BOLD}                    NATIVE BENCHMARK RESULTS                          ${NC}"
echo -e "${CYAN}${BOLD}══════════════════════════════════════════════════════════════════════${NC}"
echo ""

echo -e "${YELLOW}[3/4] Running benchmarks...${NC}"
echo ""

run_bench() {
    local name=$1
    local port=$2
    
    echo -e "${GREEN}--- ${name} (localhost:${port}) ---${NC}"
    safe_bench_output redis-benchmark -p ${port} -n ${REQUESTS} -c ${CLIENTS} -d ${DATA_SIZE} -q \
        -t ping,set,get,incr | grep -E "PING_INLINE|SET:|GET:|INCR:"
    # echo -e "${YELLOW}  Optional extended commands (best effort):${NC}"
    # safe_bench_output redis-benchmark -p ${port} -n ${REQUESTS} -c ${CLIENTS} -d ${DATA_SIZE} -q \
    #     -t lpush,rpush,lpop,rpop,sadd,hset,mset | grep -E "LPUSH:|RPUSH:|LPOP:|RPOP:|SADD:|HSET:|MSET|ERR unknown command" || true
    echo ""
}

run_bench "Redis 7" ${REDIS_PORT}
run_bench "AutoCache" ${AUTOCACHE_PORT}

echo -e "${YELLOW}[4/4] Detailed comparison...${NC}"
echo ""

get_rps() {
    local port=$1
    local cmd=$2
    shift 2
    local output
    
    if [ "$#" -gt 0 ]; then
        output=$(safe_bench_output redis-benchmark -p ${port} -n ${REQUESTS} -c ${CLIENTS} -q ${cmd} "$@")
    else
        output=$(safe_bench_output redis-benchmark -p ${port} -n ${REQUESTS} -c ${CLIENTS} -t ${cmd} -q)
    fi

    printf '%s\n' "$output" | tr '\r' '\n' | grep 'requests per second' | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1
}

REDIS_PING=$(get_rps ${REDIS_PORT} ping)
REDIS_SET=$(get_rps ${REDIS_PORT} SET 'key:__rand_int__' '__data__')
REDIS_GET=$(get_rps ${REDIS_PORT} GET 'key:__rand_int__')
REDIS_INCR=$(get_rps ${REDIS_PORT} INCR 'counter')

AC_PING=$(get_rps ${AUTOCACHE_PORT} ping)
AC_SET=$(get_rps ${AUTOCACHE_PORT} SET 'key:__rand_int__' '__data__')
AC_GET=$(get_rps ${AUTOCACHE_PORT} GET 'key:__rand_int__')
AC_INCR=$(get_rps ${AUTOCACHE_PORT} INCR 'counter')

calc_ratio() {
    if [ -n "$1" ] && [ -n "$2" ] && [ "$1" != "0" ]; then
        printf "%.2f" $(echo "scale=4; $2 / $1" | bc 2>/dev/null) 2>/dev/null || echo "N/A"
    else
        echo "N/A"
    fi
}

echo -e "${CYAN}╔═══════════════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║                     PERFORMANCE SUMMARY (requests/sec)                   ║${NC}"
echo -e "${CYAN}╠═══════════╦═══════════════════╦═══════════════════╦═════════════════════╣${NC}"
echo -e "${CYAN}║ Command   ║ Redis 7           ║ AutoCache         ║ Ratio (AC/Redis)    ║${NC}"
echo -e "${CYAN}╠═══════════╬═══════════════════╬═══════════════════╬═════════════════════╣${NC}"

print_row() {
    local cmd=$1
    local redis=$2
    local ac=$3
    local ratio=$(calc_ratio "$redis" "$ac")
    
    if [ "$ratio" != "N/A" ]; then
        is_good=$(echo "$ratio >= 0.5" | bc 2>/dev/null || echo "0")
        if [ "$is_good" = "1" ]; then
            color="${GREEN}"
        else
            color="${RED}"
        fi
    else
        color="${NC}"
    fi
    
    printf "${CYAN}║${NC} %-9s ${CYAN}║${NC} %17s ${CYAN}║${NC} %17s ${CYAN}║${NC} ${color}%19s${NC} ${CYAN}║${NC}\n" \
        "$cmd" "${redis:-N/A}" "${ac:-N/A}" "${ratio}x"
}

print_row "PING" "$REDIS_PING" "$AC_PING"
print_row "SET" "$REDIS_SET" "$AC_SET"
print_row "GET" "$REDIS_GET" "$AC_GET"
print_row "INCR" "$REDIS_INCR" "$AC_INCR"

echo -e "${CYAN}╚═══════════╩═══════════════════╩═══════════════════╩═════════════════════╝${NC}"
echo ""

if [ -n "${REDIS_PING}" ] && [ -n "${AC_PING}" ] && [ -n "${REDIS_SET}" ] && [ -n "${AC_SET}" ] && [ -n "${REDIS_GET}" ] && [ -n "${AC_GET}" ] && [ -n "${REDIS_INCR}" ] && [ -n "${AC_INCR}" ]; then
    AVG_RATIO=$(echo "scale=2; ($(calc_ratio "$REDIS_PING" "$AC_PING") + $(calc_ratio "$REDIS_SET" "$AC_SET") + $(calc_ratio "$REDIS_GET" "$AC_GET") + $(calc_ratio "$REDIS_INCR" "$AC_INCR")) / 4" | bc 2>/dev/null || echo "N/A")
else
    AVG_RATIO="N/A"
fi

echo -e "${YELLOW}Summary:${NC}"
echo "  Average performance ratio: ${AVG_RATIO}x of Redis"
echo ""
echo -e "${YELLOW}Notes:${NC}"
echo "  - Both servers running natively on localhost (no containerization)"
echo "  - No network overhead (loopback interface only)"
echo "  - Redis: C implementation with 20+ years of optimization"
echo "  - AutoCache: Go implementation (MVP stage)"
echo ""
