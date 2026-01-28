#!/usr/bin/env bash
set -e

GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

REQUESTS=${1:-100000}
CLIENTS=${2:-50}
DATA_SIZE=${3:-100}

REDIS_PORT=6380
AUTOCACHE_PORT=6381

echo -e "${CYAN}${BOLD}"
echo "╔══════════════════════════════════════════════════════════════════════╗"
echo "║     Native Performance Comparison: AutoCache vs Redis (No K8s)      ║"
echo "╚══════════════════════════════════════════════════════════════════════╝"
echo -e "${NC}"
echo "Configuration:"
echo "  Requests:   ${REQUESTS}"
echo "  Clients:    ${CLIENTS}"
echo "  Data size:  ${DATA_SIZE} bytes"
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
    redis-benchmark -p ${port} -n ${REQUESTS} -c ${CLIENTS} -d ${DATA_SIZE} -q \
        -t ping,set,get,incr,lpush,rpush,lpop,rpop,sadd,hset,mset 2>&1 | \
        grep -E "PING_INLINE|SET:|GET:|INCR:|LPUSH:|RPUSH:|LPOP:|RPOP:|SADD:|HSET:|MSET"
    echo ""
}

run_bench "Redis 7" ${REDIS_PORT}
run_bench "AutoCache" ${AUTOCACHE_PORT}

echo -e "${YELLOW}[4/4] Detailed comparison...${NC}"
echo ""

get_rps() {
    local port=$1
    local cmd=$2
    local key=$3
    
    if [ -n "$key" ]; then
        redis-benchmark -p ${port} -n ${REQUESTS} -c ${CLIENTS} -q ${cmd} "${key}" 2>&1 | \
            tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1
    else
        redis-benchmark -p ${port} -n ${REQUESTS} -c ${CLIENTS} -t ${cmd} -q 2>&1 | \
            grep -i "${cmd}" | head -1 | grep -oE '[0-9]+\.[0-9]+' | head -1
    fi
}

REDIS_PING=$(redis-benchmark -p ${REDIS_PORT} -n ${REQUESTS} -c ${CLIENTS} -t ping -q 2>&1 | grep PING_INLINE | grep -oE '[0-9]+\.[0-9]+' | head -1)
REDIS_SET=$(redis-benchmark -p ${REDIS_PORT} -n ${REQUESTS} -c ${CLIENTS} -q SET 'key:__rand_int__' __data__ 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)
REDIS_GET=$(redis-benchmark -p ${REDIS_PORT} -n ${REQUESTS} -c ${CLIENTS} -q GET 'key:__rand_int__' 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)
REDIS_INCR=$(redis-benchmark -p ${REDIS_PORT} -n ${REQUESTS} -c ${CLIENTS} -q INCR 'counter' 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)

AC_PING=$(redis-benchmark -p ${AUTOCACHE_PORT} -n ${REQUESTS} -c ${CLIENTS} -t ping -q 2>&1 | grep PING_INLINE | grep -oE '[0-9]+\.[0-9]+' | head -1)
AC_SET=$(redis-benchmark -p ${AUTOCACHE_PORT} -n ${REQUESTS} -c ${CLIENTS} -q SET 'key:__rand_int__' __data__ 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)
AC_GET=$(redis-benchmark -p ${AUTOCACHE_PORT} -n ${REQUESTS} -c ${CLIENTS} -q GET 'key:__rand_int__' 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)
AC_INCR=$(redis-benchmark -p ${AUTOCACHE_PORT} -n ${REQUESTS} -c ${CLIENTS} -q INCR 'counter' 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)

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

AVG_RATIO=$(echo "scale=2; ($(calc_ratio "$REDIS_PING" "$AC_PING") + $(calc_ratio "$REDIS_SET" "$AC_SET") + $(calc_ratio "$REDIS_GET" "$AC_GET") + $(calc_ratio "$REDIS_INCR" "$AC_INCR")) / 4" | bc 2>/dev/null || echo "N/A")

echo -e "${YELLOW}Summary:${NC}"
echo "  Average performance ratio: ${AVG_RATIO}x of Redis"
echo ""
echo -e "${YELLOW}Notes:${NC}"
echo "  - Both servers running natively on localhost (no containerization)"
echo "  - No network overhead (loopback interface only)"
echo "  - Redis: C implementation with 20+ years of optimization"
echo "  - AutoCache: Go implementation (MVP stage)"
echo ""
