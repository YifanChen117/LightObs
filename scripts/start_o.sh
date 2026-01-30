#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCKER_CONFIG=/tmp/docker-nocreds

mkdir -p "${DOCKER_CONFIG}"
printf '{"auths": {}}\n' > "${DOCKER_CONFIG}/config.json"

if ! kind get clusters | grep -q '^kind$'; then
  kind create cluster
fi

kubectl cluster-info >/dev/null

# docker build -t lightobs-server:dev -f "${ROOT}/build/Dockerfile.server" "${ROOT}"
# docker build -t lightobs-agent:dev -f "${ROOT}/build/Dockerfile.agent" "${ROOT}"

kind load docker-image lightobs-server:dev
kind load docker-image lightobs-agent:dev

kubectl apply -f "${ROOT}/deploy/k8s-lightobs.yaml"
kubectl -n lightobs rollout status deploy/lightobs-server --timeout=120s
kubectl -n lightobs rollout status ds/lightobs-agent --timeout=120s

kubectl apply -f "${ROOT}/deploy/k8s-demo.yaml"
kubectl get pod -l app=demo-nginx -o wide
kubectl get pod demo-curl -o wide

kubectl -n lightobs logs -l app=lightobs-agent --tail=20
kubectl -n lightobs logs -l app=lightobs-server --tail=20

echo "完成。可运行：kubectl -n lightobs port-forward svc/lightobs-server 8080:8080"
echo "然后运行：go build -o bin/lightobs-client ./cmd/client && ./bin/lightobs-client -ip 10.244.0.7 -server http://127.0.0.1:8080"
