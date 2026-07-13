// Package piaoju 模块根包：仅承载编译期资源嵌入。
//
// //go:embed 不能引用包目录之外的文件，因此 migrations 的嵌入点放在模块根，
// 由 internal/platform/db 消费（启动时自动执行迁移）。
package piaoju

import "embed"

// MigrationsFS 内嵌 server/migrations 全部迁移文件（up/down）。
// 迁移文件只增不改（piaoju-conventions §3）。
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS
