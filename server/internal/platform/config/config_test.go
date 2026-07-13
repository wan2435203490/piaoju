package config

import (
	"strings"
	"testing"
	"time"
)

// clearEnv 清掉全部 PIAOJU_ 变量（t.Setenv 空串在本包语义 = 未设置），防外部环境串扰。
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"PIAOJU_HTTP_ADDR", "PIAOJU_DB_DSN", "PIAOJU_JWT_SECRET",
		"PIAOJU_ACCESS_TTL", "PIAOJU_REFRESH_TTL",
		"PIAOJU_UPLOAD_DIR", "PIAOJU_UPLOAD_MAX_MB",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadMissingJWTSecret(t *testing.T) {
	clearEnv(t)
	_, err := Load()
	if err == nil {
		t.Fatal("Load() = nil error, want error when PIAOJU_JWT_SECRET is missing")
	}
	if !strings.Contains(err.Error(), "PIAOJU_JWT_SECRET") {
		t.Fatalf("err = %v, want mention of PIAOJU_JWT_SECRET", err)
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("PIAOJU_JWT_SECRET", "s")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.AccessTTL != 15*time.Minute {
		t.Errorf("AccessTTL = %v", cfg.AccessTTL)
	}
	if cfg.RefreshTTL != 720*time.Hour {
		t.Errorf("RefreshTTL = %v", cfg.RefreshTTL)
	}
	if cfg.UploadDir != "./uploads" {
		t.Errorf("UploadDir = %q", cfg.UploadDir)
	}
	if cfg.UploadMaxMB != 10 {
		t.Errorf("UploadMaxMB = %d", cfg.UploadMaxMB)
	}
	if !strings.Contains(cfg.DBDSN, "parseTime=true") {
		t.Errorf("default DSN should carry parseTime=true, got %q", cfg.DBDSN)
	}
}

func TestLoadOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("PIAOJU_JWT_SECRET", "s")
	t.Setenv("PIAOJU_HTTP_ADDR", ":9999")
	t.Setenv("PIAOJU_ACCESS_TTL", "1h")
	t.Setenv("PIAOJU_UPLOAD_MAX_MB", "20")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":9999" || cfg.AccessTTL != time.Hour || cfg.UploadMaxMB != 20 {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadBadValues(t *testing.T) {
	clearEnv(t)
	t.Setenv("PIAOJU_JWT_SECRET", "s")
	t.Setenv("PIAOJU_ACCESS_TTL", "fifteen minutes")
	if _, err := Load(); err == nil {
		t.Fatal("want error for invalid duration")
	}

	clearEnv(t)
	t.Setenv("PIAOJU_JWT_SECRET", "s")
	t.Setenv("PIAOJU_UPLOAD_MAX_MB", "ten")
	if _, err := Load(); err == nil {
		t.Fatal("want error for invalid integer")
	}
}
