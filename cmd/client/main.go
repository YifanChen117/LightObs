package main

import (
	"flag"
	"log"
	"os"

	"lightobs/internal/client/app"
)

func main() {
	var cfg app.Config
	flag.StringVar(&cfg.IP, "ip", "", "目标 IP，必填")
	flag.StringVar(&cfg.Server, "server", "http://127.0.0.1:8080", "Server 地址")
	flag.Parse()

	if cfg.IP == "" {
		flag.Usage()
		os.Exit(2)
	}

	if err := app.Run(cfg); err != nil {
		log.Printf("client 失败：%v", err)
		os.Exit(1)
	}
}

