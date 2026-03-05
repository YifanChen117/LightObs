package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GroupVersion 是本 API 组的 GVK 标识，与 CRD YAML 中的 group/version 保持一致。
var GroupVersion = groupVersion

// SchemeBuilder 用于向 runtime.Scheme 注册本包中的类型。
var SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

// AddToScheme 是 controller-runtime 的标准入口，在 main.go 中调用。
var AddToScheme = SchemeBuilder.AddToScheme

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&PodObservation{},
		&PodObservationList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
