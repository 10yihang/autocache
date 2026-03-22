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
NODE_RANGES=()
NODE_SET_RPS=()
NODE_GET_RPS=()
for pod in "${POD_ARRAY[@]}"; do
    ip=$(kubectl get pod -n ${NAMESPACE} ${pod} -o jsonpath='{.status.podIP}')
    POD_IPS+=("$ip")
done

cluster_info_raw=$(kubectl exec -n ${NAMESPACE} ${POD_ARRAY[0]} -- /autocache -cli CLUSTER INFO 2>/dev/null || true)
cluster_state=$(printf '%s\n' "$cluster_info_raw" | grep 'cluster_state:' | cut -d: -f2 | tr -d '\r' || true)
cluster_slots_assigned=$(printf '%s\n' "$cluster_info_raw" | grep 'cluster_slots_assigned:' | cut -d: -f2 | tr -d '\r' || true)

slot_range_for_ip() {
    local ip=$1
    local raw
    raw=$(cluster_slots_raw "${NAMESPACE}" "${POD_IPS[0]}")
    python3 - "$ip" "$raw" <<'PY'
import sys
ip = sys.argv[1]
raw = sys.argv[2]
lines = [line.strip() for line in raw.splitlines() if line.strip()]
for i in range(0, len(lines), 5):
    if i + 2 >= len(lines):
        break
    start = lines[i]
    end = lines[i + 1]
    owner = lines[i + 2]
    if owner == ip:
        print(f"{start}-{end}")
        break
PY
}

find_tag_for_range() {
    local start=$1
    local end=$2
    local candidate tag slot
    for candidate in $(seq 0 4096); do
        tag=$(same_slot_tag "$candidate")
        slot=$(cluster_keyslot "${NAMESPACE}" "${POD_IPS[0]}" "${tag}:probe")
        if [ -n "$slot" ] && [ "$slot" -ge "$start" ] && [ "$slot" -le "$end" ]; then
            printf '%s' "$tag"
            return 0
        fi
    done
    return 1
}

echo -e "${YELLOW}[1/4] Verifying cluster state...${NC}"
printf '%s\n' "$cluster_info_raw" | grep -E "cluster_state|cluster_slots_assigned|cluster_known_nodes" | tr -d '$'
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
    range=$(slot_range_for_ip "$ip")
    start=${range%-*}
    end=${range#*-}
    tag=$(find_tag_for_range "$start" "$end")
    NODE_RANGES[$i]="$range"
    
    echo -e "${GREEN}--- Node $i: ${pod} (${ip}) range=${range} tag=${tag} ---${NC}"
    
    echo -n "  SET: "
    set_result=$(bench_rps "${NAMESPACE}" "${ip}" \
        -n $((REQUESTS / POD_COUNT)) \
        -c ${CLIENTS} \
        -q \
        SET "${tag}:__rand_int__" __data__)
    printf '%s\n' "$set_result"
    NODE_SET_RPS[$i]=$(printf '%s\n' "$set_result" | extract_rps)
    
    echo -n "  GET: "
    get_result=$(bench_rps "${NAMESPACE}" "${ip}" \
        -n $((REQUESTS / POD_COUNT)) \
        -c ${CLIENTS} \
        -q \
        GET "${tag}:__rand_int__")
    printf '%s\n' "$get_result"
    NODE_GET_RPS[$i]=$(printf '%s\n' "$get_result" | extract_rps)
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
    range=$(slot_range_for_ip "$ip")
    start=${range%-*}
    end=${range#*-}
    tag=$(find_tag_for_range "$start" "$end")
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
    range=$(slot_range_for_ip "$ip")
    start=${range%-*}
    end=${range#*-}
    tag=$(find_tag_for_range "$start" "$end")
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

node_rows=""
for i in "${!POD_ARRAY[@]}"; do
    row=$(printf 'Node %s|%s|%s|%s' "$i" "${NODE_RANGES[$i]:-N/A}" "${NODE_SET_RPS[$i]:-N/A}" "${NODE_GET_RPS[$i]:-N/A}")
    if [ -z "$node_rows" ]; then
        node_rows="$row"
    else
        node_rows="$node_rows
$row"
    fi
done

echo ""
print_cluster_summary_table "${cluster_state:-unknown}" "${cluster_slots_assigned:-unknown}" "${POD_COUNT}" "${total_set_rps}" "${total_get_rps}" "$node_rows"

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
