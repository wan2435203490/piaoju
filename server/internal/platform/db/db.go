// Package db MySQL 连接池与内嵌迁移（golang-migrate + embed.FS）。
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Open 建立 MySQL 连接池并 Ping 验活；失败最多尝试 attempts 次、间隔 interval，
// 每次失败打 Warn 日志，全部失败返回带最后一次原因的错误（调用方退出进程）。
//
// DSN 会被强制 parseTime=true、loc=UTC：契约要求所有 DATETIME(3) 存取均为 UTC
// （piaoju-conventions §1），不给错误配置留口子。
func Open(ctx context.Context, dsn string, attempts int, interval time.Duration, log *slog.Logger) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse DSN: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC

	conn, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(10)
	conn.SetConnMaxLifetime(5 * time.Minute)

	var lastErr error
	for i := 1; i <= attempts; i++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		lastErr = conn.PingContext(pingCtx)
		cancel()
		if lastErr == nil {
			log.Info("db connected", "addr", cfg.Addr, "database", cfg.DBName)
			return conn, nil
		}
		log.Warn("db ping failed", "attempt", i, "max_attempts", attempts, "retry_in", interval, "err", lastErr)
		if i == attempts {
			break
		}
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			_ = conn.Close()
			return nil, fmt.Errorf("db: canceled while waiting for MySQL: %w", ctx.Err())
		}
	}
	_ = conn.Close()
	return nil, fmt.Errorf("db: MySQL unreachable at %s after %d attempts: %w", cfg.Addr, attempts, lastErr)
}
