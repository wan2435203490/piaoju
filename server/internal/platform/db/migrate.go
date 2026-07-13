package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"piaoju"
)

// Migrate 对目标库执行内嵌迁移（server/migrations，见根包 piaoju.MigrationsFS）。
// 幂等：已是最新版本时为 no-op（ErrNoChange 不算错）。
//
// 单个迁移文件包含多条 SQL 语句，必须 multiStatements=true 才能整文件执行；
// 业务连接不需要（也不应该）开启该选项，所以这里用独立短命连接，跑完即关。
func Migrate(dsn string, log *slog.Logger) error {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("db: migrate: parse DSN: %w", err)
	}
	cfg.MultiStatements = true
	cfg.ParseTime = true
	cfg.Loc = time.UTC

	conn, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return fmt.Errorf("db: migrate: open: %w", err)
	}
	defer conn.Close()

	src, err := iofs.New(piaoju.MigrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("db: migrate: load embedded migrations: %w", err)
	}
	driver, err := migratemysql.WithInstance(conn, &migratemysql.Config{})
	if err != nil {
		return fmt.Errorf("db: migrate: init driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "mysql", driver)
	if err != nil {
		return fmt.Errorf("db: migrate: init migrator: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: migrate: up: %w", err)
	}
	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("db: migrate: read version: %w", err)
	}
	log.Info("migrations up-to-date", "version", version, "dirty", dirty)
	return nil
}
