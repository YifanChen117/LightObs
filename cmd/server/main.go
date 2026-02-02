package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lightobs/internal/server/app"
)

func main() {
	var cfg app.Config
	flag.StringVar(&cfg.ListenAddr, "listen", ":8080", "监听地址")
	flag.StringVar(&cfg.DBDriver, "db-driver", "duckdb", "数据库类型：duckdb 或 sqlite")
	flag.StringVar(&cfg.DBPath, "db", "", "数据库文件路径")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := app.NewServer(cfg)
	if err != nil {
		log.Fatalf("server 初始化失败：%v", err)
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("server 监听：%s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server 运行失败：%v", err)
	}
}
