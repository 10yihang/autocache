#!/bin/bash
set -e

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}[1/6] Checking prerequisites...${NC}"
command -v colima >/dev/null 2>&1 || { echo -e "${RED}colima is required but not installed.${NC}"; exit 1; }
command -v docker >/dev/null 2>&1 || { echo -e "${RED}docker (cli) is required but not installed.${NC}"; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo -e "${RED}kubectl is required but not installed.${NC}"; exit 1; }

PROFILE_NAME="autocache-dev"

echo -e "${GREEN}[2/6] Starting Colima profile '${PROFILE_NAME}' with Kubernetes...${NC}"
if colima status -p ${PROFILE_NAME} >/dev/null 2>&1; then
    echo "Colima profile is running."
else
    colima start -p ${PROFILE_NAME} --kubernetes --cpu 2 --memory 4
fi

echo -e "${GREEN}[3/6] Building Docker images...${NC}"
echo "Building autocache server image..."
docker build -t autocache:latest -f deploy/docker/Dockerfile.server .
echo "Building autocache operator image..."
docker build -t autocache-operator:latest -f deploy/docker/Dockerfile.operator .

echo -e "${GREEN}[4/6] Configuring Kubectl...${NC}"
kubectl config use-context colima-${PROFILE_NAME}

echo -e "${GREEN}[5/6] Deploying Operator...${NC}"
# Install CRD
kubectl apply -f config/crd/bases/cache.autocache.io_autocaches.yaml

# Install RBAC
kubectl apply -f config/rbac/role.yaml
kubectl create clusterrolebinding autocache-operator-rolebinding --clusterrole=autocache-operator --serviceaccount=default:default --dry-run=client -o yaml | kubectl apply -f -

# Deploy Operator Deployment
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: autocache-operator
  namespace: default
  labels:
    control-plane: controller-manager
spec:
  replicas: 1
  selector:
    matchLabels:
      control-plane: controller-manager
  template:
    metadata:
      labels:
        control-plane: controller-manager
    spec:
      containers:
      - name: manager
        image: autocache-operator:latest
        imagePullPolicy: Never
        command:
        - /manager
        resources:
          limits:
            cpu: 500m
            memory: 128Mi
          requests:
            cpu: 10m
            memory: 64Mi
EOF

echo "Waiting for operator to be ready..."
kubectl wait --for=condition=available deployment/autocache-operator --timeout=60s

echo -e "${GREEN}[6/6] Creating Sample AutoCache Cluster...${NC}"
kubectl apply -f config/samples/cache_v1alpha1_autocache.yaml

# Get the cluster name from the sample yaml
CLUSTER_NAME=$(grep -m1 'name:' config/samples/cache_v1alpha1_autocache.yaml | awk '{print $2}')
echo "Cluster name: ${CLUSTER_NAME}"

echo "Waiting for AutoCache pods to be ready (this may take a minute)..."
# Wait loop for statefulset
for i in {1..30}; do
    if kubectl get statefulset ${CLUSTER_NAME} >/dev/null 2>&1; then
        break
    fi
    echo "Waiting for StatefulSet creation..."
    sleep 2
done

kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=autocache --timeout=120s || true

echo -e "${GREEN}Cluster setup complete!${NC}"
echo ""
echo "To verify:"
echo "  kubectl get pods"
echo "  kubectl get autocache"
echo ""
echo "To connect to Redis:"
echo "  kubectl port-forward svc/${CLUSTER_NAME} 6379:6379"
echo "  redis-cli -p 6379"
