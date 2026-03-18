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
  local timeout=${3:-120s}
  kubectl wait --for=condition=ready "pod/${pod}" -n "${namespace}" --timeout="${timeout}"
}

delete_pod_if_exists() {
  local namespace=$1
  shift
  kubectl delete pod "$@" -n "${namespace}" --ignore-not-found=true >/dev/null 2>&1 || true
}

ensure_benchmark_pod() {
  local namespace=$1
  local image=${2:-docker.1ms.run/redis:7-alpine}
  delete_pod_if_exists "${namespace}" redis-benchmark
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
