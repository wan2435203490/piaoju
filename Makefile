# 自动加载 .env（存在时）并导出给子进程；Make 原样取值，DSN 里的 &/()/! 不会被 shell 吃掉
-include .env
export

MYSQL_DSN ?= mysql://piaoju:piaoju@tcp(127.0.0.1:3306)/piaoju
MIGRATE   := docker run --rm -v $(PWD)/server/migrations:/migrations --network host \
             migrate/migrate:v4.17.1 -path=/migrations -database "$(MYSQL_DSN)"

.PHONY: dev test lint db-up db-down migrate-up migrate-down web-dev web-check

## 后端（server/go.mod 由 S1 建立后生效）
dev:
	cd server && go run ./cmd/api

test:
	cd server && go test ./...

lint:
	cd server && go vet ./... && test -z "$$(gofmt -l .)"

## 数据库
db-up:
	docker compose up -d mysql

db-down:
	docker compose down

migrate-up: db-up
	$(MIGRATE) up

migrate-down:
	$(MIGRATE) down 1

## 前端
web-dev:
	cd web && VITE_MOCK=1 pnpm dev

web-check:
	cd web && pnpm check && pnpm test
