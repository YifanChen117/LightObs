#!/usr/bin/env bash
set -euo pipefail

# 确保国内环境下载依赖顺畅
export GOPROXY=https://goproxy.cn,direct

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

ensure_tracefs() {
  if ! command -v docker >/dev/null 2>&1; then
    echo "未检测到 docker，请先启用 Docker Desktop 的 WSL 集成或安装 Docker Engine"
    exit 1
  fi
  for n in $(kind get nodes); do
    docker exec "$n" sh -c "mount | grep -q '/sys/kernel/tracing' || mount -t tracefs tracefs /sys/kernel/tracing"
    docker exec "$n" sh -c "mount | grep -q '/sys/kernel/tracing'"
    docker exec "$n" sh -c "mount | grep -q '/sys/kernel/debug' || mount -t debugfs debugfs /sys/kernel/debug"
    docker exec "$n" sh -c "mount | grep -q '/sys/kernel/debug'"
  done
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

run_local() {
  if [[ "${EUID}" -ne 0 ]]; then
    if ! sudo -n true 2>/dev/null; then
      echo "请使用 sudo 运行：sudo bash scripts/start.sh local"
      exit 1
    fi
  fi
  mkdir -p "${ROOT}/bin"
  go build -o "${ROOT}/bin/lightobs-server" "${ROOT}/cmd/server"
  go build -o "${ROOT}/bin/lightobs-agent"  "${ROOT}/cmd/agent"
  go build -o "${ROOT}/bin/lightobs-client" "${ROOT}/cmd/client"
  "${ROOT}/bin/lightobs-server" -listen :8080 -db-driver sqlite -db "${ROOT}/traffic.sqlite" >/tmp/lightobs-server.log 2>&1 &
  SERVER_PID=$!
  sleep 1
  IFACE="$(ip route show default 2>/dev/null | awk '/default/ {print $5; exit}')"
  if [[ -z "${IFACE:-}" ]]; then
    IFACE="$(ip -o link show | awk -F': ' '{print $2}' | grep -E '^(eth|ens|enp)' | head -n1)"
  fi
  if [[ -z "${IFACE:-}" ]]; then
    echo "无法自动检测网卡名，请手动运行 agent 并指定 -interface"
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    exit 1
  fi
  sudo -n "${ROOT}/bin/lightobs-agent" -interface "${IFACE}" -server-ip 127.0.0.1 -server-port 8080 -enable-ebpf=true >/tmp/lightobs-agent.log 2>&1 &
  AGENT_PID=$!
  sleep 1
  # 确保获取 IPv4 地址
  TARGET_IP="$(getent ahostsv4 neverssl.com | awk '{print $1; exit}')"
  if [[ -z "${TARGET_IP}" ]]; then
    echo "无法解析 neverssl.com (IPv4)，尝试使用 1.1.1.1"
    TARGET_IP="1.1.1.1"
  fi

  echo "产生测试流量: curl http://${TARGET_IP} ..."
  # 强制使用 IPv4
  curl -4 -s "http://${TARGET_IP}" >/dev/null || true
  sleep 2
  "${ROOT}/bin/lightobs-client" -ip "${TARGET_IP}" -server http://127.0.0.1:8080 || true
  kill "${AGENT_PID}"  >/dev/null 2>&1 || true
  kill "${SERVER_PID}" >/dev/null 2>&1 || true
  echo "本地跑通完成：interface=${IFACE} target_ip=${TARGET_IP}"
}

case "${mode}" in
  full)
    ensure_kind
    build_images
    load_images
    ensure_tracefs
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
  local)
    run_local
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
    echo "local: 本地启动 server/agent + 生成流量 + 查询"
    echo "build: 仅构建并导入镜像"
    exit 1
    ;;
esac
