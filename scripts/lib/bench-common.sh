#!/usr/bin/env bash

GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

bench_repo_root() {
  cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo -e "${RED}Error: required command '$1' is not installed.${NC}"
    exit 1
  }
}

sample_cluster_name() {
  grep -m1 '^  name:' "$(bench_repo_root)/config/samples/cache_v1alpha1_autocache.yaml" | awk '{print $2}'
}

wait_for_pod_ready() {
  local namespace=$1
  local pod=$2
  local timeout=${3:-120}
  timeout=${timeout%s}
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    if kubectl get pod "$pod" -n "$namespace" >/dev/null 2>&1; then
      ready=$(kubectl get pod "$pod" -n "$namespace" -o jsonpath='{.status.containerStatuses[0].ready}' 2>/dev/null || true)
      if [ "$ready" = "true" ]; then
        return 0
      fi
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
  echo -e "${RED}Timed out waiting for pod/${pod} to become Ready in namespace ${namespace}.${NC}"
  kubectl get pod "$pod" -n "$namespace" -o wide || true
  return 1
}

delete_pod_if_exists() {
  local namespace=$1
  shift
  kubectl delete pod "$@" -n "${namespace}" --ignore-not-found=true --wait=true >/dev/null 2>&1 || true
}

ensure_benchmark_pod() {
  local namespace=$1
  local image=${2:-docker.1ms.run/redis:7-alpine}
  if kubectl get pod redis-benchmark -n "${namespace}" >/dev/null 2>&1; then
    kubectl delete pod redis-benchmark -n "${namespace}" --ignore-not-found=true --wait=true >/dev/null 2>&1 || true
  fi
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: redis-benchmark
  namespace: ${namespace}
spec:
  restartPolicy: Never
  containers:
  - name: benchmark
    image: ${image}
    command: ["sleep", "3600"]
EOF
  wait_for_pod_ready "${namespace}" redis-benchmark
}

pod_ip() {
  local namespace=$1
  local pod=$2
  kubectl get pod -n "${namespace}" "${pod}" -o jsonpath='{.status.podIP}'
}

extract_rps() {
  grep 'requests per second' | tail -1 | grep -oE '[0-9]+\.[0-9]+' | head -1
}

run_k8s_benchmark() {
  local namespace=$1
  local host=$2
  shift 2
  kubectl exec -n "${namespace}" redis-benchmark -- redis-benchmark -h "${host}" -p 6379 "$@" 2>/dev/null
}

cluster_slots_raw() {
  local namespace=$1
  local host=$2
  kubectl exec -n "${namespace}" redis-benchmark -- redis-cli --raw -h "${host}" -p 6379 cluster slots 2>/dev/null
}

cluster_keyslot() {
  local namespace=$1
  local host=$2
  local key=$3
  kubectl exec -n "${namespace}" redis-benchmark -- redis-cli -h "${host}" -p 6379 cluster keyslot "$key" 2>/dev/null | tr -dc '0-9'
}

cluster_owner_for_slot() {
  local namespace=$1
  local host=$2
  local slot=$3
  local raw
  raw=$(cluster_slots_raw "${namespace}" "${host}")
  python3 - "$slot" "$raw" <<'PY'
import sys
slot = int(sys.argv[1])
raw = sys.argv[2]
lines = [line.strip() for line in raw.splitlines() if line.strip()]
for i in range(0, len(lines), 5):
    if i + 2 >= len(lines):
        break
    start = int(lines[i])
    end = int(lines[i + 1])
    ip = lines[i + 2]
    if start <= slot <= end:
        print(ip)
        break
PY
}

safe_bench_output() {
  "$@" 2>&1 || true
}

same_slot_tag() {
  printf '{bench-%s}' "$1"
}

bench_output_dir() {
  local root
  root="$(bench_repo_root)/scripts/output"
  mkdir -p "${root}"
  printf '%s' "${root}"
}

timestamped_output_file() {
  local prefix=$1
  local dir
  dir=$(bench_output_dir)
  printf '%s/%s-%s.log' "${dir}" "${prefix}" "$(date +%Y%m%d-%H%M%S)"
}

default_requests() {
  printf '%s' "${REQUESTS:-100000}"
}

default_clients() {
  printf '%s' "${CLIENTS:-50}"
}

default_data_size() {
  printf '%s' "${DATA_SIZE:-100}"
}

print_bench_config() {
  local requests=$1
  local clients=$2
  local data_size=${3:-}
  echo "Configuration:"
  echo "  Requests: ${requests}"
  echo "  Clients:  ${clients}"
  if [ -n "${data_size}" ]; then
    echo "  Data size: ${data_size} bytes"
  fi
}

print_section() {
  echo -e "${YELLOW}$1${NC}"
}
