package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lightobs/internal/agent/app"
)

func main() {
	var cfg app.Config
	flag.StringVar(&cfg.Interface, "interface", "", "要监听的网卡名（如 eth0 / vethXXX），必填")
	flag.StringVar(&cfg.ServerIP, "server-ip", "", "Server IP，必填")
	flag.IntVar(&cfg.ServerPort, "server-port", 0, "Server Port，必填")
	flag.DurationVar(&cfg.RequestTimeout, "request-timeout", 30*time.Second, "HTTP 匹配缓存超时时间")
	flag.Parse()

	if cfg.Interface == "" || cfg.ServerIP == "" || cfg.ServerPort == 0 {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg); err != nil {
		log.Printf("agent 退出：%v", err)
		os.Exit(1)
	}

	fmt.Println("agent 正常退出")
}
