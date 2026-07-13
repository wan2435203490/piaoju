// Package config 从环境变量加载服务配置（PIAOJU_ 前缀，参考仓库根目录 .env.example）。
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config 服务全量配置。除 JWTSecret 必填外均有默认值。
type Config struct {
	HTTPAddr    string        // PIAOJU_HTTP_ADDR    监听地址，默认 :8080
	DBDSN       string        // PIAOJU_DB_DSN       go-sql-driver/mysql DSN
	JWTSecret   string        // PIAOJU_JWT_SECRET   必填，缺失时 Load 报错
	AccessTTL   time.Duration // PIAOJU_ACCESS_TTL   access token 有效期，默认 15m
	RefreshTTL  time.Duration // PIAOJU_REFRESH_TTL  refresh token 有效期，默认 720h（30d）
	UploadDir   string        // PIAOJU_UPLOAD_DIR   上传文件目录，默认 ./uploads
	UploadMaxMB int           // PIAOJU_UPLOAD_MAX_MB 上传大小上限，默认 10
}

// Load 读取环境变量并校验；缺 PIAOJU_JWT_SECRET 时返回错误（启动即失败）。
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:  getenv("PIAOJU_HTTP_ADDR", ":8080"),
		DBDSN:     getenv("PIAOJU_DB_DSN", "piaoju:piaoju@tcp(127.0.0.1:3306)/piaoju?parseTime=true&loc=UTC&charset=utf8mb4"),
		JWTSecret: os.Getenv("PIAOJU_JWT_SECRET"),
		UploadDir: getenv("PIAOJU_UPLOAD_DIR", "./uploads"),
	}
	if cfg.JWTSecret == "" {
		return Config{}, fmt.Errorf("config: PIAOJU_JWT_SECRET is required (JWT signing secret, see .env.example)")
	}

	var err error
	if cfg.AccessTTL, err = getenvDuration("PIAOJU_ACCESS_TTL", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.RefreshTTL, err = getenvDuration("PIAOJU_REFRESH_TTL", 720*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.UploadMaxMB, err = getenvInt("PIAOJU_UPLOAD_MAX_MB", 10); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// getenv 读环境变量；空串视为未设置，回落默认值。
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s=%q is not a valid duration (e.g. 15m, 720h): %w", key, v, err)
	}
	return d, nil
}

func getenvInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s=%q is not a valid integer: %w", key, v, err)
	}
	return n, nil
}
