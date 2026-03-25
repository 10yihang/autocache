#!/bin/bash
set -euo pipefail

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

PROFILE_NAME="autocache-dev"
KIND_CLUSTER_NAME="autocache-dev"
USE_KIND=false

echo -e "${GREEN}[1/7] Checking prerequisites...${NC}"
command -v docker >/dev/null 2>&1 || { echo -e "${RED}docker is required but not installed.${NC}"; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo -e "${RED}kubectl is required but not installed.${NC}"; exit 1; }

if command -v kind >/dev/null 2>&1; then
    USE_KIND=true
    echo "Detected kind. Will use kind by default."
else
    command -v colima >/dev/null 2>&1 || {
        echo -e "${RED}Neither kind nor colima is installed.${NC}"
        exit 1
    }
    echo -e "${YELLOW}kind not found. Falling back to colima.${NC}"
fi

echo -e "${GREEN}[2/7] Starting local Kubernetes cluster...${NC}"
if [ "${USE_KIND}" = true ]; then
    if kind get clusters 2>/dev/null | grep -qx "${KIND_CLUSTER_NAME}"; then
        echo "kind cluster '${KIND_CLUSTER_NAME}' is already running."
    else
        kind create cluster --name "${KIND_CLUSTER_NAME}"
    fi
    kubectl config use-context "kind-${KIND_CLUSTER_NAME}"
else
    if colima status -p "${PROFILE_NAME}" >/dev/null 2>&1; then
        echo "Colima profile '${PROFILE_NAME}' is already running."
    else
        colima start -p "${PROFILE_NAME}" --kubernetes --cpu 2 --memory 4
    fi
    kubectl config use-context "colima-${PROFILE_NAME}"
fi

echo -e "${GREEN}[3/7] Building Docker images...${NC}"
echo "Building autocache server image..."
docker build -t autocache:latest -f deploy/docker/Dockerfile.server .

echo "Building autocache operator image..."
docker build -t autocache-operator:latest -f deploy/docker/Dockerfile.operator .

if [ "${USE_KIND}" = true ]; then
    echo "Loading images into kind cluster..."
    kind load docker-image autocache:latest --name "${KIND_CLUSTER_NAME}"
    kind load docker-image autocache-operator:latest --name "${KIND_CLUSTER_NAME}"
fi

echo -e "${GREEN}[4/7] Preparing namespace, CRD, and RBAC...${NC}"
kubectl get namespace system >/dev/null 2>&1 || kubectl create namespace system

kubectl apply -f config/crd/bases/cache.autocache.io_autocaches.yaml
kubectl apply -f config/rbac/service_account.yaml
kubectl apply -f config/rbac/role.yaml
kubectl apply -f config/rbac/role_binding.yaml

echo -e "${GREEN}[5/7] Deploying operator...${NC}"

# Clean up any old deployment first to avoid mixed ReplicaSets from earlier failed runs
kubectl delete deployment autocache-controller-manager -n system --ignore-not-found=true
kubectl delete pod -n system -l control-plane=controller-manager --ignore-not-found=true

TMP_MANAGER_YAML="$(mktemp)"
cp config/manager/manager.yaml "${TMP_MANAGER_YAML}"

# Use locally built image
sed -i.bak 's|image: controller:latest|image: autocache-operator:latest|g' "${TMP_MANAGER_YAML}"

# Fix binary path: image entrypoint/binary is /manager, not /autocache-operator
sed -i.bak 's|- /autocache-operator|- /manager|g' "${TMP_MANAGER_YAML}"

# Current operator image uses a non-numeric user; remove runAsNonRoot to avoid kubelet rejection
sed -i.bak '/runAsNonRoot: true/d' "${TMP_MANAGER_YAML}"

rm -f "${TMP_MANAGER_YAML}.bak"

kubectl apply -f "${TMP_MANAGER_YAML}"
rm -f "${TMP_MANAGER_YAML}"

# Ensure local image is used rather than trying to pull from a registry
kubectl patch deployment autocache-controller-manager \
  -n system \
  --type='json' \
  -p='[
    {"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"Never"}
  ]'

echo "Waiting for operator deployment rollout..."
kubectl rollout status deployment/autocache-controller-manager -n system --timeout=180s

echo -e "${GREEN}[6/7] Creating sample AutoCache cluster...${NC}"
kubectl apply -f config/samples/cache_v1alpha1_autocache.yaml

CLUSTER_NAME=$(grep -m1 '^  name:' config/samples/cache_v1alpha1_autocache.yaml | awk '{print $2}')
echo "Cluster name: ${CLUSTER_NAME}"

echo "Waiting for StatefulSet creation..."
for i in {1..45}; do
    if kubectl get statefulset "${CLUSTER_NAME}" >/dev/null 2>&1; then
        break
    fi
    sleep 2
done

echo "Waiting for AutoCache pods to be ready..."
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=autocache --timeout=240s || true

echo -e "${GREEN}[7/7] Showing status...${NC}"
echo ""
kubectl get pods -A
echo ""
kubectl get deploy -n system
echo ""
kubectl get autocache
echo ""

echo -e "${GREEN}Cluster setup complete!${NC}"
echo ""
echo "Useful commands:"
echo "  kubectl get pods -A"
echo "  kubectl get autocache"
echo "  kubectl logs -n system deploy/autocache-controller-manager"
echo "  kubectl describe autocache ${CLUSTER_NAME}"
echo ""
echo "To connect to Redis:"
echo "  kubectl port-forward svc/${CLUSTER_NAME} 6379:6379"
echo "  redis-cli -p 6379"