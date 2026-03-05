package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	api "lightobs/internal/operator/api/v1alpha1"
	"lightobs/internal/operator/reconciler"
)

const (
	// finalizerName 保证 CR 删除时 Operator 能先清理 Agent Pod，防止孤儿 Pod。
	finalizerName = "lightobs.io/agent-cleanup"

	// requeueInterval 是正常情况下的主动探测周期，用于自愈节点重启导致的 Pod 丢失。
	requeueInterval = 30 * time.Second
)

// PodObservationReconciler 是本 Operator 的核心 Reconciler，实现 Reconcile 接口。
type PodObservationReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	AgentManager  *reconciler.AgentManager
	StatusUpdater *reconciler.StatusUpdater
}

// +kubebuilder:rbac:groups=lightobs.io,resources=podobservations,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=lightobs.io,resources=podobservations/status,verbs=update;patch
// +kubebuilder:rbac:groups=lightobs.io,resources=podobservations/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;create
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile 是控制循环的核心，每次 CR 发生变化或 RequeueAfter 到期时被调用。
func (r *PodObservationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// ── 1. 获取 CR ──────────────────────────────────────────────────────────────
	cr := &api.PodObservation{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if errors.IsNotFound(err) {
			// CR 已被删除且 Finalizer 已清理，无需处理
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("获取 CR 失败：%w", err)
	}

	// ── 2. 处理删除（Finalizer 清理流程）────────────────────────────────────────
	if !cr.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, cr)
	}

	// ── 3. 注入 Finalizer（首次创建时）──────────────────────────────────────────
	if !controllerutil.ContainsFinalizer(cr, finalizerName) {
		controllerutil.AddFinalizer(cr, finalizerName)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, fmt.Errorf("注入 Finalizer 失败：%w", err)
		}
		// Update 会触发新的 Reconcile，此处直接返回
		return ctrl.Result{}, nil
	}

	// ── 4. 确保 Agent Namespace 存在 ────────────────────────────────────────────
	if err := reconciler.EnsureAgentNamespace(ctx, r.Client); err != nil {
		return ctrl.Result{}, fmt.Errorf("确保 namespace 存在失败：%w", err)
	}

	// ── 5. 查询匹配的目标 Pod ──────────────────────────────────────────────────
	targets, err := r.resolveTargets(ctx, cr)
	if err != nil {
		_ = r.StatusUpdater.Update(ctx, cr, reconciler.StatusInput{Err: err})
		return ctrl.Result{}, fmt.Errorf("解析目标 Pod 失败：%w", err)
	}

	logger.Info("目标节点计算完成",
		"targetPods", func() int {
			n := 0
			for _, t := range targets {
				n += len(t.TargetIPs)
			}
			return n
		}(),
		"targetNodes", len(targets),
	)

	// ── 6. Reconcile Agent Pod（核心差集操作）──────────────────────────────────
	totalPodCount := 0
	for _, t := range targets {
		totalPodCount += len(t.TargetIPs)
	}

	runningAgents, reconcileErr := r.AgentManager.Reconcile(ctx, cr, targets)

	// ── 7. 回写 Status ─────────────────────────────────────────────────────────
	statusInput := reconciler.StatusInput{
		TargetPodCount: totalPodCount,
		DesiredNodes:   len(targets),
		RunningAgents:  runningAgents,
		Err:            reconcileErr,
	}
	if err := r.StatusUpdater.Update(ctx, cr, statusInput); err != nil {
		logger.Error(err, "回写 Status 失败")
		// Status 回写失败不阻断主流程，记录日志后继续 Requeue
	}

	if reconcileErr != nil {
		return ctrl.Result{}, reconcileErr
	}

	// ── 8. 定期 Requeue（自愈节点重启等异常）──────────────────────────────────
	return ctrl.Result{RequeueAfter: requeueInterval}, nil
}

// handleDeletion 执行 CR 删除时的清理流程：先清理 Agent Pod，再移除 Finalizer。
func (r *PodObservationReconciler) handleDeletion(ctx context.Context, cr *api.PodObservation) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(cr, finalizerName) {
		return ctrl.Result{}, nil // Finalizer 已移除，K8s 可以真正删除 CR
	}

	logger.Info("CR 正在删除，清理 Agent Pod", "cr", cr.Name)
	if err := r.AgentManager.DeleteAll(ctx, cr); err != nil {
		return ctrl.Result{}, fmt.Errorf("清理 Agent Pod 失败：%w", err)
	}

	// 移除 Finalizer，让 K8s 真正删除 CR
	controllerutil.RemoveFinalizer(cr, finalizerName)
	if err := r.Update(ctx, cr); err != nil {
		return ctrl.Result{}, fmt.Errorf("移除 Finalizer 失败：%w", err)
	}
	return ctrl.Result{}, nil
}

// resolveTargets 根据 CR 的 Selector 查询目标 Pod，
// 并按节点聚合成 []NodeTarget（每个节点一个，包含该节点上所有目标 Pod IP）。
func (r *PodObservationReconciler) resolveTargets(ctx context.Context, cr *api.PodObservation) ([]reconciler.NodeTarget, error) {
	selector, err := metav1.LabelSelectorAsSelector(&cr.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("解析 LabelSelector 失败：%w", err)
	}
	if selector == labels.Nothing() {
		// 防止空 selector 匹配所有 Pod
		return nil, fmt.Errorf("Selector 不能为空，请指定至少一个 matchLabels 或 matchExpressions")
	}

	var podList corev1.PodList
	if err := r.List(ctx, &podList,
		client.InNamespace(cr.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return nil, fmt.Errorf("查询目标 Pod 失败：%w", err)
	}

	// 按节点聚合 Pod IP（只处理 Running 且有 PodIP 的 Pod）
	nodeIPMap := make(map[string][]string)
	for _, pod := range podList.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		if pod.Status.PodIP == "" {
			continue
		}
		if pod.Spec.NodeName == "" {
			continue
		}
		nodeIPMap[pod.Spec.NodeName] = append(nodeIPMap[pod.Spec.NodeName], pod.Status.PodIP)
	}

	targets := make([]reconciler.NodeTarget, 0, len(nodeIPMap))
	for node, ips := range nodeIPMap {
		targets = append(targets, reconciler.NodeTarget{
			NodeName:  node,
			TargetIPs: ips,
		})
	}
	return targets, nil
}

// SetupWithManager 注册 Controller 到 Manager，同时设置 Watch 规则。
func (r *PodObservationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// 主 Watch：PodObservation CR 变化
		For(&api.PodObservation{}).
		// 辅助 Watch 1：业务 Pod 变化（扩缩容）-> 触发相关 CR 的 Reconcile
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.podToObservation),
		).
		// 辅助 Watch 2：Agent Pod 变化（意外删除/OOMKill）-> 触发 Reconcile 自愈
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.agentPodToObservation),
		).
		Complete(r)
}

// podToObservation 将业务 Pod 事件映射到需要 Reconcile 的 PodObservation CR 列表。
// 遍历同 Namespace 下的所有 CR，找到 selector 匹配该 Pod 的 CR。
func (r *PodObservationReconciler) podToObservation(ctx context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil
	}
	// 跳过 Operator 自己管理的 Agent Pod，避免死循环
	if pod.Namespace == "lightobs" && pod.Labels["app.kubernetes.io/managed-by"] == "lightobs-operator" {
		return nil
	}

	var crList api.PodObservationList
	if err := r.List(ctx, &crList, client.InNamespace(pod.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, cr := range crList.Items {
		selector, err := metav1.LabelSelectorAsSelector(&cr.Spec.Selector)
		if err != nil {
			continue
		}
		if selector.Matches(labels.Set(pod.Labels)) {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKey{
					Namespace: cr.Namespace,
					Name:      cr.Name,
				},
			})
		}
	}
	return requests
}

// agentPodToObservation 将 Agent Pod 事件映射到对应的 PodObservation CR。
// 通过 Agent Pod 的 Label 找到 ownerLabelKey 字段，解析出 CR 的 namespace/name。
func (r *PodObservationReconciler) agentPodToObservation(_ context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil
	}
	// 只处理 Operator 管理的 Agent Pod
	if pod.Labels["app.kubernetes.io/managed-by"] != "lightobs-operator" {
		return nil
	}
	ownerVal := pod.Labels["lightobs.io/observation"]
	if ownerVal == "" {
		return nil
	}
	// ownerVal 格式："{namespace}.{name}"
	// 注意：namespace 本身不含点号（K8s 规范），所以第一个点是分隔符
	idx := indexOf(ownerVal, '.')
	if idx < 0 {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: client.ObjectKey{
			Namespace: ownerVal[:idx],
			Name:      ownerVal[idx+1:],
		},
	}}
}

func indexOf(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
