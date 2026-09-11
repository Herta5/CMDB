// Command migrate 供数据库管理员显式推进 CMDB 结构版本，不参与应用自动启动。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"cmdb/internal/platform/config"
	"cmdb/internal/platform/database"
	"gorm.io/gorm"
)

type migrationConfigLoader func() (config.Database, error)
type migrationDatabaseOpener func(config.Database) (*gorm.DB, error)
type schemaMigrator func(context.Context, *gorm.DB) error

func main() {
	if err := runMigration(context.Background(), config.LoadMigrationDatabase, database.Open, database.Migrate); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "数据库迁移成功")
}

// runMigration 按配置、连接、迁移三个阶段执行，并在边界处收敛全部底层错误。
func runMigration(ctx context.Context, load migrationConfigLoader, open migrationDatabaseOpener, migrate schemaMigrator) error {
	databaseConfig, err := load()
	if err != nil {
		return errors.New("数据库迁移失败：请检查管理员迁移配置")
	}
	db, err := open(databaseConfig)
	if err != nil {
		return errors.New("数据库迁移失败：无法连接数据库")
	}
	if err := migrate(ctx, db); err != nil {
		return errors.New("数据库迁移失败：结构未更新")
	}
	return nil
}
