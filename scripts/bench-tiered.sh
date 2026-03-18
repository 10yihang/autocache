#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/bench-common.sh"

NAMESPACE=${NAMESPACE:-default}
REQUESTS=${1:-$(default_requests)}
CLIENTS=${2:-$(default_clients)}
DATA_SIZE=${3:-$(default_data_size)}

print_section "=== AutoCache Hot+Warm Tiered Benchmark ==="
print_bench_config "${REQUESTS}" "${CLIENTS}" "${DATA_SIZE}"
echo

cleanup() {
  echo -e "${YELLOW}Cleaning up tiered benchmark resources...${NC}"
  delete_pod_if_exists "${NAMESPACE}" autocache-tiered redis-benchmark
}
trap cleanup EXIT

print_section "[1/4] Deploying tiered AutoCache pod..."
delete_pod_if_exists "${NAMESPACE}" autocache-tiered

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: autocache-tiered
  namespace: ${NAMESPACE}
  labels:
    app: autocache-tiered
spec:
  containers:
    - name: autocache
      image: autocache:latest
      imagePullPolicy: Never
      command:
        - /autocache
        - --data-dir
        - /data
        - --metrics-addr
        - :9121
        - --tiered-enabled
        - --badger-path
        - /data/warm
      ports:
        - containerPort: 6379
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      emptyDir: {}
EOF

wait_for_pod_ready "${NAMESPACE}" autocache-tiered
AUTOCACHE_IP=$(pod_ip "${NAMESPACE}" autocache-tiered)
echo "Tiered AutoCache ready at ${AUTOCACHE_IP}"

print_section "[2/4] Ensuring benchmark pod..."
ensure_benchmark_pod "${NAMESPACE}"

print_section "[3/4] Running tiered benchmark..."
echo -n "  SET: "
run_k8s_benchmark "${NAMESPACE}" "${AUTOCACHE_IP}" -n "${REQUESTS}" -c "${CLIENTS}" -d "${DATA_SIZE}" -q SET 'tiered:__rand_int__' __data__ | tr '\r' '\n' | grep 'requests per second' | tail -1
echo -n "  GET: "
run_k8s_benchmark "${NAMESPACE}" "${AUTOCACHE_IP}" -n "${REQUESTS}" -c "${CLIENTS}" -d "${DATA_SIZE}" -q GET 'tiered:__rand_int__' | tr '\r' '\n' | grep 'requests per second' | tail -1

print_section "[4/4] Metrics spot check..."
kubectl exec -n "${NAMESPACE}" autocache-tiered -- wget -qO- http://127.0.0.1:9121/metrics 2>/dev/null | grep -E 'autocache_tiered_(keys|migrations)_total' | head -5 || true

echo -e "${GREEN}Tiered benchmark complete.${NC}"
