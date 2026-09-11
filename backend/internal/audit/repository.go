// 本文件实现统一审计的 PostgreSQL/GORM 写入与筛选查询。
package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"gorm.io/gorm"
)

// Recorder 是其他业务领域唯一依赖的审计写入边界。
type Recorder interface {
	Record(ctx context.Context, entry Entry) error
}

// Repository 使用平台统一数据库连接保存和查询审计。
type Repository struct {
	db *gorm.DB
}

// NewRepository 创建审计仓储，数据库连接生命周期仍由平台层管理。
func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

// Record 合并请求操作者并递归移除敏感键后写入长期审计。
func (r *Repository) Record(ctx context.Context, entry Entry) error {
	if r == nil || r.db == nil {
		return ErrRepositoryUnavailable
	}
	return recordWithDatabase(ctx, r.db, entry, true)
}

// RecordInTransaction 让资源生命周期在自身事务内复用统一的身份快照、脱敏和审计模型。
func RecordInTransaction(ctx context.Context, database *gorm.DB, entry Entry) error {
	// 批量资源已从上下文取得操作者名称，不逐条关联用户和项目表，避免同步产生 N+1 查询。
	return recordWithDatabase(ctx, database, entry, false)
}

// recordWithDatabase 合并上下文、字段脱敏和必要的管理对象名称查询。
func recordWithDatabase(ctx context.Context, database *gorm.DB, entry Entry, enrichReferences bool) error {
	if database == nil {
		return ErrRepositoryUnavailable
	}
	metadata, hasMetadata := actorFromContext(ctx)
	if hasMetadata {
		if entry.ActorID == nil {
			actorID := metadata.ID
			entry.ActorID = &actorID
		}
		if entry.RequestIP == "" {
			entry.RequestIP = metadata.RequestIP
		}
	}
	detail, err := sanitizeDetail(entry.Detail)
	if err != nil {
		return err
	}
	if hasMetadata {
		if metadata.Username != "" {
			detail["actor_username"] = metadata.Username
		}
		if metadata.DisplayName != "" {
			detail["actor_display_name"] = metadata.DisplayName
		}
	}
	// 操作者名称快照用于用户被后续删除时继续辨认历史，不保存邮箱或其他身份资料。
	if enrichReferences && entry.ActorID != nil && metadata.Username == "" && metadata.DisplayName == "" {
		var actor struct {
			Username    string `gorm:"column:username"`
			DisplayName string `gorm:"column:display_name"`
		}
		if err := database.WithContext(ctx).Table("users").Select("username, display_name").Where("id = ?", *entry.ActorID).Take(&actor).Error; err == nil {
			detail["actor_username"] = actor.Username
			detail["actor_display_name"] = actor.DisplayName
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	// 项目名称快照让项目物理删除后，全局审计仍能展示原归属。
	if enrichReferences && entry.ProjectID != nil {
		var project struct {
			Name string `gorm:"column:name"`
		}
		if err := database.WithContext(ctx).Table("projects").Select("name").Where("id = ?", *entry.ProjectID).Take(&project).Error; err == nil {
			detail["project_name"] = project.Name
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	return database.WithContext(ctx).Create(&Log{
		ActorID: entry.ActorID, ProjectID: entry.ProjectID, Action: entry.Action,
		ResourceType: entry.ResourceType, ResourceID: entry.ResourceID,
		Detail: encoded, RequestIP: entry.RequestIP,
	}).Error
}

// sanitizeDetail 先按最终 JSON 形态规格化任意嵌套集合，再执行递归脱敏。
// 这样可以覆盖 []map[string]any、结构体和自定义切片等调用方可能传入的具体类型。
func sanitizeDetail(value map[string]any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var normalized map[string]any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, err
	}
	return sanitizeMap(normalized), nil
}

// List 按筛选条件查询审计，创建时间相同时使用 ID 倒序保证分页稳定。
func (r *Repository) List(ctx context.Context, filter Filter) ([]Log, int64, uint64, error) {
	if r == nil || r.db == nil {
		return nil, 0, 0, ErrRepositoryUnavailable
	}
	// 操作者筛选同时影响快照边界、总数和结果，关联必须先于这些查询构建。
	base := r.db.WithContext(ctx).Table("audit_logs AS audit_logs").
		Joins("LEFT JOIN users ON users.id = audit_logs.actor_id")
	base = applyFilter(base, filter)
	snapshotID := filter.SnapshotID
	if snapshotID == 0 {
		// 审计日志只增不删，最大 ID 可作为本次翻页期间稳定且低成本的快照边界。
		if err := base.Session(&gorm.Session{}).Select("COALESCE(MAX(audit_logs.id), 0)").Scan(&snapshotID).Error; err != nil {
			return nil, 0, 0, err
		}
	}
	base = base.Where("audit_logs.id <= ?", snapshotID)
	var total int64
	// 每个终结查询使用独立语句，避免 GORM 将上一条查询已构建的关联条件重复累积。
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	var items []Log
	query := base.Select("audit_logs.*, users.username AS actor_username, users.display_name AS actor_display_name, projects.name AS project_name").
		Joins("LEFT JOIN projects ON projects.id = audit_logs.project_id").
		Order("audit_logs.created_at DESC, audit_logs.id DESC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize)
	if err := query.Scan(&items).Error; err != nil {
		return nil, 0, 0, err
	}
	for index := range items {
		applyNameSnapshots(&items[index])
	}
	return items, total, snapshotID, nil
}

// applyFilter 只构造参数化精确条件，禁止把页面输入拼接为 SQL。
func applyFilter(query *gorm.DB, filter Filter) *gorm.DB {
	if filter.ProjectID != nil {
		query = query.Where("audit_logs.project_id = ?", *filter.ProjectID)
	}
	if filter.Action != "" {
		query = query.Where("audit_logs.action = ?", filter.Action)
	}
	if filter.ActorUsername != "" {
		query = query.Where(actorUsernameExpression(query.Dialector.Name())+" = ?", filter.ActorUsername)
	}
	if filter.ResourceType != "" {
		query = query.Where("audit_logs.resource_type = ?", filter.ResourceType)
	}
	if filter.ResourceID != "" {
		query = query.Where("audit_logs.resource_id = ?", filter.ResourceID)
	}
	if filter.StartAt != nil {
		query = query.Where("audit_logs.created_at >= ?", *filter.StartAt)
	}
	if filter.EndAt != nil {
		query = query.Where("audit_logs.created_at <= ?", *filter.EndAt)
	}
	return query
}

// actorUsernameExpression 兼容 PostgreSQL 与 SQLite，用户删除后仍可用审计快照精确筛选。
func actorUsernameExpression(dialect string) string {
	if dialect == "sqlite" {
		return "COALESCE(users.username, json_extract(audit_logs.detail, '$.actor_username'))"
	}
	return "COALESCE(users.username, audit_logs.detail ->> 'actor_username')"
}

// applyNameSnapshots 补齐已删除对象的显示名称，并在输出历史记录前清理内部用户引用。
func applyNameSnapshots(log *Log) {
	if log == nil {
		return
	}
	var detail map[string]any
	if len(log.Detail) > 0 {
		// 无法解析的历史详情没有可信快照，按空详情处理，不能绕过用户标识清理。
		decoder := json.NewDecoder(bytes.NewReader(log.Detail))
		// 原始详情重新编码时保留非用户数字的精度，避免改变项目等其他业务引用。
		decoder.UseNumber()
		if decoder.Decode(&detail) != nil {
			detail = nil
		}
	}
	if log.ActorUsername == "" {
		log.ActorUsername, _ = detail["actor_username"].(string)
	}
	if log.ActorDisplayName == "" {
		log.ActorDisplayName, _ = detail["actor_display_name"].(string)
	}
	if log.ProjectName == "" {
		log.ProjectName, _ = detail["project_name"].(string)
	}
	if (log.ResourceType == "user" || log.ResourceType == "project_member") && isNumericIdentifier(log.ResourceID) {
		// 仅快照可以区分数字用户名与旧数据库主键；缺少快照时不可猜测用户身份。
		log.ResourceID, _ = detail["target_username"].(string)
	}
	// 已解码的 JSON 值可以安全重新编码，历史记录也复用写入时的递归脱敏规则。
	log.Detail, _ = json.Marshal(sanitizeMap(detail))
}

// isNumericIdentifier 不解析整数，避免超出 uint64 范围的历史标识绕过清理。
func isNumericIdentifier(value string) bool {
	return value != "" && strings.IndexFunc(value, func(char rune) bool { return char < '0' || char > '9' }) == -1
}

// sanitizeMap 复制安全详情并递归删除认证字段及内部用户引用。
func sanitizeMap(value map[string]any) map[string]any {
	clean := make(map[string]any, len(value))
	for key, item := range value {
		if sensitiveKey(key) {
			continue
		}
		clean[key] = sanitizeValue(item)
	}
	return clean
}

// sanitizeValue 逐层处理数组和对象，确保数组内嵌数组不会绕过字段清理。
func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeMap(typed)
	case []any:
		items := make([]any, len(typed))
		for index, child := range typed {
			items[index] = sanitizeValue(child)
		}
		return items
	default:
		return value
	}
}

// sensitiveKey 统一匹配凭证及内部用户引用，兼容蛇形、短横线和大小写写法。
func sensitiveKey(key string) bool {
	normalized := strings.Map(func(value rune) rune {
		if unicode.IsLetter(value) || unicode.IsDigit(value) {
			return unicode.ToLower(value)
		}
		return -1
	}, key)
	for _, blocked := range []string{
		"actorid", "userid", "targetuserid", "owneruserid", "previousowneruserid",
		"password", "passwordhash",
		"token", "sessiontoken", "accesstoken", "refreshtoken",
		"apikey", "accesskey", "accesskeyid", "accesskeysecret",
		"secret", "secretkey", "secretaccesskey", "clientsecret",
		"credential", "encryptedcredential", "authorization",
		"ciphertext", "cmdbencryptionkey",
	} {
		if normalized == blocked {
			return true
		}
	}
	return false
}
