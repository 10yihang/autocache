#!/usr/bin/env bash

set -euo pipefail

base_url="${1:-http://127.0.0.1:8080}"

create_paste() {
  curl -sS -X POST "${base_url}/api/paste" \
    -H 'Content-Type: application/json' \
    -d "$1"
}

read_paste_json() {
  curl -sS "${base_url}/api/paste/$1"
}

read_paste_raw() {
  curl -sS "${base_url}/raw/$1"
}

extract_code() {
  printf '%s' "$1" | python3 -c 'import json,sys; print(json.load(sys.stdin)["code"])'
}

echo "Creating standard paste"
standard_response="$(create_paste '{"content":"demo standard paste","ttl":"1h"}')"
printf '%s\n' "$standard_response"
standard_code="$(extract_code "$standard_response")"
echo "Reading standard paste JSON ${standard_code}"
read_paste_json "$standard_code"
echo
echo "Reading standard paste RAW ${standard_code}"
read_paste_raw "$standard_code"
echo

echo "Creating max-view paste"
limited_response="$(create_paste '{"content":"demo limited paste","ttl":"1h","max_views":2}')"
printf '%s\n' "$limited_response"
limited_code="$(extract_code "$limited_response")"
echo "Reading max-view paste ${limited_code} twice"
read_paste_json "$limited_code"
echo
read_paste_json "$limited_code"
echo

echo "Creating burn-after-read paste"
burn_response="$(create_paste '{"content":"demo burn paste","ttl":"1h","burn_after_read":true}')"
printf '%s\n' "$burn_response"
burn_code="$(extract_code "$burn_response")"
echo "Reading burn-after-read paste ${burn_code} (json then raw should fail)"
read_paste_json "$burn_code"
echo
curl -sS -o /dev/null -w 'raw status after burn: %{http_code}\n' "${base_url}/raw/${burn_code}"
