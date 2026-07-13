package db

// 本机无 Docker/MySQL：只测「不碰真库」的纯逻辑 —— 内嵌迁移能被 iofs 正确
// 解析、版本序列完整。真库执行路径由 Wave 3 集成阶段 docker compose 冒烟覆盖。

import (
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"

	"piaoju"
)

func TestEmbeddedMigrationsParse(t *testing.T) {
	src, err := iofs.New(piaoju.MigrationsFS, "migrations")
	if err != nil {
		t.Fatalf("iofs.New: %v", err)
	}
	t.Cleanup(func() { _ = src.Close() })

	first, err := src.First()
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if first != 1 {
		t.Fatalf("first version = %d, want 1", first)
	}

	// 版本连续且每版都有 up/down。
	var versions []uint
	for v, err := first, error(nil); err == nil; v, err = src.Next(v) {
		versions = append(versions, v)
		up, _, uerr := src.ReadUp(v)
		if uerr != nil {
			t.Fatalf("ReadUp(%d): %v", v, uerr)
		}
		_ = up.Close()
		down, _, derr := src.ReadDown(v)
		if derr != nil {
			t.Fatalf("ReadDown(%d): %v", v, derr)
		}
		_ = down.Close()
	}
	want := []uint{1, 2, 3}
	if len(versions) != len(want) {
		t.Fatalf("versions = %v, want %v", versions, want)
	}
	for i := range want {
		if versions[i] != want[i] {
			t.Fatalf("versions = %v, want %v", versions, want)
		}
	}
}

func TestEmbeddedFSMatchesDisk(t *testing.T) {
	entries, err := piaoju.MigrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	disk, err := os.ReadDir("../../../migrations")
	if err != nil {
		t.Skipf("migrations dir not readable from test cwd: %v", err)
	}
	var diskSQL int
	for _, e := range disk {
		if !e.IsDir() {
			diskSQL++
		}
	}
	if len(entries) != diskSQL {
		t.Fatalf("embedded %d files, disk has %d — re-run go build (embed stale?)", len(entries), diskSQL)
	}
}

func TestMigrateBadDSN(t *testing.T) {
	err := Migrate("not a dsn", discardLogger())
	if err == nil {
		t.Fatal("want error for invalid DSN")
	}
	if !strings.Contains(err.Error(), "DSN") {
		t.Fatalf("err = %v, want DSN parse error", err)
	}
}
