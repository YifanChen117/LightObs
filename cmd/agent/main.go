package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"lightobs/internal/agent/app"
)

func main() {
	var cfg app.Config

	// --- 基础 CLI 参数（与旧 DaemonSet 模式兼容） ---
	flag.StringVar(&cfg.Interface, "interface", "any", "监听的网卡名，'any' 表示所有网卡")
	flag.StringVar(&cfg.ServerIP, "server-ip", "", "Server IP 或域名")
	flag.IntVar(&cfg.ServerPort, "server-port", 8080, "Server 端口")
	flag.DurationVar(&cfg.RequestTimeout, "request-timeout", 30*time.Second, "HTTP 请求等待超时")
	flag.DurationVar(&cfg.HTTPPostTimeout, "post-timeout", 5*time.Second, "上报 HTTP POST 超时")
	flag.BoolVar(&cfg.EnableEBPF, "enable-ebpf", false, "是否启用 eBPF PID 关联")
	flag.IntVar(&cfg.WatchPort, "watch-port", 0, "要监听的 HTTP 端口（0 使用默认值 80）")

	// target-ips: CLI 参数形式（逗号分隔），Operator 注入时优先使用环境变量。
	var targetIPsFlag string
	flag.StringVar(&targetIPsFlag, "target-ips", "", "目标 Pod IP 白名单（逗号分隔），空表示不过滤")

	flag.Parse()

	// --- 环境变量覆盖（Operator 通过 env 注入配置） ---
	// 优先级：env > CLI flag，便于 Operator 无感知地覆盖默认值。
	if v := os.Getenv("LIGHTOBS_SERVER_IP"); v != "" {
		cfg.ServerIP = v
	}
	if v := os.Getenv("LIGHTOBS_SERVER_PORT"); v != "" {
		if port := parseIntEnv(v, "LIGHTOBS_SERVER_PORT"); port > 0 {
			cfg.ServerPort = port
		}
	}
	if v := os.Getenv("LIGHTOBS_WATCH_PORT"); v != "" {
		if port := parseIntEnv(v, "LIGHTOBS_WATCH_PORT"); port > 0 {
			cfg.WatchPort = port
		}
	}
	if v := os.Getenv("LIGHTOBS_ENABLE_EBPF"); v == "true" || v == "1" {
		cfg.EnableEBPF = true
	}
	if v := os.Getenv("LIGHTOBS_TARGET_IPS"); v != "" {
		// 环境变量优先级高于 CLI flag
		targetIPsFlag = v
	}

	// 解析目标 IP 列表
	if targetIPsFlag != "" {
		for _, ip := range strings.Split(targetIPsFlag, ",") {
			ip = strings.TrimSpace(ip)
			if ip != "" {
				cfg.TargetIPs = append(cfg.TargetIPs, ip)
			}
		}
	}

	if cfg.ServerIP == "" {
		log.Fatal("必须指定 server-ip（或设置环境变量 LIGHTOBS_SERVER_IP）")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg); err != nil {
		log.Fatalf("agent 运行失败：%v", err)
	}
}

func parseIntEnv(s, name string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			log.Printf("环境变量 %s=%q 不是有效整数，已忽略", name, s)
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
