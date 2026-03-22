#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/bench-common.sh"

output=$(print_cluster_summary_table \
  "ok" \
  "16384" \
  "3" \
  "12670.67" \
  "25870.05" \
  $'Node 0|0-5461|6946.86|26109.66\nNode 1|5462-10922|6190.03|17889.09\nNode 2|10923-16383|6980.80|28409.09')

printf '%s\n' "$output"

grep -q "CLUSTER PERFORMANCE SUMMARY" <<<"$output"
grep -q "cluster_state" <<<"$output"
grep -q "Aggregate SET" <<<"$output"
grep -q "Node 1" <<<"$output"
