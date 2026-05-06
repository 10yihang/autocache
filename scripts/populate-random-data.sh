#!/usr/bin/env bash
# Continuously populate an AutoCache / Redis cluster with random data.
# Usage: ./scripts/populate-random-data.sh [host:port]  or  [port]
#   Default: 127.0.0.1:7001 (cluster-aware, follows MOVED redirects)
#   Examples:
#     ./scripts/populate-random-data.sh 7001
#     ./scripts/populate-random-data.sh 192.168.1.100:6379
#   Ctrl+C to stop.

CLUSTER_ARG="${1:-127.0.0.1:7001}"
if [[ "$CLUSTER_ARG" == *:* ]]; then
  CLUSTER_HOST="${CLUSTER_ARG%:*}"
  CLUSTER_PORT="${CLUSTER_ARG##*:}"
else
  CLUSTER_HOST="127.0.0.1"
  CLUSTER_PORT="$CLUSTER_ARG"
fi

CHARS="abcdefghijklmnopqrstuvwxyz0123456789"
N=${#CHARS}

info()  { printf '\033[36m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }

# Pure-bash: echo 4 random chars (no forks, no subshells, no external bins)
r4() { local o="" i=0; while [ $i -lt 4 ]; do o="${o}${CHARS:$((RANDOM%N)):1}"; i=$((i+1)); done; echo "$o"; }
r6() { local o="" i=0; while [ $i -lt 6 ]; do o="${o}${CHARS:$((RANDOM%N)):1}"; i=$((i+1)); done; echo "$o"; }
r8() { local o="" i=0; while [ $i -lt 8 ]; do o="${o}${CHARS:$((RANDOM%N)):1}"; i=$((i+1)); done; echo "$o"; }
r16() { local o="" i=0; while [ $i -lt 16 ]; do o="${o}${CHARS:$((RANDOM%N)):1}"; i=$((i+1)); done; echo "$o"; }

info "Starting random data population → ${CLUSTER_HOST}:${CLUSTER_PORT}"
info "Press Ctrl+C to stop."
echo ""

H=$CLUSTER_HOST
P=$CLUSTER_PORT
ops=0 start=$(date +%s)
while true; do
  key="k:$(r4):$(r6)"
  val="$(r16)"
  fld="f:$(r4)"
  mem="m:$(r8)"
  score=$((RANDOM % 1000))
  r=$((RANDOM % 100))

  if   [ $r -lt 30 ]; then redis-cli -c -h "$H" -p "$P" set    "$key" "$val"         >/dev/null 2>&1
  elif [ $r -lt 45 ]; then redis-cli -c -h "$H" -p "$P" hset   "$key" "$fld" "$val"  >/dev/null 2>&1
  elif [ $r -lt 55 ]; then redis-cli -c -h "$H" -p "$P" hget   "$key" "$fld"         >/dev/null 2>&1
  elif [ $r -lt 65 ]; then redis-cli -c -h "$H" -p "$P" sadd   "$key" "$mem"         >/dev/null 2>&1
  elif [ $r -lt 73 ]; then redis-cli -c -h "$H" -p "$P" zadd   "$key" "$score" "$mem" >/dev/null 2>&1
  elif [ $r -lt 81 ]; then redis-cli -c -h "$H" -p "$P" lpush  "$key" "$val"         >/dev/null 2>&1
  elif [ $r -lt 88 ]; then redis-cli -c -h "$H" -p "$P" get    "$key"                >/dev/null 2>&1
  elif [ $r -lt 93 ]; then redis-cli -c -h "$H" -p "$P" incr   "$key"                >/dev/null 2>&1
  elif [ $r -lt 97 ]; then redis-cli -c -h "$H" -p "$P" del    "$key"                >/dev/null 2>&1
  else                      redis-cli -c -h "$H" -p "$P" expire "$key" "$((60 + RANDOM % 600))" >/dev/null 2>&1
  fi

  ops=$((ops + 1))
  if [ $((ops % 500)) -eq 0 ]; then
    now=$(date +%s); elapsed=$((now - start))
    if [ $elapsed -gt 0 ]; then rate=$((ops / elapsed)); else rate=0; fi
    keys=$(redis-cli -c -h "$H" -p "$P" dbsize 2>&1 || echo "?")
    info "ops=$ops | rate=${rate}/s | keys=$keys"
  fi
done
