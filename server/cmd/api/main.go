// Command api 拾光票局后端入口。
// 启动流程：加载配置 → 连 MySQL（重试 5 次 × 2s，失败退出）→ 自动执行内嵌迁移 → 起 HTTP。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"piaoju/internal/platform/config"
	"piaoju/internal/platform/db"
	"piaoju/internal/platform/token"
)

const (
	dbConnectAttempts = 5
	dbConnectInterval = 2 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := db.Open(ctx, cfg.DBDSN, dbConnectAttempts, dbConnectInterval, log)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := db.Migrate(cfg.DBDSN, log); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           newRouter(conn, token.NewManager(cfg.JWTSecret)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		log.Info("shutting down", "timeout", shutdownTimeout)
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
