#!/usr/bin/env bash
# start_with_operator.sh ── 启动 LiteObs + Operator 完整演示环境
# 用法：
#   bash scripts/start_with_operator.sh [mode]
#
# mode:
#   full      (默认) 创建集群 + 构建镜像 + 导入 + 部署全套 + demo + 创建CR + 验证
#   build     仅构建并导入三个镜像（server / agent / operator）
#   deploy    集群已存在 + 镜像已导入 → 仅执行部署 + demo + CR
#   cr        仅创建/更新 PodObservation CR（镜像和服务均已就绪）
#   verify    仅验证 Operator 是否成功运作（pobs status + agent pod + client查询）
#   restart   滚动重启 server 和 operator
#   query     port-forward 后执行 client 查询

set -euo pipefail

export GOPROXY=https://goproxy.cn,direct

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCKER_CONFIG=/tmp/docker-nocreds

MODE="${1:-full}"

# ── 颜色输出 ──────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; RESET='\033[0m'
info()    { echo -e "${CYAN}[INFO]${RESET}  $*"; }
success() { echo -e "${GREEN}[OK]${RESET}    $*"; }
warn()    { echo -e "${YELLOW}[WARN]${RESET}  $*"; }

# ── Docker config（避免凭证文件干扰 kind 加载）─────────────
mkdir -p "${DOCKER_CONFIG}"
printf '{"auths": {}}\n' > "${DOCKER_CONFIG}/config.json"

# ══════════════════════════════════════════════════════════
#  函数定义
# ══════════════════════════════════════════════════════════

ensure_kind() {
  if ! kind get clusters 2>/dev/null | grep -q '^kind$'; then
    info "集群不存在，正在创建 kind 集群..."
    kind create cluster
  fi
  kubectl cluster-info >/dev/null
  success "kind 集群已就绪"
}

build_images() {
  info "构建 lightobs-server:dev ..."
  DOCKER_CONFIG="${DOCKER_CONFIG}" docker build \
    -t lightobs-server:dev -f "${ROOT}/build/Dockerfile.server" "${ROOT}"

  info "构建 lightobs-agent:dev ..."
  DOCKER_CONFIG="${DOCKER_CONFIG}" docker build \
    -t lightobs-agent:dev -f "${ROOT}/build/Dockerfile.agent" "${ROOT}"

  info "构建 lightobs-operator:dev ..."
  DOCKER_CONFIG="${DOCKER_CONFIG}" docker build \
    -t lightobs-operator:dev -f "${ROOT}/build/Dockerfile.operator" "${ROOT}"

  success "三个镜像均已构建完成"
}

load_images() {
  info "导入镜像到 kind 集群..."
  kind load docker-image lightobs-server:dev
  kind load docker-image lightobs-agent:dev
  kind load docker-image lightobs-operator:dev
  success "镜像导入完成"
}

ensure_tracefs() {
  if ! command -v docker >/dev/null 2>&1; then
    echo "未检测到 docker，请先启用 Docker Desktop 的 WSL 集成或安装 Docker Engine"
    exit 1
  fi
  info "挂载 tracefs / debugfs 到各 kind 节点..."
  for n in $(kind get nodes); do
    docker exec "$n" sh -c \
      "mount | grep -q '/sys/kernel/tracing' || mount -t tracefs tracefs /sys/kernel/tracing"
    docker exec "$n" sh -c \
      "mount | grep -q '/sys/kernel/debug' || mount -t debugfs debugfs /sys/kernel/debug"
  done
  success "tracefs / debugfs 挂载完成"
}

deploy_lightobs_server() {
  info "部署 lightobs namespace + server + service..."
  kubectl apply -f "${ROOT}/deploy/k8s-lightobs.yaml"

  info "等待 lightobs-server 就绪..."
  kubectl -n lightobs rollout status deploy/lightobs-server --timeout=120s
  success "lightobs-server 已就绪"
}

apply_crd() {
  info "安装 PodObservation CRD..."
  kubectl apply -f "${ROOT}/deploy/crds/podobservation.yaml"
  # 等待 CRD established
  kubectl wait --for=condition=Established \
    crd/podobservations.lightobs.io --timeout=60s
  success "CRD podobservations.lightobs.io 已就绪"
}

deploy_operator() {
  info "部署 lightobs-operator（ServiceAccount / ClusterRole / Deployment）..."

  # operator.yaml 中 image 字段是 lightobs-operator:latest，
  # 需替换成本地 dev tag，或在 apply 后 patch imagePullPolicy
  kubectl apply -f "${ROOT}/deploy/operator/operator.yaml"

  # 将镜像替换为本次构建的 dev tag（kind 内已 load）
  if docker image inspect lightobs-operator:dev >/dev/null 2>&1; then
    kind load docker-image lightobs-operator:dev >/dev/null 2>&1 || true
  fi
  kubectl -n lightobs set image deploy/lightobs-operator \
    operator=lightobs-operator:dev 2>/dev/null || true
  kubectl -n lightobs patch deploy/lightobs-operator \
    -p '{"spec":{"template":{"spec":{"containers":[{"name":"operator","imagePullPolicy":"IfNotPresent"}]}}}}' 2>/dev/null || true
  kubectl -n lightobs rollout restart deploy/lightobs-operator >/dev/null 2>&1 || true

  info "等待 lightobs-operator 就绪..."
  kubectl -n lightobs rollout status deploy/lightobs-operator --timeout=30s
  success "lightobs-operator 已就绪"
}

deploy_demo() {
  info "部署 demo namespace（nginx + curl）..."
  kubectl -n demo delete pod demo-curl --ignore-not-found >/dev/null 2>&1 || true
  kubectl apply -f "${ROOT}/deploy/k8s-demo.yaml"
  kubectl -n demo wait --for=condition=Available deploy/demo-nginx --timeout=120s
  kubectl -n demo wait --for=condition=Ready pod/demo-curl --timeout=120s
  success "demo 环境已就绪"
  kubectl -n demo get pod -l app=demo-nginx -o wide
  kubectl -n demo get pod demo-curl -o wide
}

create_pobs_cr() {
  info "创建 PodObservation CR（观测 demo-nginx）..."
  # 内嵌 CR，避免额外文件依赖；也可单独放到 deploy/demo-pobs.yaml
  kubectl apply -f - <<'EOF'
apiVersion: lightobs.io/v1alpha1
kind: PodObservation
metadata:
  name: watch-demo-nginx
  namespace: demo
spec:
  selector:
    matchLabels:
      app: demo-nginx
  capture:
    port: 80
    interface: "any"
    enableEBPF: true
  reportTo:
    serverAddress: "lightobs-server.lightobs.svc.cluster.local:8080"
EOF
  success "PodObservation CR 已创建"
}

verify_operator() {
  info "验证 Operator 是否成功处理 CR..."

  # 1. CR Status 应变为 Running
  local phase
  local retries=0
  local max_retries=15
  until phase=$(kubectl -n demo get pobs watch-demo-nginx \
    -o jsonpath='{.status.phase}' 2>/dev/null) && [[ "${phase}" == "Running" ]]; do
    retries=$((retries + 1))
    if [[ ${retries} -ge ${max_retries} ]]; then
      warn "超时：PodObservation phase 仍为 '${phase:-<空>}'，继续尝试查询..."
      break
    fi
    echo -n "."
    sleep 3
  done
  echo ""

  echo ""
  info "── PodObservation 状态 ──────────────────────────────"
  kubectl -n demo get pobs watch-demo-nginx -o wide || true

  info "── Operator 管理的 Agent Pod ────────────────────────"
  kubectl -n lightobs get pod \
    -l "app.kubernetes.io/managed-by=lightobs-operator" -o wide || true

  local agent_count
  agent_count=$(kubectl -n lightobs get pod \
    -l "app.kubernetes.io/managed-by=lightobs-operator" \
    --field-selector=status.phase=Running \
    -o name 2>/dev/null | wc -l)

  if [[ "${agent_count}" -gt 0 ]]; then
    success "Operator 已成功启动 ${agent_count} 个 Agent Pod！"
  else
    warn "暂未检测到 Running 状态的 Agent Pod，请稍后再查：\n  kubectl -n lightobs get pod -l app.kubernetes.io/managed-by=lightobs-operator"
  fi

  info "── Operator 日志（最新20行）────────────────────────"
  kubectl -n lightobs logs -l app=lightobs-operator --tail=20 || true

  info "── Server 日志（最新20行）──────────────────────────"
  kubectl -n lightobs logs -l app=lightobs-server --tail=20 || true
}

run_client_query() {
  mkdir -p "${ROOT}/bin"
  info "编译 lightobs-client..."
  go build -o "${ROOT}/bin/lightobs-client" "${ROOT}/cmd/client"

  NGINX_POD_IP="$(kubectl -n demo get pod -l app=demo-nginx \
    -o jsonpath='{.items[0].status.podIP}')"
  info "nginx Pod IP = ${NGINX_POD_IP}"

  kubectl -n lightobs port-forward svc/lightobs-server 8080:8080 \
    >/tmp/lightobs-portforward.log 2>&1 &
  PORT_FORWARD_PID=$!
  sleep 2

  "${ROOT}/bin/lightobs-client" \
    -ip "${NGINX_POD_IP}" -server http://127.0.0.1:8080 || true

  kill "${PORT_FORWARD_PID}" >/dev/null 2>&1 || true
  success "查询完成。nginx Pod IP=${NGINX_POD_IP}"
}

# ══════════════════════════════════════════════════════════
#  入口 dispatch
# ══════════════════════════════════════════════════════════
case "${MODE}" in
  full)
    ensure_kind
    build_images
    load_images
    ensure_tracefs
    deploy_lightobs_server
    apply_crd
    deploy_operator
    deploy_demo
    create_pobs_cr
    verify_operator
    run_client_query
    ;;

  build)
    ensure_kind
    build_images
    load_images
    ;;

  deploy)
    ensure_kind
    deploy_lightobs_server
    apply_crd
    deploy_operator
    deploy_demo
    create_pobs_cr
    verify_operator
    run_client_query
    ;;

  cr)
    ensure_kind
    create_pobs_cr
    verify_operator
    ;;

  verify)
    ensure_kind
    verify_operator
    run_client_query
    ;;

  restart)
    ensure_kind
    info "滚动重启 lightobs-server 和 lightobs-operator..."
    kubectl -n lightobs rollout restart deploy/lightobs-server
    kubectl -n lightobs rollout restart deploy/lightobs-operator
    kubectl -n lightobs rollout status deploy/lightobs-server --timeout=120s
    kubectl -n lightobs rollout status deploy/lightobs-operator --timeout=120s
    success "重启完成"
    ;;

  query)
    ensure_kind
    run_client_query
    ;;

  *)
    echo "用法: $0 {full|build|deploy|cr|verify|restart|query}"
    echo ""
    echo "  full     创建集群(如需) + 构建三镜像 + 导入 + 部署全套 + demo + 创建CR + 验证"
    echo "  build    仅构建并导入 server / agent / operator 三个镜像"
    echo "  deploy   集群已存在、镜像已导入 → 部署服务 + CRD + Operator + demo + CR + 验证"
    echo "  cr       仅创建/更新 PodObservation CR 并验证"
    echo "  verify   仅验证 Operator 状态 + 执行 client 查询"
    echo "  restart  滚动重启 server 和 operator"
    echo "  query    port-forward 后执行 client 查询"
    exit 1
    ;;
esac
