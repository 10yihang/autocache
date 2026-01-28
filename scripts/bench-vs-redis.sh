#!/bin/bash
set -e

GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

NAMESPACE="default"
REQUESTS=${1:-50000}
CLIENTS=${2:-50}
DATA_SIZE=${3:-100}

echo -e "${CYAN}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║       AutoCache vs Redis Performance Comparison           ║${NC}"
echo -e "${CYAN}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo "Requests: ${REQUESTS}"
echo "Clients: ${CLIENTS}"  
echo "Data size: ${DATA_SIZE} bytes"
echo ""

cleanup() {
    echo -e "${YELLOW}Cleaning up...${NC}"
    kubectl delete pod redis-server redis-benchmark -n ${NAMESPACE} --ignore-not-found=true >/dev/null 2>&1
}
trap cleanup EXIT

echo -e "${YELLOW}[1/5] Deploying Redis server for comparison...${NC}"
kubectl delete pod redis-server -n ${NAMESPACE} --ignore-not-found=true >/dev/null 2>&1
sleep 2

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: redis-server
  namespace: ${NAMESPACE}
spec:
  containers:
  - name: redis
    image: docker.1ms.run/redis:7-alpine
    ports:
    - containerPort: 6379
    resources:
      limits:
        cpu: "1"
        memory: "512Mi"
EOF

echo "Waiting for Redis server..."
kubectl wait --for=condition=ready pod/redis-server -n ${NAMESPACE} --timeout=120s
REDIS_IP=$(kubectl get pod -n ${NAMESPACE} redis-server -o jsonpath='{.status.podIP}')
echo "Redis server ready at ${REDIS_IP}"

echo ""
echo -e "${YELLOW}[2/5] Deploying benchmark pod...${NC}"
kubectl delete pod redis-benchmark -n ${NAMESPACE} --ignore-not-found=true >/dev/null 2>&1
sleep 2

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: redis-benchmark
  namespace: ${NAMESPACE}
spec:
  containers:
  - name: benchmark
    image: docker.1ms.run/redis:7-alpine
    command: ["sleep", "3600"]
EOF

kubectl wait --for=condition=ready pod/redis-benchmark -n ${NAMESPACE} --timeout=120s
echo "Benchmark pod ready"

AUTOCACHE_IP=$(kubectl get pod -n ${NAMESPACE} -l app.kubernetes.io/name=autocache -o jsonpath='{.items[0].status.podIP}')
echo "AutoCache node: ${AUTOCACHE_IP}"
echo ""

RESULTS_FILE="/tmp/bench_results_$$"

run_single_bench() {
    local host=$1
    local cmd=$2
    local key=$3
    
    result=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
        -h ${host} -p 6379 \
        -n ${REQUESTS} \
        -c ${CLIENTS} \
        -d ${DATA_SIZE} \
        -q \
        ${cmd} ${key} 2>/dev/null | tail -1)
    
    rps=$(echo "$result" | grep -oE '[0-9]+\.[0-9]+' | head -1)
    echo "${rps:-0}"
}

echo -e "${YELLOW}[3/5] Benchmarking Redis...${NC}"
echo -e "${GREEN}--- Redis 7 ---${NC}"

echo -n "  PING:  "
REDIS_PING=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
    -h ${REDIS_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -t ping -q 2>/dev/null | grep PING_INLINE | grep -oE '[0-9]+\.[0-9]+' | head -1)
echo "${REDIS_PING:-0} req/s"

echo -n "  SET:   "
REDIS_SET=$(run_single_bench ${REDIS_IP} "SET" "redis:__rand_int__ __data__")
echo "${REDIS_SET} req/s"

echo -n "  GET:   "
REDIS_GET=$(run_single_bench ${REDIS_IP} "GET" "redis:__rand_int__")
echo "${REDIS_GET} req/s"

echo -n "  INCR:  "
REDIS_INCR=$(run_single_bench ${REDIS_IP} "INCR" "redis:counter")
echo "${REDIS_INCR} req/s"

echo -n "  LPUSH: "
REDIS_LPUSH=$(run_single_bench ${REDIS_IP} "LPUSH" "redis:list __data__")
echo "${REDIS_LPUSH} req/s"

echo -n "  LPOP:  "
REDIS_LPOP=$(run_single_bench ${REDIS_IP} "LPOP" "redis:list")
echo "${REDIS_LPOP} req/s"

echo ""
echo -e "${YELLOW}[4/5] Benchmarking AutoCache...${NC}"
echo -e "${GREEN}--- AutoCache ---${NC}"

echo -n "  PING:  "
AC_PING=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
    -h ${AUTOCACHE_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -t ping -q 2>/dev/null | grep PING_INLINE | grep -oE '[0-9]+\.[0-9]+' | head -1)
echo "${AC_PING:-0} req/s"

echo -n "  SET:   "
AC_SET=$(run_single_bench ${AUTOCACHE_IP} "SET" "{b}:__rand_int__ __data__")
echo "${AC_SET} req/s"

echo -n "  GET:   "
AC_GET=$(run_single_bench ${AUTOCACHE_IP} "GET" "{b}:__rand_int__")
echo "${AC_GET} req/s"

echo -n "  INCR:  "
AC_INCR=$(run_single_bench ${AUTOCACHE_IP} "INCR" "{b}:counter")
echo "${AC_INCR} req/s"

echo -n "  LPUSH: "
AC_LPUSH=$(run_single_bench ${AUTOCACHE_IP} "LPUSH" "{b}:list __data__")
echo "${AC_LPUSH} req/s"

echo -n "  LPOP:  "
AC_LPOP=$(run_single_bench ${AUTOCACHE_IP} "LPOP" "{b}:list")
echo "${AC_LPOP} req/s"

echo ""
echo -e "${YELLOW}[5/5] Results Comparison${NC}"
echo ""

calc_ratio() {
    local redis=$1
    local ac=$2
    
    if [ -z "$redis" ] || [ -z "$ac" ] || [ "$redis" = "0" ] || [ "$ac" = "0" ]; then
        echo "N/A"
        return
    fi
    
    ratio=$(echo "scale=2; $ac / $redis" | bc 2>/dev/null)
    echo "${ratio}"
}

echo -e "${CYAN}╔═══════════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║                    Performance Comparison (req/s)                     ║${NC}"
echo -e "${CYAN}╠═══════════╦═══════════════════╦═══════════════════╦═══════════════════╣${NC}"
echo -e "${CYAN}║ Command   ║ Redis 7           ║ AutoCache         ║ Ratio (AC/Redis)  ║${NC}"
echo -e "${CYAN}╠═══════════╬═══════════════════╬═══════════════════╬═══════════════════╣${NC}"

print_row() {
    local cmd=$1
    local redis=$2
    local ac=$3
    local ratio=$(calc_ratio "$redis" "$ac")
    
    if [ "$ratio" != "N/A" ]; then
        is_better=$(echo "$ratio >= 1" | bc 2>/dev/null)
        if [ "$is_better" = "1" ]; then
            color="${GREEN}"
        else
            color="${RED}"
        fi
        ratio_str="${ratio}x"
    else
        color="${NC}"
        ratio_str="N/A"
    fi
    
    printf "${CYAN}║${NC} %-9s ${CYAN}║${NC} %17s ${CYAN}║${NC} %17s ${CYAN}║${NC} ${color}%17s${NC} ${CYAN}║${NC}\n" \
        "$cmd" "${redis:-N/A}" "${ac:-N/A}" "$ratio_str"
}

print_row "PING" "$REDIS_PING" "$AC_PING"
print_row "SET" "$REDIS_SET" "$AC_SET"
print_row "GET" "$REDIS_GET" "$AC_GET"
print_row "INCR" "$REDIS_INCR" "$AC_INCR"
print_row "LPUSH" "$REDIS_LPUSH" "$AC_LPUSH"
print_row "LPOP" "$REDIS_LPOP" "$AC_LPOP"

echo -e "${CYAN}╚═══════════╩═══════════════════╩═══════════════════╩═══════════════════╝${NC}"
echo ""
echo -e "${YELLOW}Notes:${NC}"
echo "  - Both tests run in the same K8s cluster with similar resource limits"
echo "  - AutoCache uses hash tag {b} to route all keys to single node"
echo "  - Redis runs in standalone mode (no cluster overhead)"
echo "  - Ratio > 1.0 means AutoCache is faster (green)"
echo "  - Ratio < 1.0 means Redis is faster (red)"
echo ""

echo -e "${GREEN}Benchmark complete!${NC}"
