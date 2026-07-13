package db

// 本机无 Docker/MySQL：仅覆盖 DSN 解析 / 重试退出等纯逻辑路径。

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestOpenBadDSN(t *testing.T) {
	_, err := Open(context.Background(), "://broken", 1, time.Millisecond, discardLogger())
	if err == nil {
		t.Fatal("want error for invalid DSN")
	}
	if !strings.Contains(err.Error(), "DSN") {
		t.Fatalf("err = %v, want DSN parse error", err)
	}
}

// TestOpenUnreachableGivesUp 连不上的地址重试耗尽后必须返回带原因的错误（不 hang 死）。
func TestOpenUnreachableGivesUp(t *testing.T) {
	// 127.0.0.1:1 几乎必然拒连；2 次尝试 × 1ms 间隔，秒级完成。
	dsn := "u:p@tcp(127.0.0.1:1)/nope?timeout=200ms"
	start := time.Now()
	_, err := Open(context.Background(), dsn, 2, time.Millisecond, discardLogger())
	if err == nil {
		t.Fatal("want error for unreachable MySQL")
	}
	if !strings.Contains(err.Error(), "after 2 attempts") {
		t.Fatalf("err = %v, want attempts count in message", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("retry loop took unreasonably long")
	}
}

// TestOpenContextCanceled 等待重试期间 ctx 取消要立即退出。
func TestOpenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	dsn := "u:p@tcp(127.0.0.1:1)/nope?timeout=200ms"
	_, err := Open(ctx, dsn, 5, 10*time.Second, discardLogger())
	if err == nil {
		t.Fatal("want error when context canceled")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("err = %v, want cancellation error", err)
	}
}
