#!/bin/bash
set -e

GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m'

CLUSTER_NAME=$(grep -m1 'name:' config/samples/cache_v1alpha1_autocache.yaml | awk '{print $2}')
NAMESPACE="default"
REQUESTS=${1:-100000}
CLIENTS=${2:-50}

echo -e "${GREEN}=== AutoCache Multi-Node Cluster Benchmark ===${NC}"
echo "Cluster: ${CLUSTER_NAME}"
echo "Requests: ${REQUESTS}"
echo "Clients: ${CLIENTS}"
echo ""

PODS=$(kubectl get pods -n ${NAMESPACE} -l app.kubernetes.io/name=autocache -o jsonpath='{.items[*].metadata.name}')
POD_ARRAY=($PODS)
POD_COUNT=${#POD_ARRAY[@]}

if [ "$POD_COUNT" -lt 1 ]; then
    echo -e "${RED}No autocache pods found${NC}"
    exit 1
fi

echo -e "${GREEN}Found ${POD_COUNT} nodes: ${PODS}${NC}"
echo ""

POD_IPS=()
for pod in "${POD_ARRAY[@]}"; do
    ip=$(kubectl get pod -n ${NAMESPACE} ${pod} -o jsonpath='{.status.podIP}')
    POD_IPS+=("$ip")
done

echo -e "${YELLOW}[1/4] Verifying cluster state...${NC}"
kubectl exec -n ${NAMESPACE} ${POD_ARRAY[0]} -- /autocache -cli CLUSTER INFO 2>/dev/null | grep -E "cluster_state|cluster_slots_assigned|cluster_known_nodes" | tr -d '$'
echo ""

kubectl delete pod redis-benchmark -n ${NAMESPACE} --ignore-not-found=true >/dev/null 2>&1

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: redis-benchmark
  namespace: ${NAMESPACE}
spec:
  restartPolicy: Never
  containers:
  - name: benchmark
    image: docker.1ms.run/redis:7-alpine
    command: ["sleep", "3600"]
EOF

echo -e "${YELLOW}Waiting for benchmark pod...${NC}"
kubectl wait --for=condition=ready pod/redis-benchmark -n ${NAMESPACE} --timeout=120s

echo ""
echo -e "${YELLOW}[2/4] Testing individual nodes...${NC}"
echo "Each node handles keys in its own slot range"
echo ""

TAGS=("b" "c" "a")

for i in "${!POD_ARRAY[@]}"; do
    pod=${POD_ARRAY[$i]}
    ip=${POD_IPS[$i]}
    tag=${TAGS[$i]}
    
    echo -e "${GREEN}--- Node $i: ${pod} (${ip}) tag={${tag}} ---${NC}"
    
    echo -n "  SET: "
    kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
        -h ${ip} -p 6379 \
        -n $((REQUESTS / POD_COUNT)) \
        -c ${CLIENTS} \
        -q \
        SET "{${tag}}:__rand_int__" __data__ 2>/dev/null | tail -1 || echo "failed"
    
    echo -n "  GET: "
    kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
        -h ${ip} -p 6379 \
        -n $((REQUESTS / POD_COUNT)) \
        -c ${CLIENTS} \
        -q \
        GET "{${tag}}:__rand_int__" 2>/dev/null | tail -1 || echo "failed"
    echo ""
done

echo -e "${YELLOW}[3/4] Parallel benchmark (all nodes simultaneously)...${NC}"
echo "Running SET/GET on all ${POD_COUNT} nodes in parallel"
echo ""

RESULTS_DIR="/tmp/autocache-bench-$$"
mkdir -p ${RESULTS_DIR}

TAGS=("b" "c" "a")

PIDS=()
for i in "${!POD_ARRAY[@]}"; do
    ip=${POD_IPS[$i]}
    tag=${TAGS[$i]}
    (
        result=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
            -h ${ip} -p 6379 \
            -n $((REQUESTS / POD_COUNT)) \
            -c ${CLIENTS} \
            -q \
            SET "{${tag}}:__rand_int__" __data__ 2>/dev/null | tail -1)
        echo "$result" > ${RESULTS_DIR}/set_${i}.txt
    ) &
    PIDS+=($!)
done

for pid in "${PIDS[@]}"; do
    wait $pid 2>/dev/null || true
done

total_set_rps=0
echo "SET results:"
for i in "${!POD_ARRAY[@]}"; do
    if [ -f "${RESULTS_DIR}/set_${i}.txt" ]; then
        result=$(cat ${RESULTS_DIR}/set_${i}.txt)
        rps=$(echo "$result" | grep -oE '[0-9]+\.[0-9]+' | head -1)
        if [ -n "$rps" ]; then
            total_set_rps=$(echo "$total_set_rps + $rps" | bc 2>/dev/null || echo "$total_set_rps")
        fi
        echo "  Node $i: $result"
    fi
done

PIDS=()
for i in "${!POD_ARRAY[@]}"; do
    ip=${POD_IPS[$i]}
    tag=${TAGS[$i]}
    (
        result=$(kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
            -h ${ip} -p 6379 \
            -n $((REQUESTS / POD_COUNT)) \
            -c ${CLIENTS} \
            -q \
            GET "{${tag}}:__rand_int__" 2>/dev/null | tail -1)
        echo "$result" > ${RESULTS_DIR}/get_${i}.txt
    ) &
    PIDS+=($!)
done

for pid in "${PIDS[@]}"; do
    wait $pid 2>/dev/null || true
done

echo ""
total_get_rps=0
echo "GET results:"
for i in "${!POD_ARRAY[@]}"; do
    if [ -f "${RESULTS_DIR}/get_${i}.txt" ]; then
        result=$(cat ${RESULTS_DIR}/get_${i}.txt)
        rps=$(echo "$result" | grep -oE '[0-9]+\.[0-9]+' | head -1)
        if [ -n "$rps" ]; then
            total_get_rps=$(echo "$total_get_rps + $rps" | bc 2>/dev/null || echo "$total_get_rps")
        fi
        echo "  Node $i: $result"
    fi
done

rm -rf ${RESULTS_DIR}

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Aggregate SET: ~${total_set_rps} req/s${NC}"
echo -e "${GREEN}Aggregate GET: ~${total_get_rps} req/s${NC}"
echo -e "${GREEN}========================================${NC}"

echo ""
echo -e "${YELLOW}[4/4] Full benchmark suite on single node...${NC}"
SEED_IP=${POD_IPS[0]}
kubectl exec -n ${NAMESPACE} redis-benchmark -- redis-benchmark \
    -h ${SEED_IP} -p 6379 \
    -n ${REQUESTS} \
    -c ${CLIENTS} \
    -q \
    -t ping,set,get,incr,lpush,rpush,lpop,rpop,sadd,hset,mset 2>/dev/null || true

echo ""
echo -e "${YELLOW}Cleaning up...${NC}"
kubectl delete pod redis-benchmark -n ${NAMESPACE} --ignore-not-found=true >/dev/null 2>&1

echo -e "${GREEN}Done.${NC}"
