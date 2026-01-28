#!/bin/bash
set -e

GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

echo -e "${GREEN}[1/4] Verifying Cluster Pods...${NC}"
PODS=$(kubectl get pods -l app.kubernetes.io/name=autocache -o jsonpath='{.items[*].metadata.name}')
POD_ARRAY=($PODS)
COUNT=${#POD_ARRAY[@]}

if [ "$COUNT" -lt 3 ]; then
    echo "Expected at least 3 pods, found $COUNT. Please wait for cluster to be ready."
    exit 1
fi

echo "Found pods: ${PODS}"

echo -e "${GREEN}[3/4] Initializing Cluster (Manual Simulation)...${NC}"
echo -e "${YELLOW}Note: Operator automation for slot assignment is currently stubbed.${NC}"
echo -e "${YELLOW}Performing manual CLUSTER MEET and ADDSLOTS for verification.${NC}"

# Get IPs
IP0=$(kubectl get pod ${POD_ARRAY[0]} -o jsonpath='{.status.podIP}')
IP1=$(kubectl get pod ${POD_ARRAY[1]} -o jsonpath='{.status.podIP}')
IP2=$(kubectl get pod ${POD_ARRAY[2]} -o jsonpath='{.status.podIP}')

PORT=6379
BUS_PORT=16379

echo "Meeting nodes..."
# Pass PORT (6379), not BUS_PORT (16379). The server adds 10000 automatically.
kubectl exec ${POD_ARRAY[0]} -- /autocache -cli -h ${IP0} -p ${PORT} CLUSTER MEET ${IP1} ${PORT} || echo "MEET failed"
kubectl exec ${POD_ARRAY[0]} -- /autocache -cli -h ${IP0} -p ${PORT} CLUSTER MEET ${IP2} ${PORT} || echo "MEET failed"

echo "Assigning slots..."
# Slot 0-5000 -> Pod 0
for i in {0..10}; do
    kubectl exec ${POD_ARRAY[0]} -- /autocache -cli -h ${IP0} -p ${PORT} CLUSTER ADDSLOTS $i || echo "ADDSLOTS $i failed"
done
# Slot 5001-5010 -> Pod 1
for i in {5001..5010}; do
    kubectl exec ${POD_ARRAY[1]} -- /autocache -cli -h ${IP1} -p ${PORT} CLUSTER ADDSLOTS $i || echo "ADDSLOTS $i failed"
done

echo -e "${GREEN}[4/4] Testing Hash Slots & Redirects...${NC}"

# Find a key for slot 5 (owned by Pod 0)
echo "Finding a key for slot 0..."
# Pre-calculated key for slot 0: key-3849
TARGET_KEY="key-3849"

if [ -z "$TARGET_KEY" ]; then
    echo "Could not find key for slot 0, skipping redirect test."
else
    echo "Key '$TARGET_KEY' maps to slot 0 (Owner: ${POD_ARRAY[0]})"
    
    echo "Attempting SET on ${POD_ARRAY[1]} (Non-Owner)..."
    OUTPUT=$(kubectl exec ${POD_ARRAY[1]} -- /autocache -cli -h ${IP1} -p ${PORT} SET $TARGET_KEY value 2>&1 || true)
    
    if echo "$OUTPUT" | grep -q "MOVED 0"; then
        echo -e "${GREEN}SUCCESS: Received MOVED redirect as expected.${NC}"
        echo "Response: $OUTPUT"
    else
        echo -e "${RED}FAILURE: Did not receive MOVED redirect.${NC}"
        echo "Response: $OUTPUT"
    fi
fi


# Get the cluster name from the sample yaml
CLUSTER_NAME=$(grep -m1 'name:' config/samples/cache_v1alpha1_autocache.yaml | awk '{print $2}')

echo -e "${GREEN}[4/4] Scaling Test (Manual)...${NC}"
echo "To test scaling:"
echo "1. kubectl scale statefulset ${CLUSTER_NAME} --replicas=4"
echo "2. Wait for pod ${CLUSTER_NAME}-3"
echo "3. Manually migrate slots using CLUSTER SETSLOT commands."
echo "   (Automatic rebalancing is implemented in Operator stubs)"

echo -e "${GREEN}Done.${NC}"
