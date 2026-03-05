package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ──────────────────────────────────────────────────────────
// DeepCopy 实现：controller-runtime 要求所有注册到 Scheme 的
// 类型实现 runtime.Object（即 DeepCopyObject）。
// 在使用 controller-gen 之前，此处手动实现。
// ──────────────────────────────────────────────────────────

// DeepCopyObject 实现 runtime.Object 接口。
func (in *PodObservation) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}

// DeepCopy 返回 PodObservation 的深拷贝。
func (in *PodObservation) DeepCopy() *PodObservation {
	if in == nil {
		return nil
	}
	out := new(PodObservation)
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = deepCopySpec(in.Spec)
	out.Status = deepCopyStatus(in.Status)
	return out
}

// DeepCopyObject 实现 runtime.Object 接口。
func (in *PodObservationList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	return in.DeepCopy()
}

// DeepCopy 返回 PodObservationList 的深拷贝。
func (in *PodObservationList) DeepCopy() *PodObservationList {
	if in == nil {
		return nil
	}
	out := new(PodObservationList)
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]PodObservation, len(in.Items))
		for i := range in.Items {
			out.Items[i] = *in.Items[i].DeepCopy()
		}
	}
	return out
}

// deepCopySpec 深拷贝 PodObservationSpec。
// LabelSelector 内部含有指针字段（MatchExpressions），需要通过 DeepCopyInto 处理。
func deepCopySpec(in PodObservationSpec) PodObservationSpec {
	out := PodObservationSpec{
		Capture:  in.Capture,  // 纯值类型，直接赋值
		ReportTo: in.ReportTo, // 纯值类型，直接赋值
	}
	in.Selector.DeepCopyInto(&out.Selector)
	return out
}

// deepCopyStatus 深拷贝 PodObservationStatus。
func deepCopyStatus(in PodObservationStatus) PodObservationStatus {
	out := PodObservationStatus{
		Phase:          in.Phase,
		ObservedNodes:  in.ObservedNodes,
		TargetPodCount: in.TargetPodCount,
	}
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i, c := range in.Conditions {
			// metav1.Condition 内部含有 Time 指针，使用 DeepCopy
			out.Conditions[i] = *c.DeepCopy()
		}
	}
	return out
}
