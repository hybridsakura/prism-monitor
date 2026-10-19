package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/hybridsakura/prism-monitor/internal/api"
	"github.com/hybridsakura/prism-monitor/internal/collect"
	"github.com/hybridsakura/prism-monitor/internal/store"
	webhmd "github.com/hybridsakura/prism-monitor/web-hmd"
)

var (
	version = "v0.1.0-hmd"
)

type config struct {
	port     int
	host     string
	dbPath   string
	procRoot string
	interval time.Duration
}

func parseConfig() config {
	cfg := config{
		port:     getEnvInt("PRISM_HMD_PORT", 21121), // 默认 21121 端口，避开 21120
		host:     getEnv("PRISM_HOST", "0.0.0.0"),
		dbPath:   getEnv("PRISM_DB", "prism-monitor.db"),
		procRoot: getEnv("PRISM_PROC_ROOT", "/"),
		interval: 2 * time.Second,
	}

	flag.IntVar(&cfg.port, "port", cfg.port, "HTTP 监听端口（默认 21121，环境变量 PRISM_HMD_PORT）")
	flag.StringVar(&cfg.host, "host", cfg.host, "HTTP 监听地址（默认 0.0.0.0）")
	flag.StringVar(&cfg.dbPath, "db", cfg.dbPath, "SQLite 数据文件路径（默认 prism-monitor.db）")
	flag.StringVar(&cfg.procRoot, "proc-root", cfg.procRoot, "/proc 与 /sys 挂载根目录（默认 /）")
	flag.Parse()

	return cfg
}

func main() {
	cfg := parseConfig()
	log.Printf("[prism-hmd] 浅色高密度 HMD 分支面板启动，端口 :%d，数据库 %s，proc 根 %s",
		cfg.port, cfg.dbPath, cfg.procRoot)

	st, err := store.Open(cfg.dbPath)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}
	defer st.Close()

	coll := collect.New(cfg.procRoot)
	// 挂载 webhmd.FS 浅色 Canvas 界面
	srv := api.New(coll, st, webhmd.FS)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 采集循环
	go func() {
		ticker := time.NewTicker(cfg.interval)
		defer ticker.Stop()
		lastStore := time.Now()

		if snap, err := coll.Collect(); err == nil {
			srv.Publish(snap)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				snap, err := coll.Collect()
				if err != nil {
					continue
				}
				srv.Publish(snap)
				if now.Sub(lastStore) >= 10*time.Second {
					_ = st.InsertBatch(snap.Ts, snap.Metrics())
					lastStore = now
				}
			}
		}
	}()

	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.host, cfg.port),
		Handler:      srv.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0,
	}

	go func() {
		log.Printf("[prism-hmd] HMD 控制台服务已就绪: http://%s:%d", cfg.host, cfg.port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务异常: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	cancel()
	log.Printf("[prism-hmd] 退出完毕")
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getEnvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
