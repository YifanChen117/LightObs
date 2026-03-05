package app

import "time"

// Config 是 Agent 的运行时配置，字段同时兼容老 CLI 启动和 Operator 注入的环境变量。
type Config struct {
	Interface string // 网卡名，"any" 表示监听所有网卡

	ServerIP   string // Server 地址
	ServerPort int    // Server 端口

	RequestTimeout  time.Duration // HTTP 请求等待超时
	HTTPPostTimeout time.Duration // 上报 HTTP POST 超时

	EnableEBPF bool // 是否启用 eBPF PID 关联

	// Operator 注入字段：
	// WatchPort 是要监听的目标 HTTP 端口（Operator 从 CRD spec.capture.port 注入）。
	// 0 表示使用默认值（向下兼容：80 端口）。
	WatchPort int

	// TargetIPs 是本节点上目标 Pod 的 IP 列表（Operator 从 Pod 信息注入）。
	// 为空时表示"无过滤"，与原始 DaemonSet 模式兼容。
	TargetIPs []string
}
