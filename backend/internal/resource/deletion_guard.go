// 本文件集中维护父对象删除不变量与锁顺序，项目和资源领域必须复用同一套依赖检查。
package resource

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrDeleteDependencyConflict 统一表示资产或活动任务仍引用父对象，不公开依赖数量和归属细节。
var ErrDeleteDependencyConflict = errors.New("存在资产或活动同步任务，不能删除")

// DeletionGuard 绑定调用方事务；锁、检查、审计和父删除必须使用该事务连接。
type DeletionGuard struct{ db *gorm.DB }

// NewDeletionGuard 创建资源核心守卫，供平台层装配以及事务仓储重新绑定。
func NewDeletionGuard(db *gorm.DB) *DeletionGuard { return &DeletionGuard{db: db} }

// WithTransaction 保留装配的守卫并绑定当前事务，禁止项目仓储把锁查询发回事务外连接。
func (g *DeletionGuard) WithTransaction(tx *gorm.DB) *DeletionGuard {
	guard := *g
	guard.db = tx
	return &guard
}

// LockProjectForOperation 先锁项目，阻止父检查期间产生新来源、任务或资产；停用项目既有任务也可持锁执行。
func (g *DeletionGuard) LockProjectForOperation(ctx context.Context, projectID uint64) error {
	var parent struct{ ID uint64 }
	return g.db.WithContext(ctx).Table("projects").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", projectID).Take(&parent).Error
}

// LockSourceForOperation 总是先锁项目再重读来源；合并归属条件使不存在和跨项目对象保持同一结果。
func (g *DeletionGuard) LockSourceForOperation(ctx context.Context, projectID, sourceID uint64) (*Source, error) {
	if err := g.LockProjectForOperation(ctx, projectID); err != nil {
		return nil, err
	}
	var source Source
	if err := g.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND project_id = ?", sourceID, projectID).Take(&source).Error; err != nil {
		return nil, err
	}
	return &source, nil
}

// CheckProjectDependencies 在项目锁之后按来源 ID 递增加锁，再检查项目下全部资产和活动任务。
func (g *DeletionGuard) CheckProjectDependencies(ctx context.Context, projectID uint64) error {
	var sources []struct{ ID uint64 }
	if err := g.db.WithContext(ctx).Table("resource_sources").Select("id").Where("project_id = ?", projectID).Order("id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&sources).Error; err != nil {
		return err
	}
	return g.checkDependencies(ctx, "project_id", projectID)
}

// CheckSourceDependencies 在父锁保护下检查来源；正常、失联乃至异常状态的在库资产都不能被父删除清理。
func (g *DeletionGuard) CheckSourceDependencies(ctx context.Context, sourceID uint64) error {
	return g.checkDependencies(ctx, "source_id", sourceID)
}

// checkDependencies 仅查询是否存在，固定表名和字段来自内部调用，不受外部输入控制。
func (g *DeletionGuard) checkDependencies(ctx context.Context, column string, id uint64) error {
	for _, table := range []string{"resources_servers", "resources_databases", "resources_load_balancers", "sync_jobs"} {
		query := "SELECT EXISTS (SELECT 1 FROM " + table + " WHERE " + column + " = ?"
		if table == "sync_jobs" {
			query += " AND status IN ('queued', 'running')"
		}
		var exists bool
		if err := g.db.WithContext(ctx).Raw(query+")", id).Scan(&exists).Error; err != nil {
			return err
		}
		if exists {
			return ErrDeleteDependencyConflict
		}
	}
	return nil
}

// NormalizeDeleteError 把外键兜底拒绝收敛为同一业务错误；必须在事务回滚后调用，底层细节不进入响应。
func NormalizeDeleteError(err error) error {
	if errors.Is(err, gorm.ErrForeignKeyViolated) {
		return ErrDeleteDependencyConflict
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23503" {
		return ErrDeleteDependencyConflict
	}
	return err
}
