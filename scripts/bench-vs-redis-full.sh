#!/usr/bin/env bash
set -e

GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

NAMESPACE="default"
REQUESTS=${1:-50000}
CLIENTS=${2:-50}

echo -e "${CYAN}${BOLD}"
echo "╔══════════════════════════════════════════════════════════════════════╗"
echo "║         Complete Performance Comparison: AutoCache vs Redis          ║"
echo "║                    (Standalone & Cluster Modes)                      ║"
echo "╚══════════════════════════════════════════════════════════════════════╝"
echo -e "${NC}"
echo "Configuration: ${REQUESTS} requests, ${CLIENTS} concurrent clients"
echo ""

cleanup() {
    echo -e "${YELLOW}Cleaning up test resources...${NC}"
    kubectl delete pod redis-standalone redis-benchmark -n ${NAMESPACE} --ignore-not-found=true >/dev/null 2>&1
    kubectl delete pod redis-cluster-0 redis-cluster-1 redis-cluster-2 -n ${NAMESPACE} --ignore-not-found=true >/dev/null 2>&1
    kubectl delete pod autocache-standalone -n ${NAMESPACE} --ignore-not-found=true >/dev/null 2>&1
}
trap cleanup EXIT

run_bench() {
    local name=$1
    local host=$2
    local tag=$3
    
    local ping_result=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
        -h ${host} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -t ping -q 2>&1 | grep PING_INLINE | grep -oE '[0-9]+\.[0-9]+' | head -1)
    
    local set_result=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
        -h ${host} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q \
        SET "${tag}:__rand_int__" __data__ 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)
    
    local get_result=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
        -h ${host} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q \
        GET "${tag}:__rand_int__" 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)
    
    local incr_result=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
        -h ${host} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q \
        INCR "${tag}:counter" 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)
    
    echo "${ping_result:-0}|${set_result:-0}|${get_result:-0}|${incr_result:-0}"
}

echo -e "${YELLOW}[1/7] Deploying benchmark pod...${NC}"
kubectl delete pod redis-benchmark -n ${NAMESPACE} --ignore-not-found=true >/dev/null 2>&1
sleep 1
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: redis-benchmark
  namespace: ${NAMESPACE}
spec:
  containers:
  - name: bench
    image: docker.1ms.run/redis:7-alpine
    command: ["sleep", "3600"]
    resources:
      limits:
        cpu: "2"
        memory: "512Mi"
EOF
kubectl wait --for=condition=ready pod/redis-benchmark -n ${NAMESPACE} --timeout=120s
echo "  Benchmark pod ready"

echo ""
echo -e "${YELLOW}[2/7] Deploying Redis Standalone...${NC}"
kubectl delete pod redis-standalone -n ${NAMESPACE} --ignore-not-found=true >/dev/null 2>&1
sleep 1
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: redis-standalone
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
kubectl wait --for=condition=ready pod/redis-standalone -n ${NAMESPACE} --timeout=120s
REDIS_STANDALONE_IP=$(kubectl get pod -n ${NAMESPACE} redis-standalone -o jsonpath='{.status.podIP}')
echo "  Redis Standalone ready at ${REDIS_STANDALONE_IP}"

echo ""
echo -e "${YELLOW}[3/7] Deploying Redis Cluster (3 nodes)...${NC}"
for i in 0 1 2; do
    kubectl delete pod redis-cluster-${i} -n ${NAMESPACE} --ignore-not-found=true >/dev/null 2>&1
done
sleep 1

for i in 0 1 2; do
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: redis-cluster-${i}
  namespace: ${NAMESPACE}
  labels:
    app: redis-cluster
spec:
  containers:
  - name: redis
    image: docker.1ms.run/redis:7-alpine
    command: ["redis-server", "--cluster-enabled", "yes", "--cluster-config-file", "/data/nodes.conf", "--cluster-node-timeout", "5000", "--appendonly", "yes"]
    ports:
    - containerPort: 6379
    - containerPort: 16379
    resources:
      limits:
        cpu: "1"
        memory: "512Mi"
EOF
done

for i in 0 1 2; do
    kubectl wait --for=condition=ready pod/redis-cluster-${i} -n ${NAMESPACE} --timeout=120s
done

REDIS_CLUSTER_IPS=()
for i in 0 1 2; do
    ip=$(kubectl get pod -n ${NAMESPACE} redis-cluster-${i} -o jsonpath='{.status.podIP}')
    REDIS_CLUSTER_IPS+=("$ip")
done
echo "  Redis Cluster nodes: ${REDIS_CLUSTER_IPS[*]}"

echo "  Initializing Redis Cluster..."
CLUSTER_CREATE_CMD="redis-cli --cluster create"
for ip in "${REDIS_CLUSTER_IPS[@]}"; do
    CLUSTER_CREATE_CMD="$CLUSTER_CREATE_CMD ${ip}:6379"
done
CLUSTER_CREATE_CMD="$CLUSTER_CREATE_CMD --cluster-replicas 0 --cluster-yes"

kubectl exec -n ${NAMESPACE} redis-benchmark -- sh -c "$CLUSTER_CREATE_CMD" >/dev/null 2>&1 || true
sleep 3

kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-cli -h ${REDIS_CLUSTER_IPS[0]} -p 6379 CLUSTER INFO 2>/dev/null | grep -E "cluster_state|cluster_slots_assigned" | tr -d '\r'

echo ""
echo -e "${YELLOW}[4/7] Checking AutoCache Cluster...${NC}"
AC_CLUSTER_IPS=()
AC_PODS=$(kubectl get pods -n ${NAMESPACE} -l app.kubernetes.io/name=autocache -o jsonpath='{.items[*].metadata.name}')
for pod in $AC_PODS; do
    ip=$(kubectl get pod -n ${NAMESPACE} ${pod} -o jsonpath='{.status.podIP}')
    AC_CLUSTER_IPS+=("$ip")
done
echo "  AutoCache Cluster nodes: ${AC_CLUSTER_IPS[*]}"
kubectl exec -n ${NAMESPACE} ${AC_PODS%% *} -- /autocache -cli CLUSTER INFO 2>/dev/null | grep -E "cluster_state|cluster_slots_assigned" | tr -d '\r$'

echo ""
echo -e "${YELLOW}[5/7] Deploying AutoCache Standalone...${NC}"
kubectl delete pod autocache-standalone -n ${NAMESPACE} --ignore-not-found=true >/dev/null 2>&1
sleep 1
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
    command: ["/autocache"]
    ports:
    - containerPort: 6379
    resources:
      limits:
        cpu: "1"
        memory: "512Mi"
EOF
kubectl wait --for=condition=ready pod/autocache-standalone -n ${NAMESPACE} --timeout=120s
AC_STANDALONE_IP=$(kubectl get pod -n ${NAMESPACE} autocache-standalone -o jsonpath='{.status.podIP}')
echo "  AutoCache Standalone ready at ${AC_STANDALONE_IP}"

echo ""
echo -e "${CYAN}${BOLD}══════════════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}${BOLD}                         BENCHMARK RESULTS                            ${NC}"
echo -e "${CYAN}${BOLD}══════════════════════════════════════════════════════════════════════${NC}"
echo ""

echo -e "${YELLOW}[6/7] Running Benchmarks...${NC}"
echo ""

echo -e "${GREEN}--- Redis Standalone ---${NC}"
echo -n "  PING: "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${REDIS_STANDALONE_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -t ping -q 2>&1 | grep PING_INLINE | tail -1
echo -n "  SET:  "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${REDIS_STANDALONE_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q SET 'key:__rand_int__' __data__ 2>&1 | tail -1
echo -n "  GET:  "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${REDIS_STANDALONE_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q GET 'key:__rand_int__' 2>&1 | tail -1
echo -n "  INCR: "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${REDIS_STANDALONE_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q INCR 'counter' 2>&1 | tail -1
REDIS_STANDALONE_SET=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${REDIS_STANDALONE_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q SET 'key:__rand_int__' __data__ 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)

echo ""
echo -e "${GREEN}--- Redis Cluster (single node, hash tag) ---${NC}"
echo -n "  PING: "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${REDIS_CLUSTER_IPS[0]} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -t ping -q 2>&1 | grep PING_INLINE | tail -1
echo -n "  SET:  "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${REDIS_CLUSTER_IPS[0]} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q SET '{rc}:__rand_int__' __data__ 2>&1 | tail -1
echo -n "  GET:  "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${REDIS_CLUSTER_IPS[0]} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q GET '{rc}:__rand_int__' 2>&1 | tail -1
echo -n "  INCR: "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${REDIS_CLUSTER_IPS[0]} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q INCR '{rc}:counter' 2>&1 | tail -1
REDIS_CLUSTER_SET=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${REDIS_CLUSTER_IPS[0]} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q SET '{rc}:__rand_int__' __data__ 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)

echo ""
echo -e "${GREEN}--- AutoCache Standalone ---${NC}"
echo -n "  PING: "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${AC_STANDALONE_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -t ping -q 2>&1 | grep PING_INLINE | tail -1
echo -n "  SET:  "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${AC_STANDALONE_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q SET 'key:__rand_int__' __data__ 2>&1 | tail -1
echo -n "  GET:  "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${AC_STANDALONE_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q GET 'key:__rand_int__' 2>&1 | tail -1
echo -n "  INCR: "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${AC_STANDALONE_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q INCR 'counter' 2>&1 | tail -1
AC_STANDALONE_SET=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${AC_STANDALONE_IP} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q SET 'key:__rand_int__' __data__ 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)

echo ""
echo -e "${GREEN}--- AutoCache Cluster (single node, hash tag) ---${NC}"
echo -n "  PING: "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${AC_CLUSTER_IPS[0]} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -t ping -q 2>&1 | grep PING_INLINE | tail -1
echo -n "  SET:  "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${AC_CLUSTER_IPS[0]} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q SET '{b}:__rand_int__' __data__ 2>&1 | tail -1
echo -n "  GET:  "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${AC_CLUSTER_IPS[0]} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q GET '{b}:__rand_int__' 2>&1 | tail -1
echo -n "  INCR: "; kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${AC_CLUSTER_IPS[0]} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q INCR '{b}:counter' 2>&1 | tail -1
AC_CLUSTER_SET=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark -h ${AC_CLUSTER_IPS[0]} -p 6379 -n ${REQUESTS} -c ${CLIENTS} -q SET '{b}:__rand_int__' __data__ 2>&1 | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1)

echo ""
echo -e "${YELLOW}[7/7] Summary${NC}"
echo ""
echo -e "${CYAN}╔══════════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║                      PERFORMANCE SUMMARY (SET ops/s)                 ║${NC}"
echo -e "${CYAN}╠══════════════════════════════════════════════════════════════════════╣${NC}"
echo -e "${CYAN}║                      │ Redis 7          │ AutoCache        │ Ratio  ║${NC}"
echo -e "${CYAN}╠══════════════════════╪══════════════════╪══════════════════╪════════╣${NC}"

calc_ratio() {
    if [ -n "$1" ] && [ -n "$2" ] && [ "$1" != "0" ]; then
        echo "scale=2; $2 / $1" | bc 2>/dev/null || echo "N/A"
    else
        echo "N/A"
    fi
}

STANDALONE_RATIO=$(calc_ratio "$REDIS_STANDALONE_SET" "$AC_STANDALONE_SET")
CLUSTER_RATIO=$(calc_ratio "$REDIS_CLUSTER_SET" "$AC_CLUSTER_SET")

printf "${CYAN}║${NC} %-20s ${CYAN}│${NC} %16s ${CYAN}│${NC} %16s ${CYAN}│${NC} %6s ${CYAN}║${NC}\n" \
    "Standalone" "${REDIS_STANDALONE_SET:-N/A}" "${AC_STANDALONE_SET:-N/A}" "${STANDALONE_RATIO}x"
printf "${CYAN}║${NC} %-20s ${CYAN}│${NC} %16s ${CYAN}│${NC} %16s ${CYAN}│${NC} %6s ${CYAN}║${NC}\n" \
    "Cluster (per node)" "${REDIS_CLUSTER_SET:-N/A}" "${AC_CLUSTER_SET:-N/A}" "${CLUSTER_RATIO}x"

echo -e "${CYAN}╚══════════════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${YELLOW}Notes:${NC}"
echo "  - All tests run on same K8s cluster with identical resource limits (1 CPU, 512Mi)"
echo "  - Cluster tests use hash tags to route all keys to single node (fair comparison)"
echo "  - Redis is C-based, AutoCache is Go-based (expected performance difference)"
echo "  - Ratio = AutoCache / Redis (higher is better for AutoCache)"
echo ""
echo -e "${GREEN}Benchmark complete!${NC}"
