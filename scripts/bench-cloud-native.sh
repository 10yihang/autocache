#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/bench-common.sh"

MODE=${1:-help}
REQUESTS=${2:-$(default_requests)}
CLIENTS=${3:-$(default_clients)}
DATA_SIZE=${4:-$(default_data_size)}

usage() {
  cat <<EOF
Usage:
  scripts/bench-cloud-native.sh setup
  scripts/bench-cloud-native.sh compare [requests] [clients] [data_size]
  scripts/bench-cloud-native.sh cluster [requests] [clients]
  scripts/bench-cloud-native.sh tiered [requests] [clients] [data_size]
  scripts/bench-cloud-native.sh full [requests] [clients]
  scripts/bench-cloud-native.sh record <mode> [args...]

Modes:
  setup    Start local Kubernetes benchmark environment via cluster-setup.sh
  compare  Run AutoCache vs Redis comparison in Kubernetes
  cluster  Run cluster-focused AutoCache benchmark in Kubernetes
  tiered   Run AutoCache hot+warm benchmark in Kubernetes
  full     Run the full standalone + cluster comparison in Kubernetes
  record   Run a mode and save stdout/stderr to scripts/output/
EOF
}

case "${MODE}" in
  setup)
    exec "$(dirname "$0")/cluster-setup.sh"
    ;;
  compare)
    exec "$(dirname "$0")/bench-vs-redis.sh" "${REQUESTS}" "${CLIENTS}" "${DATA_SIZE}"
    ;;
  cluster)
    exec "$(dirname "$0")/bench-cluster.sh" "${REQUESTS}" "${CLIENTS}"
    ;;
  tiered)
    exec "$(dirname "$0")/bench-tiered.sh" "${REQUESTS}" "${CLIENTS}" "${DATA_SIZE}"
    ;;
  full)
    exec "$(dirname "$0")/bench-vs-redis-full.sh" "${REQUESTS}" "${CLIENTS}"
    ;;
  record)
    shift || true
    submode=${1:-}
    if [ -z "${submode}" ]; then
      echo -e "${RED}record mode requires a submode.${NC}"
      echo
      usage
      exit 1
    fi
    shift || true
    logfile=$(timestamped_output_file "bench-${submode}")
    echo -e "${CYAN}Recording output to ${logfile}${NC}"
    "$(dirname "$0")/bench-cloud-native.sh" "${submode}" "$@" | tee "${logfile}"
    ;;
  help|-h|--help)
    usage
    ;;
  *)
    echo -e "${RED}Unknown mode: ${MODE}${NC}"
    echo
    usage
    exit 1
    ;;
esac
