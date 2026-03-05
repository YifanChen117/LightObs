#!/usr/bin/env bash
#
# scripts/demo_operator_magic.sh ── 演示 Operator 的核心优势
#

set -euo pipefail

# ── 颜色输出 ──────────────────────────────────────────────
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
RESET='\033[0m'

info()    { echo -e "\n${CYAN}➜ $*${RESET}"; }
step()    { echo -e "${MAGENTA}▶ $1${RESET}"; read -r -p "  (按 Enter 键继续...)" ; echo ""; }
success() { echo -e "${GREEN}✔ $*${RESET}"; }

clear
echo -e "${YELLOW}======================================================${RESET}"
echo -e "${YELLOW}       Operator Demo演示      ${RESET}"
echo -e "${YELLOW}======================================================${RESET}"
echo ""

# 确保资源就绪
if ! kubectl get pobs watch-demo-nginx -n demo >/dev/null 2>&1; then
    echo "错误：找不到 watch-demo-nginx，请先运行 start_with_operator.sh 部署基础环境"
    exit 1
fi

# ==============================================================================
# 场景 1：动态自动发现与扩缩容感知 (Auto-Discovery)
# ==============================================================================
info "[场景 1] 动态感知与自动扩缩容体验"
echo "  当前 nginx 的副本数为 1，且 Operator 已为其创建了观测配置。"

step "查看当前的 PodObservation 状态和 nginx Pod 数量"
kubectl get deploy demo-nginx -n demo
echo ""
kubectl get pobs watch-demo-nginx -n demo

step "将应用 demo-nginx 扩容到 3 个副本"
kubectl scale deploy demo-nginx --replicas=3 -n demo

echo -e "  ${YELLOW}Operator 会自动监听到 K8s 中新增的 2 个 Pod，并将其 IP 纳入对应的 Agent 配置中。${RESET}"
for i in {1..7}; do
    echo -n "."
    sleep 1
done
echo ""

step "再次查看 PodObservation 状态 (注意 PODS 的数量是否变为了 3)"
kubectl get pobs watch-demo-nginx -n demo
echo ""
kubectl get pod -l app=demo-nginx -n demo -o wide

# ==============================================================================
# 场景 2：故障自愈 (Self-Healing)
# ==============================================================================
info "[场景 2] Agent 故障自愈体验"
echo "  假设由于节点 OOM 或人为误操作，底层的 eBPF Agent 崩溃被删除了。"

step "查看当前 Operator 管理的 Agent Pods"
kubectl get pod -n lightobs | grep agent || true

AGENT_POD=$(kubectl get pod -n lightobs | grep 'lightobs-agent-watch-demo-nginx' | grep Running | head -n 1 | awk '{print $1}' || true)

if [ -z "$AGENT_POD" ]; then
    echo "未找到对应的 Agent Pod，跳过自愈演示"
else
    step "人为【粗暴删除】 Agent Pod: $AGENT_POD"
    kubectl delete pod "$AGENT_POD" -n lightobs
    
    echo -e "  ${YELLOW}Operator 的 Reconcile 循环会在几秒内发现实际状态（被删掉）与期望状态不符，并瞬间重建它！${RESET}"
    for i in {1..5}; do
        echo -n "."
        sleep 1
    done
    echo ""

    step "再次查看 Agent Pods (你会发现一个新的 Pod 被瞬间拉起，且 AGE 只有几秒)"
    kubectl get pod -n lightobs | grep lightobs-agent-watch-demo-nginx || true
fi

# ==============================================================================
# 场景 3：声明式动态配置修改 (Dynamic Config Update)
# ==============================================================================
info "[场景 3] 基于 CRD 的配置热更新"
echo "  传统方式需要 SSH 登录机器改 Agent 配置文件，现在只需一行 kubectl patch 即可生效。"

step "将 watch-demo-nginx 的抓取端口从 80 动态修改为 8080"
kubectl patch pobs watch-demo-nginx -n demo --type='merge' -p '{"spec":{"capture":{"port":8080}}}'

echo -e "  ${YELLOW}此时 Operator 已经被唤醒，更新了底层资源的配置并可能重建 Agent 触发重载。${RESET}"
sleep 3
echo ""
step "查看 CRD 是否成功更新"
kubectl get pobs watch-demo-nginx -n demo -o yaml | grep -A 3 capture || true

step "为了后续实验正常，我们将端口还原为 80"
kubectl patch pobs watch-demo-nginx -n demo --type='merge' -p '{"spec":{"capture":{"port":80}}}'
kubectl get pobs watch-demo-nginx -n demo -o yaml | grep -A 3 capture || true
sleep 2

echo ""
echo -e "${GREEN}======================================================${RESET}"
echo -e "${GREEN}  演示结束${RESET}"
echo -e "${GREEN}======================================================${RESET}"
