// 本文件用真实 SQLite 数据库验证共享同步与生命周期规则，不依赖任何真实云账号。
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

type blockingCollector struct {
	entered chan struct{}
	release chan struct{}
}

func (c blockingCollector) Collect(context.Context, Source, []byte) ([]CollectionResult, error) {
	close(c.entered)
	<-c.release
	return []CollectionResult{}, nil
}

type collectorStub struct {
	results []CollectionResult
	err     error
}

func (c collectorStub) Collect(context.Context, Source, []byte) ([]CollectionResult, error) {
	return c.results, c.err
}

func newResourceServiceTest(t *testing.T) (*Service, *gorm.DB, *Source, *time.Time) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("打开资源测试数据库失败")
	}
	if err := db.AutoMigrate(&Source{}, &Resource{}, &Endpoint{}, &SyncJob{}, &AuditLog{}); err != nil {
		t.Fatal("创建资源测试表失败")
	}
	cipher := NewCredentialCipher("resource-service-test-key")
	encrypted, _ := cipher.Encrypt([]byte(`{"token":"example"}`))
	source := &Source{ProjectID: 1, Provider: ProviderAWS, Name: "测试接入源", Region: "cn-test", EncryptedCredential: encrypted, Enabled: true, SyncIntervalMinutes: 60}
	if err := db.Create(source).Error; err != nil {
		t.Fatal("准备接入源失败")
	}
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	service := NewService(NewRepository(db), cipher)
	service.now = func() time.Time { return now }
	return service, db, source, &now
}

// TestSyncAuditContainsChangesButNeverCredential 验证资源变化可追溯且审计内容不含凭证。
func TestSyncAuditContainsChangesButNeverCredential(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	_, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "ec2", Snapshots: []Snapshot{{ExternalID: "i-audit", Name: "审计实例"}}}}})
	if err != nil {
		t.Fatalf("同步审计准备失败：%v", err)
	}
	var audits []AuditLog
	_ = db.Order("id ASC").Find(&audits).Error
	encoded, _ := json.Marshal(audits)
	if !strings.Contains(string(encoded), "resource.created") || strings.Contains(string(encoded), "example") || strings.Contains(string(encoded), "token") {
		t.Fatal("审计必须记录资源变化且不得包含凭证内容或字段")
	}
}

// TestSyncIsIdempotentMarksMissingAndRestores 验证同步不重复建档、缺失后失联及重新出现恢复原记录。
func TestSyncIsIdempotentMarksMissingAndRestores(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	snapshot := Snapshot{ResourceType: "ec2", ExternalID: "i-1", Name: "计算节点", CloudStatus: "running"}
	collector := collectorStub{results: []CollectionResult{{ResourceType: "ec2", Snapshots: []Snapshot{snapshot}}}}
	firstJob, err := service.Sync(context.Background(), source.ID, "manual", collector)
	if err != nil {
		t.Fatalf("首次同步失败：%v", err)
	}
	if string(firstJob.Statistics) != `{"ec2":{"added":1,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0}}` {
		t.Fatal("首次同步必须记录完整且脱敏的资源变更统计")
	}
	*now = now.Add(time.Hour)
	if _, err := service.Sync(context.Background(), source.ID, "manual", collector); err != nil {
		t.Fatal("重复同步失败")
	}
	var resources []Resource
	_ = db.Find(&resources).Error
	if len(resources) != 1 {
		t.Fatalf("重复同步生成了 %d 条资源", len(resources))
	}
	*now = now.Add(time.Hour)
	service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "ec2"}}})
	_ = db.First(&resources[0], resources[0].ID).Error
	if resources[0].LifecycleStatus != LifecycleLost || resources[0].MissingSince == nil {
		t.Fatal("成功采集中的缺失资源必须标记失联")
	}
	*now = now.Add(time.Hour)
	service.Sync(context.Background(), source.ID, "manual", collector)
	var restored Resource
	_ = db.First(&restored, resources[0].ID).Error
	if restored.LifecycleStatus != LifecycleActive || restored.MissingSince != nil {
		t.Fatal("重新出现的资源必须恢复原记录")
	}
}

// TestSyncFailureDoesNotMarkResourcesMissing 验证单类失败和认证失败都不能错误更新失联状态。
func TestSyncFailureDoesNotMarkResourcesMissing(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	active := Resource{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: "i-1", LifecycleStatus: LifecycleActive, FirstSeenAt: time.Now(), LastSeenAt: time.Now()}
	_ = db.Create(&active).Error
	service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "ec2", Err: errors.New("采集失败")}}})
	_ = db.First(&active, active.ID).Error
	if active.LifecycleStatus != LifecycleActive {
		t.Fatal("单类采集失败不得标记失联")
	}
	service.Sync(context.Background(), source.ID, "manual", collectorStub{err: ErrAuthenticationFailed})
	_ = db.First(&active, active.ID).Error
	if active.LifecycleStatus != LifecycleActive {
		t.Fatal("认证失败不得标记失联")
	}
}

// TestPurgeLostResourcesAfterThreeDays 验证仅物理删除连续失联满 72 小时的资源。
func TestPurgeLostResourcesAfterThreeDays(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	old := now.Add(-72 * time.Hour)
	recent := now.Add(-71 * time.Hour)
	for index, missing := range []*time.Time{&old, &recent} {
		_ = db.Create(&Resource{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: string(rune('a' + index)), LifecycleStatus: LifecycleLost, MissingSince: missing, FirstSeenAt: old, LastSeenAt: old}).Error
	}
	deleted, err := service.PurgeLostResources(context.Background())
	if err != nil || deleted != 1 {
		t.Fatalf("三天清理数量错误：deleted=%d err=%v", deleted, err)
	}
	var audit AuditLog
	if err := db.Where("action = ? AND resource_id = ?", "resource.deleted", "a").First(&audit).Error; err != nil {
		t.Fatal("物理删除资源前必须保留独立审计记录")
	}
}

// TestSyncRejectsConcurrentRunForSameSource 验证同一接入源不能并发执行两个同步任务。
func TestSyncRejectsConcurrentRunForSameSource(t *testing.T) {
	service, _, source, _ := newResourceServiceTest(t)
	collector := blockingCollector{entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() { _, _ = service.Sync(context.Background(), source.ID, "manual", collector); close(done) }()
	<-collector.entered
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{}); !errors.Is(err, ErrSyncAlreadyRunning) {
		t.Fatal("同一接入源并发同步必须被拒绝")
	}
	close(collector.release)
	<-done
}

// TestSyncDueSourcesOnlyRunsEnabledDueSources 验证调度只处理已启用且到期的接入源。
func TestSyncDueSourcesOnlyRunsEnabledDueSources(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	_ = db.Model(source).Updates(map[string]any{"next_sync_at": past, "enabled": true}).Error
	disabled := Source{ProjectID: 1, Provider: ProviderAWS, Name: "停用源", EncryptedCredential: source.EncryptedCredential, Enabled: false, SyncIntervalMinutes: 60, NextSyncAt: &past}
	upcoming := Source{ProjectID: 1, Provider: ProviderAWS, Name: "未到期源", EncryptedCredential: source.EncryptedCredential, Enabled: true, SyncIntervalMinutes: 60, NextSyncAt: &future}
	_ = db.Create(&disabled).Error
	_ = db.Create(&upcoming).Error
	service.SyncDueSources(context.Background(), map[string]Collector{ProviderAWS: collectorStub{}})
	var jobs []SyncJob
	_ = db.Find(&jobs).Error
	if len(jobs) != 1 || jobs[0].SourceID != source.ID || jobs[0].Trigger != "scheduled" {
		t.Fatal("调度器必须仅为到期且启用的接入源创建定时任务")
	}
}
