// Package v1alpha1 包含 LightObs Operator 的 CRD API 类型定义。
package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ──────────────────────────────────────────────────────────
// PodObservationSpec：用户填写的期望状态
// ──────────────────────────────────────────────────────────

// PodObservationSpec 定义了一个观测任务的目标和采集参数。
type PodObservationSpec struct {
	// Selector 用于从当前 Namespace 选取要观测的 Pod，语义与 K8s LabelSelector 一致。
	Selector metav1.LabelSelector `json:"selector"`

	// Capture 定义采集相关参数。
	Capture CaptureSpec `json:"capture,omitempty"`

	// ReportTo 定义数据上报的目标 Server，默认使用集群内 lightobs-server Service。
	ReportTo ReportToSpec `json:"reportTo,omitempty"`
}

// CaptureSpec 定义采集参数。
type CaptureSpec struct {
	// Port 是要监听的目标 HTTP 端口，默认 80。
	// +kubebuilder:default=80
	Port int `json:"port,omitempty"`

	// Interface 是 Agent 监听的网卡名，默认 "any"。
	// +kubebuilder:default="any"
	Interface string `json:"interface,omitempty"`

	// EnableEBPF 控制是否启用 eBPF PID 关联，默认 true。
	// +kubebuilder:default=true
	EnableEBPF bool `json:"enableEBPF,omitempty"`
}

// ReportToSpec 定义数据上报目标。
type ReportToSpec struct {
	// ServerAddress 是 Server 的访问地址（host 或 host:port）。
	// 默认："lightobs-server.lightobs.svc.cluster.local:8080"
	ServerAddress string `json:"serverAddress,omitempty"`
}

// ──────────────────────────────────────────────────────────
// PodObservationStatus：Operator 回写的实际状态
// ──────────────────────────────────────────────────────────

// ObservationPhase 描述观测任务的整体阶段。
type ObservationPhase string

const (
	// PhasePending 表示 Operator 尚未完成 Agent 部署。
	PhasePending ObservationPhase = "Pending"
	// PhaseRunning 表示所有目标节点上的 Agent 均已就绪。
	PhaseRunning ObservationPhase = "Running"
	// PhaseFailed 表示出现了无法自动恢复的错误，需要人工介入。
	PhaseFailed ObservationPhase = "Failed"
)

// PodObservationStatus 是 Operator 回写的运行时状态，用户只读。
type PodObservationStatus struct {
	// Phase 是观测任务的整体阶段。
	Phase ObservationPhase `json:"phase,omitempty"`

	// ObservedNodes 是当前已在其上部署 Agent 的节点数量。
	ObservedNodes int `json:"observedNodes,omitempty"`

	// TargetPodCount 是当前匹配 Selector 的 Running Pod 数量。
	TargetPodCount int `json:"targetPodCount,omitempty"`

	// Conditions 提供细粒度的状态信息，遵循 K8s 标准 Condition 约定。
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// 标准 Condition Type 常量
const (
	// ConditionAgentsReady 表示所有目标节点上的 Agent Pod 均已 Running。
	ConditionAgentsReady = "AgentsReady"
	// ConditionSelectorValid 表示 Selector 字段格式合法且能匹配到 Pod。
	ConditionSelectorValid = "SelectorValid"
)

// ──────────────────────────────────────────────────────────
// PodObservation：顶层 CRD 对象
// ──────────────────────────────────────────────────────────

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=pobs
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Nodes",type="integer",JSONPath=".status.observedNodes"
// +kubebuilder:printcolumn:name="Pods",type="integer",JSONPath=".status.targetPodCount"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// PodObservation 声明一个流量观测任务：
// 用户通过 spec.selector 指定想观测的 Pod，Operator 自动在相应节点部署 Agent 并采集 HTTP 流量。
type PodObservation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodObservationSpec   `json:"spec,omitempty"`
	Status PodObservationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PodObservationList 是 PodObservation 的列表类型，供 controller-runtime 自动使用。
type PodObservationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodObservation `json:"items"`
}
