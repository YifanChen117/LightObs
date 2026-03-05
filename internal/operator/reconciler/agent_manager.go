package reconciler

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	api "lightobs/internal/operator/api/v1alpha1"
)

const (
	// agentLabelKey / agentLabelValue 标记所有由 Operator 管理的 Agent Pod。
	agentLabelKey   = "app.kubernetes.io/managed-by"
	agentLabelValue = "lightobs-operator"

	// ownerLabelKey 存储该 Agent Pod 对应的 PodObservation CR 的 namespace/name。
	ownerLabelKey = "lightobs.io/observation"

	// nodeLabelKey 存储该 Agent Pod 被调度到的节点名。
	nodeLabelKey = "lightobs.io/node"

	// agentNamespace 是所有 Agent Pod 统一运行的命名空间。
	agentNamespace = "lightobs"

	agentImageEnvKey  = "LIGHTOBS_AGENT_IMAGE"
	agentImageDefault = "lightobs-agent:dev"

	// defaultServerAddress 是 Server 的默认集群内访问地址。
	defaultServerAddress = "lightobs-server.lightobs.svc.cluster.local:8080"
)

// NodeTarget 描述一个节点上需要运行的 Agent 配置。
type NodeTarget struct {
	NodeName  string
	TargetIPs []string // 该节点上目标 Pod 的 IP 列表
}

// AgentManager 封装 Agent Pod 的创建、删除、状态查询逻辑。
type AgentManager struct {
	client client.Client
}

// NewAgentManager 构造 AgentManager。
func NewAgentManager(c client.Client) *AgentManager {
	return &AgentManager{client: c}
}

// Reconcile 根据期望节点列表（desired）与现有 Agent Pod（actual）做差集操作：
//   - 新增 = desired - actual（在新节点上创建 Agent Pod）
//   - 删除 = actual - desired（删除不再需要的 Agent Pod）
//   - 更新 = IP 列表变化的节点（删旧建新）
//
// 返回实际成功部署的节点数量。
func (m *AgentManager) Reconcile(ctx context.Context, cr *api.PodObservation, desired []NodeTarget) (int, error) {
	logger := log.FromContext(ctx)

	// 1. 查询当前由本 CR 管理的 Agent Pod
	existing, err := m.listAgentPods(ctx, cr)
	if err != nil {
		return 0, fmt.Errorf("列出 Agent Pod 失败：%w", err)
	}

	// 以 nodeName 为 key 建索引
	existingByNode := make(map[string]*corev1.Pod, len(existing))
	for i := range existing {
		existingByNode[existing[i].Spec.NodeName] = &existing[i]
	}
	desiredByNode := make(map[string]NodeTarget, len(desired))
	for _, t := range desired {
		desiredByNode[t.NodeName] = t
	}

	// 2. 删除不再需要的节点上的 Agent Pod
	for nodeName, pod := range existingByNode {
		if _, ok := desiredByNode[nodeName]; !ok {
			logger.Info("删除多余 Agent Pod", "node", nodeName, "pod", pod.Name)
			if err := m.client.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
				return 0, fmt.Errorf("删除 Agent Pod %s 失败：%w", pod.Name, err)
			}
		}
	}

	// 3. 新建或更新需要的节点上的 Agent Pod
	readyCount := 0
	for _, target := range desired {
		existing, exists := existingByNode[target.NodeName]

		if exists {
			// 检查 IP 列表是否变化（通过环境变量对比）
			if !ipListChanged(existing, target.TargetIPs) {
				if isPodRunning(existing) {
					readyCount++
				}
				continue // 无变化，跳过
			}
			// IP 列表变化，删除旧 Pod，下方重建
			logger.Info("目标 IP 变化，重建 Agent Pod", "node", target.NodeName)
			if err := m.client.Delete(ctx, existing); err != nil && !errors.IsNotFound(err) {
				return 0, fmt.Errorf("删除旧 Agent Pod 失败：%w", err)
			}
		}

		// 创建新 Agent Pod
		pod := m.buildAgentPod(cr, target)
		logger.Info("创建 Agent Pod", "node", target.NodeName, "pod", pod.Name)
		if err := m.client.Create(ctx, pod); err != nil {
			if errors.IsAlreadyExists(err) {
				// 并发创建竞争，忽略即可，下次 Reconcile 会看到
				logger.Info("Agent Pod 已存在（竞争创建），忽略", "pod", pod.Name)
			} else {
				return 0, fmt.Errorf("创建 Agent Pod %s 失败：%w", pod.Name, err)
			}
		}
		// 新建的 Pod 尚未 Running，不计入 readyCount，等下次 Reconcile 检查
	}

	return readyCount, nil
}

// DeleteAll 删除本 CR 管理的所有 Agent Pod（在 Finalizer 清理流程中调用）。
func (m *AgentManager) DeleteAll(ctx context.Context, cr *api.PodObservation) error {
	logger := log.FromContext(ctx)
	existing, err := m.listAgentPods(ctx, cr)
	if err != nil {
		return fmt.Errorf("列出 Agent Pod 失败（清理阶段）：%w", err)
	}
	for i := range existing {
		pod := &existing[i]
		logger.Info("清理 Agent Pod", "pod", pod.Name, "node", pod.Spec.NodeName)
		if err := m.client.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("删除 Agent Pod %s 失败：%w", pod.Name, err)
		}
	}
	return nil
}

// listAgentPods 查询属于指定 CR 的所有 Agent Pod。
func (m *AgentManager) listAgentPods(ctx context.Context, cr *api.PodObservation) ([]corev1.Pod, error) {
	var list corev1.PodList
	ownerVal := ownerLabelValue(cr)
	if err := m.client.List(ctx, &list,
		client.InNamespace(agentNamespace),
		client.MatchingLabels{
			agentLabelKey: agentLabelValue,
			ownerLabelKey: ownerVal,
		},
	); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// buildAgentPod 根据 CR 和节点目标构造 Agent Pod 对象。
func (m *AgentManager) buildAgentPod(cr *api.PodObservation, target NodeTarget) *corev1.Pod {
	serverAddr := cr.Spec.ReportTo.ServerAddress
	if serverAddr == "" {
		serverAddr = defaultServerAddress
	}
	// 拆分 host:port
	serverIP, serverPort := splitAddr(serverAddr)

	port := cr.Spec.Capture.Port
	if port == 0 {
		port = 80
	}
	iface := cr.Spec.Capture.Interface
	if iface == "" {
		iface = "any"
	}
	enableEBPF := "false"
	if cr.Spec.Capture.EnableEBPF {
		enableEBPF = "true"
	}

	podName := agentPodName(cr, target.NodeName)
	privileged := true

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: agentNamespace,
			Labels: map[string]string{
				agentLabelKey: agentLabelValue,
				ownerLabelKey: ownerLabelValue(cr),
				nodeLabelKey:  target.NodeName,
			},
			Annotations: map[string]string{
				// 记录目标 IP 列表，用于下次 Diff 判断是否需要重建
				"lightobs.io/target-ips": strings.Join(target.TargetIPs, ","),
			},
		},
		Spec: corev1.PodSpec{
			// 强制调度到目标节点
			NodeName:    target.NodeName,
			HostNetwork: true,
			HostPID:     true,
			DNSPolicy:   corev1.DNSClusterFirstWithHostNet,
			// 重启策略：Always，节点重启后 kubelet 自动 restart 容器
			RestartPolicy: corev1.RestartPolicyAlways,
			// 不参与服务发现，是纯采集进程
			AutomountServiceAccountToken: boolPtr(false),
			Containers: []corev1.Container{
				{
					Name:  "agent",
					Image: agentImageValue(),
					// 通过 sh -c 先挂载必要的 fs，再启动 agent
					Command: []string{"/bin/sh", "-c"},
					Args: []string{
						`mount | grep -q '/sys/kernel/tracing' || mount -t tracefs tracefs /sys/kernel/tracing;` +
							`mount | grep -q '/sys/kernel/debug' || mount -t debugfs debugfs /sys/kernel/debug;` +
							`exec /lightobs-agent`,
					},
					Env: []corev1.EnvVar{
						{Name: "LIGHTOBS_SERVER_IP", Value: serverIP},
						{Name: "LIGHTOBS_SERVER_PORT", Value: serverPort},
						{Name: "LIGHTOBS_WATCH_PORT", Value: fmt.Sprintf("%d", port)},
						{Name: "LIGHTOBS_ENABLE_EBPF", Value: enableEBPF},
						{Name: "LIGHTOBS_TARGET_IPS", Value: strings.Join(target.TargetIPs, ",")},
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: &privileged,
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{"NET_RAW", "NET_ADMIN"},
						},
					},
				},
			},
		},
	}
}

// ──────────────── 辅助函数 ────────────────

func agentImageValue() string {
	if v := strings.TrimSpace(os.Getenv(agentImageEnvKey)); v != "" {
		return v
	}
	return agentImageDefault
}

func ownerLabelValue(cr *api.PodObservation) string {
	// 格式：namespace.name，只包含合法 label 字符（点号在 label value 中合法）
	return fmt.Sprintf("%s.%s", cr.Namespace, cr.Name)
}

// agentPodName 生成 Agent Pod 名称，格式：lightobs-agent-{cr-name}-{node}
// 节点名中的特殊字符替换为连字符，确保符合 DNS 子域名规范。
func agentPodName(cr *api.PodObservation, nodeName string) string {
	safeNode := strings.ReplaceAll(nodeName, ".", "-")
	safeNode = strings.ReplaceAll(safeNode, "_", "-")
	name := fmt.Sprintf("lightobs-agent-%s-%s", cr.Name, safeNode)
	// K8s Pod 名最长 253 字符，截断保证安全
	if len(name) > 253 {
		name = name[:253]
	}
	return name
}

// ipListChanged 比较 Pod Annotation 中记录的 IP 列表与期望 IP 列表是否一致。
func ipListChanged(pod *corev1.Pod, desiredIPs []string) bool {
	recorded := pod.Annotations["lightobs.io/target-ips"]
	desired := strings.Join(desiredIPs, ",")
	return recorded != desired
}

// isPodRunning 返回 Pod 是否处于 Running 阶段。
func isPodRunning(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodRunning
}

// splitAddr 将 "host:port" 拆成两个字符串；
// 若无端口部分则默认 "8080"。
func splitAddr(addr string) (host, port string) {
	if idx := strings.LastIndex(addr, ":"); idx >= 0 {
		return addr[:idx], addr[idx+1:]
	}
	return addr, "8080"
}

func boolPtr(b bool) *bool { return &b }

// GetAgentPodsStatus 返回本 CR 下 Agent Pod 的 (total, running) 统计。
func (m *AgentManager) GetAgentPodsStatus(ctx context.Context, cr *api.PodObservation) (total, running int, err error) {
	pods, err := m.listAgentPods(ctx, cr)
	if err != nil {
		return 0, 0, err
	}
	for i := range pods {
		total++
		if isPodRunning(&pods[i]) {
			running++
		}
	}
	return
}

// EnsureAgentNamespace 确保 agentNamespace 存在，不存在时创建。
func EnsureAgentNamespace(ctx context.Context, c client.Client) error {
	ns := &corev1.Namespace{}
	err := c.Get(ctx, types.NamespacedName{Name: agentNamespace}, ns)
	if err == nil {
		return nil // 已存在
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("查询 namespace %s 失败：%w", agentNamespace, err)
	}
	// 创建
	ns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: agentNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "lightobs-operator",
			},
		},
	}
	return c.Create(ctx, ns)
}
