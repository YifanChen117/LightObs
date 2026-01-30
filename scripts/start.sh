#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCKER_CONFIG=/tmp/docker-nocreds

mode="${1:-full}"

mkdir -p "${DOCKER_CONFIG}"
printf '{"auths": {}}\n' > "${DOCKER_CONFIG}/config.json"

ensure_kind() {
  if ! kind get clusters | grep -q '^kind$'; then
    kind create cluster
  fi
  kubectl cluster-info >/dev/null
}

build_images() {
  DOCKER_CONFIG="${DOCKER_CONFIG}" docker build -t lightobs-server:dev -f "${ROOT}/build/Dockerfile.server" "${ROOT}"
  DOCKER_CONFIG="${DOCKER_CONFIG}" docker build -t lightobs-agent:dev -f "${ROOT}/build/Dockerfile.agent" "${ROOT}"
}

load_images() {
  kind load docker-image lightobs-server:dev
  kind load docker-image lightobs-agent:dev
}

deploy_lightobs() {
  kubectl apply -f "${ROOT}/deploy/k8s-lightobs.yaml"
  kubectl -n lightobs rollout status deploy/lightobs-server --timeout=120s
  kubectl -n lightobs rollout status ds/lightobs-agent --timeout=120s
}

deploy_demo() {
  kubectl apply -f "${ROOT}/deploy/k8s-demo.yaml"
  kubectl -n demo wait --for=condition=Available deploy/demo-nginx --timeout=120s
  kubectl -n demo wait --for=condition=Ready pod/demo-curl --timeout=120s
  kubectl -n demo get pod -l app=demo-nginx -o wide
  kubectl -n demo get pod demo-curl -o wide
}

show_logs() {
  kubectl -n lightobs logs -l app=lightobs-agent --tail=20
  kubectl -n lightobs logs -l app=lightobs-server --tail=20
}

run_client_query() {
  mkdir -p "${ROOT}/bin"
  go build -o "${ROOT}/bin/lightobs-client" "${ROOT}/cmd/client"
  NGINX_POD_IP="$(kubectl -n demo get pod -l app=demo-nginx -o jsonpath='{.items[0].status.podIP}')"
  kubectl -n lightobs port-forward svc/lightobs-server 8080:8080 >/tmp/lightobs-portforward.log 2>&1 &
  PORT_FORWARD_PID=$!
  sleep 1
  "${ROOT}/bin/lightobs-client" -ip "${NGINX_POD_IP}" -server http://127.0.0.1:8080 || true
  kill "${PORT_FORWARD_PID}" >/dev/null 2>&1 || true
  echo "完成。nginx Pod IP=${NGINX_POD_IP}"
}

case "${mode}" in
  full)
    ensure_kind
    build_images
    load_images
    deploy_lightobs
    deploy_demo
    show_logs
    run_client_query
    ;;
  deploy)
    ensure_kind
    deploy_lightobs
    deploy_demo
    show_logs
    ;;
  restart)
    ensure_kind
    kubectl -n lightobs rollout restart deploy/lightobs-server
    kubectl -n lightobs rollout restart ds/lightobs-agent
    kubectl -n lightobs rollout status deploy/lightobs-server --timeout=120s
    kubectl -n lightobs rollout status ds/lightobs-agent --timeout=120s
    ;;
  query)
    ensure_kind
    run_client_query
    ;;
  build)
    ensure_kind
    build_images
    load_images
    ;;
  *)
    echo "用法: $0 {full|deploy|restart|query|build}"
    echo "full: 创建集群(如需) + 构建镜像 + 导入镜像 + 部署 + demo + 日志 + 查询"
    echo "deploy: 创建集群(如需) + 部署 + demo + 日志"
    echo "restart: 只滚动重启 server/agent"
    echo "query: 仅查询（会自动 port-forward 并运行 client）"
    echo "build: 仅构建并导入镜像"
    exit 1
    ;;
esac
