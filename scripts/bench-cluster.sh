#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/bench-common.sh"

CLUSTER_NAME=$(sample_cluster_name)
NAMESPACE=${NAMESPACE:-default}
REQUESTS=${1:-$(default_requests)}
CLIENTS=${2:-$(default_clients)}

echo -e "${GREEN}=== AutoCache Multi-Node Cluster Benchmark ===${NC}"
echo "Cluster: ${CLUSTER_NAME}"
print_bench_config "${REQUESTS}" "${CLIENTS}"
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

echo -e "${YELLOW}Waiting for benchmark pod...${NC}"
ensure_benchmark_pod "${NAMESPACE}"

echo ""
echo -e "${YELLOW}[2/4] Testing individual nodes...${NC}"
echo "Each node handles keys in its own slot range"
echo ""

bench_rps() {
    local output
    output=$(run_k8s_benchmark "$@")
    printf '%s\n' "$output" | tr '\r' '\n' | grep 'requests per second' | tail -1
}

for i in "${!POD_ARRAY[@]}"; do
    pod=${POD_ARRAY[$i]}
    ip=${POD_IPS[$i]}
    tag=$(same_slot_tag "$i")
    
    echo -e "${GREEN}--- Node $i: ${pod} (${ip}) tag=${tag} ---${NC}"
    
    echo -n "  SET: "
    bench_rps "${NAMESPACE}" "${ip}" \
        -n $((REQUESTS / POD_COUNT)) \
        -c ${CLIENTS} \
        -q \
        SET "${tag}:__rand_int__" __data__ || echo "failed"
    
    echo -n "  GET: "
    bench_rps "${NAMESPACE}" "${ip}" \
        -n $((REQUESTS / POD_COUNT)) \
        -c ${CLIENTS} \
        -q \
        GET "${tag}:__rand_int__" || echo "failed"
    echo ""
done

echo -e "${YELLOW}[3/4] Parallel benchmark (all nodes simultaneously)...${NC}"
echo "Running SET/GET on all ${POD_COUNT} nodes in parallel"
echo ""

RESULTS_DIR="/tmp/autocache-bench-$$"
mkdir -p ${RESULTS_DIR}

PIDS=()
for i in "${!POD_ARRAY[@]}"; do
    ip=${POD_IPS[$i]}
    tag=$(same_slot_tag "$i")
    (
        result=$(bench_rps "${NAMESPACE}" "${ip}" \
            -n $((REQUESTS / POD_COUNT)) \
            -c ${CLIENTS} \
            -q \
            SET "${tag}:__rand_int__" __data__)
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
        rps=$(printf '%s\n' "$result" | extract_rps)
        if [ -n "$rps" ]; then
            total_set_rps=$(echo "$total_set_rps + $rps" | bc 2>/dev/null || echo "$total_set_rps")
        fi
        echo "  Node $i: $result"
    fi
done

PIDS=()
for i in "${!POD_ARRAY[@]}"; do
    ip=${POD_IPS[$i]}
    tag=$(same_slot_tag "$i")
    (
        result=$(bench_rps "${NAMESPACE}" "${ip}" \
            -n $((REQUESTS / POD_COUNT)) \
            -c ${CLIENTS} \
            -q \
            GET "${tag}:__rand_int__")
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
        rps=$(printf '%s\n' "$result" | extract_rps)
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
    -t ping,set,get,incr,lpush,rpush,lpop,rpop,sadd,hset,mset 2>/dev/null | tr '\r' '\n' || true

echo ""
echo -e "${YELLOW}Cleaning up...${NC}"
delete_pod_if_exists "${NAMESPACE}" redis-benchmark

echo -e "${GREEN}Done.${NC}"
