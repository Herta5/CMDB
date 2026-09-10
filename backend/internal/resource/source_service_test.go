// 本文件验证接入源创建、加密存储和项目隔离查询。
package resource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestCreateSourceEncryptsCredentialAndDefaultsInterval 验证凭证不以明文落库且同步周期默认为一小时。
func TestCreateSourceEncryptsCredentialAndDefaultsInterval(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	_ = db.AutoMigrate(&Source{})
	cipher := NewCredentialCipher("source-test-deployment-key")
	service := NewService(NewRepository(db), cipher)
	credential := json.RawMessage(`{"access_key_id":"example-id","access_key_secret":"example-secret"}`)
	source, err := service.CreateSource(context.Background(), CreateSourceInput{ProjectID: 7, Provider: ProviderAliyun, Name: "阿里云生产账号", Region: "cn-hangzhou", Credential: credential})
	if err != nil {
		t.Fatal("创建接入源失败")
	}
	if source.SyncIntervalMinutes != 60 || source.EncryptedCredential == "" || strings.Contains(source.EncryptedCredential, "example") {
		t.Fatal("接入源必须使用默认周期并加密凭证")
	}
	plain, err := cipher.Decrypt(source.EncryptedCredential)
	if err != nil || string(plain) != string(credential) {
		t.Fatal("持久化密文无法由部署密钥恢复")
	}
	encoded, _ := json.Marshal(source)
	if strings.Contains(string(encoded), "encrypted") || strings.Contains(string(encoded), "example-secret") {
		t.Fatal("接入源响应不得暴露密文或明文凭证")
	}
}

// TestCreateSourceRejectsRemovedKubernetesProvider 验证旧客户端不能继续创建已下线的平台接入源。
func TestCreateSourceRejectsRemovedKubernetesProvider(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	_ = db.AutoMigrate(&Source{})
	service := NewService(NewRepository(db), NewCredentialCipher("source-provider-key"))
	_, err := service.CreateSource(context.Background(), CreateSourceInput{ProjectID: 7, Provider: "kubernetes", Name: "旧集群", Credential: json.RawMessage(`{"token":"value"}`)})
	if err == nil {
		t.Fatal("已移除的 Kubernetes provider 必须被拒绝")
	}
}

// TestUpdateSourceKeepsCredentialAndDeleteCascades 验证未提交新凭证时保留密文，并可删除项目内接入源。
func TestUpdateSourceKeepsCredentialAndDeleteCascades(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	_ = db.Exec("PRAGMA foreign_keys = ON").Error
	_ = db.AutoMigrate(&Source{}, &Server{}, &Database{}, &LoadBalancer{}, &SyncJob{}, &AuditLog{})
	service := NewService(NewRepository(db), NewCredentialCipher("source-update-key"))
	created, err := service.CreateSource(context.Background(), CreateSourceInput{ProjectID: 3, Provider: ProviderAWS, Name: "旧名称", Credential: json.RawMessage(`{"token":"old"}`)})
	if err != nil {
		t.Fatal("准备接入源失败")
	}
	originalCiphertext := created.EncryptedCredential
	updated, err := service.UpdateSource(context.Background(), 3, created.ID, UpdateSourceInput{Name: "新名称", Region: "ap-east-1", Config: json.RawMessage(`{"tag":"prod"}`), Enabled: false, SyncIntervalMinutes: 120})
	if err != nil {
		t.Fatal("更新接入源失败")
	}
	if updated.Name != "新名称" || updated.Enabled || updated.EncryptedCredential != originalCiphertext || updated.SyncIntervalMinutes != 120 {
		t.Fatal("更新非敏感配置时必须保留原凭证并应用启停和周期")
	}
	if err := service.DeleteSource(context.Background(), 3, created.ID); err != nil {
		t.Fatal("删除接入源失败")
	}
	if _, err := service.FindSourceForProject(context.Background(), 3, created.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("删除后接入源必须不可见")
	}
}

// TestListJobsUsesProjectBoundary 验证同步任务查询不会返回其他项目的记录。
func TestListJobsUsesProjectBoundary(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	_ = db.AutoMigrate(&Source{}, &SyncJob{})
	service := NewService(NewRepository(db), NewCredentialCipher("job-list-key"))
	now := time.Now()
	_ = db.Create(&SyncJob{ProjectID: 1, SourceID: 10, Status: "success", Trigger: "manual", StartedAt: now}).Error
	_ = db.Create(&SyncJob{ProjectID: 2, SourceID: 20, Status: "failed", Trigger: "scheduled", StartedAt: now}).Error
	values, total, err := service.ListJobs(context.Background(), 1, 0, "", 1, 20)
	if err != nil || total != 1 || len(values) != 1 || values[0].ProjectID != 1 {
		t.Fatal("任务列表必须严格限制在当前项目")
	}
}

// TestListSourcesFiltersProjectAndProvider 验证列表不能跨项目或跨平台返回接入源。
func TestListSourcesFiltersProjectAndProvider(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	_ = db.AutoMigrate(&Source{})
	service := NewService(NewRepository(db), NewCredentialCipher("source-test-key"))
	for _, input := range []CreateSourceInput{{ProjectID: 1, Provider: ProviderAWS, Name: "AWS"}, {ProjectID: 1, Provider: ProviderAliyun, Name: "阿里云"}, {ProjectID: 2, Provider: ProviderAWS, Name: "其他项目"}} {
		input.Credential = json.RawMessage(`{"token":"value"}`)
		if _, err := service.CreateSource(context.Background(), input); err != nil {
			t.Fatal("准备接入源失败")
		}
	}
	values, err := service.ListSources(context.Background(), 1, ProviderAWS)
	if err != nil || len(values) != 1 || values[0].Name != "AWS" {
		t.Fatal("接入源列表未正确执行项目和平台过滤")
	}
}
