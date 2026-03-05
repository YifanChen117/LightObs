package reconciler

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	api "lightobs/internal/operator/api/v1alpha1"
)

// StatusUpdater 封装 CR Status 的构建与回写逻辑。
type StatusUpdater struct {
	client client.Client
}

// NewStatusUpdater 构造 StatusUpdater。
func NewStatusUpdater(c client.Client) *StatusUpdater {
	return &StatusUpdater{client: c}
}

// StatusInput 是计算 Status 所需的输入数据。
type StatusInput struct {
	TargetPodCount int
	DesiredNodes   int
	RunningAgents  int
	Err            error // 如果 Reconcile 过程中出现错误，Err != nil
}

// Update 根据输入数据计算并回写 CR 的 Status 字段。
// 使用 Status().Update() 而非整体 Update()，避免覆盖 Spec。
func (u *StatusUpdater) Update(ctx context.Context, cr *api.PodObservation, input StatusInput) error {
	now := metav1.Now()

	// 计算 Phase 和 Condition
	phase, cond := computePhaseAndCondition(input, now)

	// 更新 Status 字段（在 cr 对象上直接修改，然后 Patch）
	patch := client.MergeFrom(cr.DeepCopy())

	cr.Status.Phase = phase
	cr.Status.TargetPodCount = input.TargetPodCount
	cr.Status.ObservedNodes = input.RunningAgents

	// 合并 Condition（避免覆盖其他 Controller 写入的 Condition）
	cr.Status.Conditions = mergeCondition(cr.Status.Conditions, cond)

	if err := u.client.Status().Patch(ctx, cr, patch); err != nil {
		return fmt.Errorf("回写 CR Status 失败：%w", err)
	}
	return nil
}

// computePhaseAndCondition 根据运行时数据推导 Phase 和 AgentsReady Condition。
func computePhaseAndCondition(input StatusInput, now metav1.Time) (api.ObservationPhase, metav1.Condition) {
	if input.Err != nil {
		return api.PhaseFailed, metav1.Condition{
			Type:               api.ConditionAgentsReady,
			Status:             metav1.ConditionFalse,
			Reason:             "ReconcileError",
			Message:            input.Err.Error(),
			LastTransitionTime: now,
		}
	}

	if input.TargetPodCount == 0 {
		return api.PhasePending, metav1.Condition{
			Type:               api.ConditionAgentsReady,
			Status:             metav1.ConditionFalse,
			Reason:             "NoTargetPods",
			Message:            "Selector 未匹配到任何 Running Pod，等待目标 Pod 就绪",
			LastTransitionTime: now,
		}
	}

	if input.RunningAgents < input.DesiredNodes {
		return api.PhasePending, metav1.Condition{
			Type:   api.ConditionAgentsReady,
			Status: metav1.ConditionFalse,
			Reason: "AgentsDeploying",
			Message: fmt.Sprintf("Agent 部署中：%d/%d 节点已就绪",
				input.RunningAgents, input.DesiredNodes),
			LastTransitionTime: now,
		}
	}

	return api.PhaseRunning, metav1.Condition{
		Type:               api.ConditionAgentsReady,
		Status:             metav1.ConditionTrue,
		Reason:             "AllAgentsRunning",
		Message:            fmt.Sprintf("所有 %d 个目标节点的 Agent 均已就绪", input.DesiredNodes),
		LastTransitionTime: now,
	}
}

// mergeCondition 将新 Condition 合并进 Condition 列表。
// 若已存在同 Type 的 Condition 且 Status 不变，保留原 LastTransitionTime（符合 K8s 约定）。
func mergeCondition(existing []metav1.Condition, newCond metav1.Condition) []metav1.Condition {
	for i, c := range existing {
		if c.Type == newCond.Type {
			if c.Status == newCond.Status {
				// Status 未变，保留原 LastTransitionTime
				newCond.LastTransitionTime = c.LastTransitionTime
			}
			existing[i] = newCond
			return existing
		}
	}
	// 不存在同 Type，追加
	return append(existing, newCond)
}
