# syntax=docker/dockerfile:1
# 多阶段构建：静态编译（CGO_ENABLED=0）+ distroless 运行层，目标镜像 < 25MB。

FROM golang:1.25-alpine AS build
WORKDIR /src
# 先拷 go.mod/go.sum 利用层缓存
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
# WORKDIR=/app：compose 把 ./uploads 挂到 /app/uploads，对应默认 PIAOJU_UPLOAD_DIR=./uploads
WORKDIR /app
COPY --from=build /out/api /app/api
EXPOSE 8080
ENTRYPOINT ["/app/api"]
