// Package v1alpha1 包含 LightObs Operator 的 CRD API 类型定义（v1alpha1 版本）。
package v1alpha1

import "k8s.io/apimachinery/pkg/runtime/schema"

// groupVersion 定义本 API 包的 Group 和 Version。
// 与 deploy/crds/podobservation.yaml 中的 `group: lightobs.io` 保持一致。
var groupVersion = schema.GroupVersion{
	Group:   "lightobs.io",
	Version: "v1alpha1",
}
