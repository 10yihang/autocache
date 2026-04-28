#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/bench-common.sh"

NAMESPACE=${NAMESPACE:-default}
REQUESTS=${1:-$(default_requests)}
CLIENTS=${2:-$(default_clients)}
DATA_SIZE=${3:-$(default_data_size)}

echo -e "${CYAN}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║       AutoCache vs Redis Performance Comparison           ║${NC}"
echo -e "${CYAN}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
print_bench_config "${REQUESTS}" "${CLIENTS}" "${DATA_SIZE}"
echo ""

cleanup() {
    echo -e "${YELLOW}Cleaning up...${NC}"
    delete_pod_if_exists "${NAMESPACE}" redis-server autocache-standalone redis-benchmark
}
trap cleanup EXIT

echo -e "${YELLOW}[1/5] Deploying Redis server for comparison...${NC}"
delete_pod_if_exists "${NAMESPACE}" redis-server
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
      requests:
        cpu: "1m"
        memory: "128Mi"
      limits:
        cpu: "2"
        memory: "512Mi"
EOF

echo "Waiting for Redis server..."
wait_for_pod_ready "${NAMESPACE}" redis-server
REDIS_IP=$(pod_ip "${NAMESPACE}" redis-server)
echo "Redis server ready at ${REDIS_IP}"

echo ""
echo -e "${YELLOW}[1b/5] Deploying standalone AutoCache baseline...${NC}"
delete_pod_if_exists "${NAMESPACE}" autocache-standalone

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: autocache-standalone
  namespace: ${NAMESPACE}
spec:
  containers:
  - name: autocache
    image: autocache:latest
    imagePullPolicy: Never
    command:
      - /autocache
      - --addr
      - :6379
      - --data-dir
      - /data
      - --metrics-addr
      - :9121
      - --quiet-connections
    ports:
      - containerPort: 6379
    resources:
      requests:
        cpu: "1"
        memory: "128Mi"
      limits:
        cpu: "2"
        memory: "512Mi"
    volumeMounts:
      - name: data
        mountPath: /data
  volumes:
    - name: data
      emptyDir: {}
EOF

echo "Waiting for standalone AutoCache..."
wait_for_pod_ready "${NAMESPACE}" autocache-standalone
AUTOCACHE_IP=$(pod_ip "${NAMESPACE}" autocache-standalone)
echo "Standalone AutoCache ready at ${AUTOCACHE_IP}"

echo ""
echo -e "${YELLOW}[2/5] Deploying benchmark pod...${NC}"
sleep 2
ensure_benchmark_pod "${NAMESPACE}"
echo "Benchmark pod ready"
echo "AutoCache benchmark target: ${AUTOCACHE_IP}"
echo ""

RESULTS_FILE="/tmp/bench_results_$$"

run_single_bench() {
    local host=$1
    local cmd=$2
    local key=$3
    local result
    
    result=$(run_k8s_benchmark "${NAMESPACE}" "${host}" \
        -n ${REQUESTS} \
        -c ${CLIENTS} \
        -d ${DATA_SIZE} \
        -q \
        ${cmd} ${key} | tail -1)
    
    rps=$(printf '%s\n' "$result" | tr '\r' '\n' | grep 'requests per second' | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)
    echo "${rps:-0}"
}

echo -e "${YELLOW}[3/5] Benchmarking Redis...${NC}"
echo -e "${GREEN}--- Redis 7 ---${NC}"

echo -n "  PING:  "
REDIS_PING=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
    -h ${REDIS_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -t ping -q 2>/dev/null | tr '\r' '\n' | grep 'requests per second' | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)
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


echo ""
echo -e "${YELLOW}[4/5] Benchmarking AutoCache...${NC}"
echo -e "${GREEN}--- AutoCache ---${NC}"

echo -n "  PING:  "
AC_PING=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
    -h ${AUTOCACHE_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -t ping -q 2>/dev/null | tr '\r' '\n' | grep 'requests per second' | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)
echo "${AC_PING:-0} req/s"

echo -n "  SET:   "
AC_SET=$(run_single_bench ${AUTOCACHE_IP} "SET" "autocache:__rand_int__ __data__")
echo "${AC_SET} req/s"

echo -n "  GET:   "
AC_GET=$(run_single_bench ${AUTOCACHE_IP} "GET" "autocache:__rand_int__")
echo "${AC_GET} req/s"

echo -n "  INCR:  "
AC_INCR=$(run_single_bench ${AUTOCACHE_IP} "INCR" "autocache:counter")
echo "${AC_INCR} req/s"

echo -e "${YELLOW}  Optional note: non-core command results may be zero if the target runtime does not expose that command in this environment.${NC}"

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

echo -e "${CYAN}╚═══════════╩═══════════════════╩═══════════════════╩═══════════════════╝${NC}"
echo ""
echo -e "${YELLOW}Notes:${NC}"
echo "  - Both tests run in the same K8s cluster with similar resource limits"
echo "  - AutoCache and Redis both run in standalone Pod mode for this comparison"
echo "  - Ratio > 1.0 means AutoCache is faster (green)"
echo "  - Ratio < 1.0 means Redis is faster (red)"
echo ""

echo -e "${GREEN}Benchmark complete!${NC}"
