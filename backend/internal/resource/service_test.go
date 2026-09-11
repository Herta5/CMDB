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

// Probe 让阻塞采集器满足统一接口；并发同步测试不应进入连接探测路径。
func (c blockingCollector) Probe(context.Context, Source, []byte) ([]CollectionResult, error) {
	return nil, errors.New("并发同步测试不应执行连接探测")
}

type collectorStub struct {
	results      []CollectionResult
	err          error
	probeResults []CollectionResult
	probeErr     error
	collectCalls *int
	probeCalls   *int
}

func (c collectorStub) Collect(context.Context, Source, []byte) ([]CollectionResult, error) {
	if c.collectCalls != nil {
		*c.collectCalls++
	}
	return c.results, c.err
}

// Probe 返回不含资源快照的轻量探测结果，用于区分连接测试与完整同步。
func (c collectorStub) Probe(context.Context, Source, []byte) ([]CollectionResult, error) {
	if c.probeCalls != nil {
		*c.probeCalls++
	}
	return c.probeResults, c.probeErr
}

func newResourceServiceTest(t *testing.T) (*Service, *gorm.DB, *Source, *time.Time) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("打开资源测试数据库失败")
	}
	// SQLite 内存库按连接隔离；异步工作器测试固定单连接，避免读到另一份空数据库。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("获取资源测试数据库连接失败")
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&Source{}, &Server{}, &Database{}, &LoadBalancer{}, &SyncJob{}, &AuditLog{}); err != nil {
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
	snapshot := Snapshot{ResourceType: "ec2", ExternalID: "i-1", Name: "计算节点", CloudStatus: "running", Endpoints: []EndpointSnapshot{{Kind: "private", Address: "10.0.0.8"}}}
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
	var resources []Server
	_ = db.Find(&resources).Error
	if len(resources) != 1 {
		t.Fatalf("重复同步生成了 %d 条资源", len(resources))
	}
	if !strings.Contains(string(resources[0].PrivateIPs), "10.0.0.8") {
		t.Fatal("资源访问端点必须随资源快照写入同一行")
	}
	*now = now.Add(time.Hour)
	service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "ec2"}}})
	_ = db.First(&resources[0], resources[0].ID).Error
	if resources[0].AssetStatus != AssetStatusLost || resources[0].MissingSince == nil {
		t.Fatal("成功采集中的缺失资源必须标记失联")
	}
	*now = now.Add(time.Hour)
	service.Sync(context.Background(), source.ID, "manual", collector)
	var restored Server
	_ = db.First(&restored, resources[0].ID).Error
	if restored.AssetStatus != AssetStatusActive || restored.MissingSince != nil {
		t.Fatal("重新出现的资源必须恢复原记录")
	}
}

// TestSyncUnchangedResourceOnlyRefreshesLastSeen 防止重复快照重写业务字段、虚增更新统计和制造无意义审计。
func TestSyncUnchangedResourceOnlyRefreshesLastSeen(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	firstSnapshot := Snapshot{ResourceType: "ec2", ExternalID: "i-stable", Name: "稳定实例", Region: "cn-test", CloudStatus: "running", RawAttributes: []byte(`{"name":"stable","cpu":4}`), Endpoints: []EndpointSnapshot{{Kind: "private", Address: "10.0.0.8"}}}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "ec2", Snapshots: []Snapshot{firstSnapshot}}}}); err != nil {
		t.Fatalf("准备首次同步失败：%v", err)
	}
	var before Server
	if err := db.Where("external_id = ?", firstSnapshot.ExternalID).First(&before).Error; err != nil {
		t.Fatalf("读取首次同步资源失败：%v", err)
	}

	// 通过真实数据库更新回调观察 SQL 的更新列；相同 JSON 对象即使键顺序不同，也不应触发业务字段更新。
	updatedColumns := map[string]int{}
	if err := db.Callback().Update().Before("gorm:update").Register("test:count_business_updates", func(tx *gorm.DB) {
		if tx.Statement.Table != "resources_servers" {
			return
		}
		if updates, ok := tx.Statement.Dest.(map[string]any); ok {
			for column := range updates {
				updatedColumns[column]++
			}
		}
	}); err != nil {
		t.Fatalf("注册数据库更新观察器失败：%v", err)
	}
	*now = now.Add(time.Hour)
	secondSnapshot := firstSnapshot
	secondSnapshot.RawAttributes = []byte(`{"cpu":4,"name":"stable"}`)
	job, err := service.Sync(context.Background(), source.ID, "scheduled", collectorStub{results: []CollectionResult{{ResourceType: "ec2", Snapshots: []Snapshot{secondSnapshot}}}})
	if err != nil {
		t.Fatalf("重复同步失败：%v", err)
	}
	if string(job.Statistics) != `{"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0}}` {
		t.Fatalf("相同快照不得计为业务更新：%s", job.Statistics)
	}
	if len(updatedColumns) != 1 || updatedColumns["last_seen_at"] != 1 {
		t.Fatalf("相同快照只能刷新最近发现时间：columns=%v", updatedColumns)
	}
	var persisted Server
	if err := db.Where("external_id = ?", firstSnapshot.ExternalID).First(&persisted).Error; err != nil {
		t.Fatalf("读取重复同步后的资源失败：%v", err)
	}
	if !persisted.LastSeenAt.Equal(*now) {
		t.Fatalf("相同快照仍须刷新最近发现时间：got=%s want=%s", persisted.LastSeenAt, *now)
	}
	if !persisted.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("相同快照不得改变业务更新时间：before=%s after=%s", before.UpdatedAt, persisted.UpdatedAt)
	}
	var updatedAudits int64
	if err := db.Model(&AuditLog{}).Where("action = ? AND resource_id = ?", "resource.updated", firstSnapshot.ExternalID).Count(&updatedAudits).Error; err != nil {
		t.Fatalf("查询资源更新审计失败：%v", err)
	}
	if updatedAudits != 0 {
		t.Fatalf("相同快照不得产生资源更新审计：count=%d", updatedAudits)
	}
}

// TestSyncChangedResourceRecordsOneUpdate 验证真实业务字段变化仍会持久化、计数并保留审计。
func TestSyncChangedResourceRecordsOneUpdate(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	snapshot := Snapshot{ResourceType: "ec2", ExternalID: "i-changed", Name: "变更前", CloudStatus: "running"}
	collector := collectorStub{results: []CollectionResult{{ResourceType: "ec2", Snapshots: []Snapshot{snapshot}}}}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collector); err != nil {
		t.Fatalf("准备首次同步失败：%v", err)
	}
	*now = now.Add(time.Hour)
	snapshot.Name = "变更后"
	collector.results[0].Snapshots[0] = snapshot
	job, err := service.Sync(context.Background(), source.ID, "scheduled", collector)
	if err != nil {
		t.Fatalf("同步资源变更失败：%v", err)
	}
	if string(job.Statistics) != `{"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":1}}` {
		t.Fatalf("真实业务变化必须计为一次更新：%s", job.Statistics)
	}
	var persisted Server
	if err := db.Where("external_id = ?", snapshot.ExternalID).First(&persisted).Error; err != nil || persisted.Name != "变更后" || !persisted.LastSeenAt.Equal(*now) {
		t.Fatalf("真实业务变化未正确持久化：resource=%+v err=%v", persisted, err)
	}
	var updatedAudits int64
	if err := db.Model(&AuditLog{}).Where("action = ? AND resource_id = ?", "resource.updated", snapshot.ExternalID).Count(&updatedAudits).Error; err != nil || updatedAudits != 1 {
		t.Fatalf("真实业务变化必须产生一次更新审计：count=%d err=%v", updatedAudits, err)
	}
}

// TestSyncDetectsTypeSpecificFieldChanges 验证 RDS 和负载均衡专属字段参与真实变化判断。
func TestSyncDetectsTypeSpecificFieldChanges(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	first := []CollectionResult{
		{ResourceType: "rds", Snapshots: []Snapshot{{ExternalID: "db-specific", Engine: "mysql", EngineVersion: "8.0"}}},
		{ResourceType: "elb", Snapshots: []Snapshot{{ExternalID: "lb-specific", NetworkType: "internet-facing"}}},
	}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: first}); err != nil {
		t.Fatalf("准备类型专属字段测试失败：%v", err)
	}
	*now = now.Add(time.Hour)
	second := []CollectionResult{
		{ResourceType: "rds", Snapshots: []Snapshot{{ExternalID: "db-specific", Engine: "postgres", EngineVersion: "17"}}},
		{ResourceType: "elb", Snapshots: []Snapshot{{ExternalID: "lb-specific", NetworkType: "internal"}}},
	}
	job, err := service.Sync(context.Background(), source.ID, "scheduled", collectorStub{results: second})
	if err != nil {
		t.Fatalf("同步类型专属字段变化失败：%v", err)
	}
	if string(job.Statistics) != `{"elb":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":1},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":1}}` {
		t.Fatalf("类型专属字段变化必须分别计为更新：%s", job.Statistics)
	}
	var database Database
	var loadBalancer LoadBalancer
	if err := db.Where("external_id = ?", "db-specific").First(&database).Error; err != nil || database.Engine != "postgres" || database.EngineVersion != "17" {
		t.Fatalf("RDS 专属字段未正确更新：resource=%+v err=%v", database, err)
	}
	if err := db.Where("external_id = ?", "lb-specific").First(&loadBalancer).Error; err != nil || loadBalancer.NetworkType != "internal" {
		t.Fatalf("负载均衡专属字段未正确更新：resource=%+v err=%v", loadBalancer, err)
	}
}

// TestSyncEndpointOrderDoesNotCreateFalseUpdates 防止云 API 和 DNS 返回集合顺序变化时制造伪更新。
func TestSyncEndpointOrderDoesNotCreateFalseUpdates(t *testing.T) {
	service, _, source, now := newResourceServiceTest(t)
	first := []CollectionResult{
		{ResourceType: "ec2", Snapshots: []Snapshot{{
			ExternalID: "i-order",
			Endpoints:  []EndpointSnapshot{{Kind: "private", Address: "10.0.0.2"}, {Kind: "private", Address: "10.0.0.1"}},
		}}},
		{ResourceType: "rds", Snapshots: []Snapshot{{
			ExternalID: "db-order",
			Endpoints: []EndpointSnapshot{
				{Kind: "hostname", Address: "secondary.example", Port: 3306, Protocol: "tcp"},
				{Kind: "hostname", Address: "primary.example", Port: 3306, Protocol: "tcp", ResolvedIPs: []string{"192.0.2.2", "192.0.2.1"}},
			},
		}}},
	}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: first}); err != nil {
		t.Fatalf("准备端点顺序测试失败：%v", err)
	}
	*now = now.Add(time.Hour)
	second := []CollectionResult{
		{ResourceType: "ec2", Snapshots: []Snapshot{{
			ExternalID: "i-order",
			Endpoints:  []EndpointSnapshot{{Kind: "private", Address: "10.0.0.1"}, {Kind: "private", Address: "10.0.0.2"}},
		}}},
		{ResourceType: "rds", Snapshots: []Snapshot{{
			ExternalID: "db-order",
			Endpoints: []EndpointSnapshot{
				{Kind: "hostname", Address: "primary.example", Port: 3306, Protocol: "tcp", ResolvedIPs: []string{"192.0.2.1", "192.0.2.2"}},
				{Kind: "hostname", Address: "secondary.example", Port: 3306, Protocol: "tcp"},
			},
		}}},
	}
	job, err := service.Sync(context.Background(), source.ID, "scheduled", collectorStub{results: second})
	if err != nil {
		t.Fatalf("重复端点集合同步失败：%v", err)
	}
	if string(job.Statistics) != `{"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0}}` {
		t.Fatalf("相同端点集合换序不得计为更新：%s", job.Statistics)
	}
}

// TestSyncLegacyEndpointOrderDoesNotCreateUpgradeUpdate 防止升级后首次同步把旧版无序端点数据误报为配置变化。
func TestSyncLegacyEndpointOrderDoesNotCreateUpgradeUpdate(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	old := now.Add(-time.Hour)
	server := Server{
		AssetBase:  AssetBase{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: "i-legacy-order", AssetStatus: AssetStatusActive, FirstSeenAt: old, LastSeenAt: old},
		PrivateIPs: json.RawMessage(`["10.0.0.2","10.0.0.1"]`),
		PublicIPs:  json.RawMessage(`null`),
	}
	database := Database{
		AssetBase: AssetBase{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "rds", ExternalID: "db-legacy-order", AssetStatus: AssetStatusActive, FirstSeenAt: old, LastSeenAt: old},
		Endpoints: json.RawMessage(`[{"kind":"hostname","address":"secondary.example","port":3306,"protocol":"tcp","resolved_ips":null},{"kind":"hostname","address":"primary.example","port":3306,"protocol":"tcp","resolved_ips":["192.0.2.2","192.0.2.1"]}]`),
	}
	if err := db.Create(&server).Error; err != nil {
		t.Fatalf("准备旧版服务器端点失败：%v", err)
	}
	if err := db.Create(&database).Error; err != nil {
		t.Fatalf("准备旧版数据库端点失败：%v", err)
	}
	job, err := service.Sync(context.Background(), source.ID, "scheduled", collectorStub{results: []CollectionResult{
		{ResourceType: "ec2", Snapshots: []Snapshot{{ExternalID: server.ExternalID, Endpoints: []EndpointSnapshot{{Kind: "private", Address: "10.0.0.1"}, {Kind: "private", Address: "10.0.0.2"}}}}},
		{ResourceType: "rds", Snapshots: []Snapshot{{ExternalID: database.ExternalID, Endpoints: []EndpointSnapshot{{Kind: "hostname", Address: "primary.example", Port: 3306, Protocol: "tcp", ResolvedIPs: []string{"192.0.2.1", "192.0.2.2"}}, {Kind: "hostname", Address: "secondary.example", Port: 3306, Protocol: "tcp"}}}}},
	}})
	if err != nil {
		t.Fatalf("同步旧版端点数据失败：%v", err)
	}
	if string(job.Statistics) != `{"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0}}` {
		t.Fatalf("旧版端点数据与相同快照不得产生升级伪更新：%s", job.Statistics)
	}
}

// TestSyncRawAttributesPreserveLargeIntegerChanges 防止 JSON 大整数经浮点转换后丢失精度并漏报变化。
func TestSyncRawAttributesPreserveLargeIntegerChanges(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	snapshot := Snapshot{ResourceType: "ec2", ExternalID: "i-large-number", RawAttributes: []byte(`{"sequence":9007199254740992}`)}
	collector := collectorStub{results: []CollectionResult{{ResourceType: "ec2", Snapshots: []Snapshot{snapshot}}}}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collector); err != nil {
		t.Fatalf("准备大整数属性测试失败：%v", err)
	}
	*now = now.Add(time.Hour)
	snapshot.RawAttributes = []byte(`{"sequence":9007199254740993}`)
	collector.results[0].Snapshots[0] = snapshot
	job, err := service.Sync(context.Background(), source.ID, "scheduled", collector)
	if err != nil {
		t.Fatalf("同步大整数属性变化失败：%v", err)
	}
	if string(job.Statistics) != `{"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":1}}` {
		t.Fatalf("大整数属性变化必须计为更新：%s", job.Statistics)
	}
	var persisted Server
	if err := db.Where("external_id = ?", snapshot.ExternalID).First(&persisted).Error; err != nil || !jsonValuesEqual(persisted.RawAttributes, snapshot.RawAttributes) {
		t.Fatalf("大整数属性变化未正确持久化：raw=%s err=%v", persisted.RawAttributes, err)
	}
}

// TestSyncRoutesAssetsIntoThreeTables 验证服务器、数据库和负载均衡分别持久化专属字段。
func TestSyncRoutesAssetsIntoThreeTables(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	results := []CollectionResult{
		{ResourceType: "ec2", Snapshots: []Snapshot{{ExternalID: "i-1", Endpoints: []EndpointSnapshot{{Kind: "private", Address: "10.0.0.8"}}}}},
		{ResourceType: "rds", Snapshots: []Snapshot{{ExternalID: "db-1", Engine: "mysql", EngineVersion: "8.0", Endpoints: []EndpointSnapshot{{Kind: "hostname", Address: "db.example", Port: 3306}}}}},
		{ResourceType: "elb", Snapshots: []Snapshot{{ExternalID: "lb-1", NetworkType: "internet-facing", Endpoints: []EndpointSnapshot{{Kind: "public", Address: "lb.example", Port: 443}}}}},
	}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: results}); err != nil {
		t.Fatalf("三类资产同步失败：%v", err)
	}
	var server Server
	var database Database
	var loadBalancer LoadBalancer
	if db.First(&server).Error != nil || db.First(&database).Error != nil || db.First(&loadBalancer).Error != nil {
		t.Fatal("三类资产必须分别写入独立表")
	}
	if !strings.Contains(string(server.PrivateIPs), "10.0.0.8") || database.Engine != "mysql" || !strings.Contains(string(database.Endpoints), "db.example") || loadBalancer.NetworkType != "internet-facing" || !strings.Contains(string(loadBalancer.Endpoints), "lb.example") {
		t.Fatal("三类资产的专属字段未完整保存")
	}
}

// TestSyncFailureDoesNotMarkResourcesMissing 验证单类失败和认证失败都不能错误更新失联状态。
func TestSyncFailureDoesNotMarkResourcesMissing(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	active := Server{AssetBase: AssetBase{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: "i-1", AssetStatus: AssetStatusActive, FirstSeenAt: time.Now(), LastSeenAt: time.Now()}, PrivateIPs: json.RawMessage("[]"), PublicIPs: json.RawMessage("[]")}
	_ = db.Create(&active).Error
	old := now.Add(-24 * time.Hour)
	expiredLost := Server{AssetBase: AssetBase{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: "i-expired-lost", AssetStatus: AssetStatusLost, MissingSince: &old, FirstSeenAt: old, LastSeenAt: old}, PrivateIPs: json.RawMessage("[]"), PublicIPs: json.RawMessage("[]")}
	_ = db.Create(&expiredLost).Error
	service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "ec2", Err: errors.New("采集失败")}}})
	_ = db.First(&active, active.ID).Error
	if active.AssetStatus != AssetStatusActive {
		t.Fatal("单类采集失败不得标记失联")
	}
	service.Sync(context.Background(), source.ID, "manual", collectorStub{err: ErrAuthenticationFailed})
	_ = db.First(&active, active.ID).Error
	if active.AssetStatus != AssetStatusActive {
		t.Fatal("认证失败不得标记失联")
	}
	var remaining, deletedAudits int64
	_ = db.Model(&Server{}).Where("external_id = ?", expiredLost.ExternalID).Count(&remaining).Error
	_ = db.Model(&AuditLog{}).Where("action = ? AND resource_id = ?", "resource.deleted", expiredLost.ExternalID).Count(&deletedAudits).Error
	if remaining != 1 || deletedAudits != 0 {
		t.Fatalf("类型失败和认证失败都不得清理过期失联资源：remaining=%d audits=%d", remaining, deletedAudits)
	}
}

// TestPermissionFailureKeepsSafeJobSummary 验证权限错误只保存可操作的安全摘要，不落库云端原文。
func TestPermissionFailureKeepsSafeJobSummary(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	job, _ := service.Sync(context.Background(), source.ID, "manual", collectorStub{err: ErrPermissionDenied})
	var persisted SyncJob
	_ = db.First(&persisted, job.ID).Error
	if persisted.Status != "failed" || persisted.ErrorSummary != "云账号权限不足，请授予资源只读权限" {
		t.Fatalf("权限失败摘要不正确：status=%s summary=%s", persisted.Status, persisted.ErrorSummary)
	}
}

// TestSyncDeletesExpiredLostResourcesAfterRestoringSeenResources 验证删除只发生在成功类型同步中，并归入当前任务统计。
func TestSyncDeletesExpiredLostResourcesAfterRestoringSeenResources(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	old := now.Add(-24 * time.Hour)
	recent := now.Add(-23 * time.Hour)
	for index, missing := range []*time.Time{&old, &recent, &old} {
		_ = db.Create(&Server{AssetBase: AssetBase{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: string(rune('a' + index)), AssetStatus: AssetStatusLost, MissingSince: missing, FirstSeenAt: old, LastSeenAt: old}, PrivateIPs: json.RawMessage("[]"), PublicIPs: json.RawMessage("[]")}).Error
	}
	job, err := service.Sync(context.Background(), source.ID, "scheduled", collectorStub{results: []CollectionResult{{ResourceType: "ec2", Snapshots: []Snapshot{{ExternalID: "c", Name: "重新出现"}}}}})
	if err != nil {
		t.Fatalf("执行带过期失联资源的同步失败：%v", err)
	}
	if string(job.Statistics) != `{"ec2":{"added":0,"deleted":1,"failed":0,"lost":0,"restored":1,"updated":0}}` {
		t.Fatalf("同步任务必须记录恢复和删除数量：%s", job.Statistics)
	}
	var deletedCount int64
	if err := db.Model(&Server{}).Where("external_id = ?", "a").Count(&deletedCount).Error; err != nil || deletedCount != 0 {
		t.Fatalf("持续失联满一天的资源必须被删除：count=%d err=%v", deletedCount, err)
	}
	var recentResource, restoredResource Server
	if err := db.Where("external_id = ?", "b").First(&recentResource).Error; err != nil || recentResource.AssetStatus != AssetStatusLost {
		t.Fatalf("失联不足一天的资源必须保留：resource=%+v err=%v", recentResource, err)
	}
	if err := db.Where("external_id = ?", "c").First(&restoredResource).Error; err != nil || restoredResource.AssetStatus != AssetStatusActive {
		t.Fatalf("本次重新出现的资源必须先恢复而不能误删：resource=%+v err=%v", restoredResource, err)
	}
	var audit AuditLog
	if err := db.Where("action = ? AND resource_id = ?", "resource.deleted", "a").First(&audit).Error; err != nil {
		t.Fatal("物理删除资源前必须保留独立审计记录")
	}
}

// TestSyncRollsBackEarlierTypeDeletionWhenLaterTypeWriteFails 防止失败任务留下无法归属统计的已提交删除。
func TestSyncRollsBackEarlierTypeDeletionWhenLaterTypeWriteFails(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	old := now.Add(-24 * time.Hour)
	resource := Server{AssetBase: AssetBase{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: "i-atomic-delete", AssetStatus: AssetStatusLost, MissingSince: &old, FirstSeenAt: old, LastSeenAt: old}, PrivateIPs: json.RawMessage("[]"), PublicIPs: json.RawMessage("[]")}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("准备过期失联资源失败：%v", err)
	}
	if err := db.Exec(`CREATE TRIGGER fail_database_insert BEFORE INSERT ON resources_databases BEGIN SELECT RAISE(ABORT, '强制数据库写入失败'); END`).Error; err != nil {
		t.Fatalf("准备数据库失败触发器失败：%v", err)
	}
	_, err := service.Sync(context.Background(), source.ID, "scheduled", collectorStub{results: []CollectionResult{
		{ResourceType: "ec2"},
		{ResourceType: "rds", Snapshots: []Snapshot{{ExternalID: "db-fail"}}},
	}})
	if err == nil {
		t.Fatal("后续资源类型写入失败必须结束当前同步")
	}
	var remaining int64
	if err := db.Model(&Server{}).Where("external_id = ?", resource.ExternalID).Count(&remaining).Error; err != nil || remaining != 1 {
		t.Fatalf("后续类型失败必须回滚先前类型的删除：count=%d err=%v", remaining, err)
	}
	var deletedAudits int64
	if err := db.Model(&AuditLog{}).Where("action = ? AND resource_id = ?", "resource.deleted", resource.ExternalID).Count(&deletedAudits).Error; err != nil || deletedAudits != 0 {
		t.Fatalf("回滚的删除不得遗留审计：count=%d err=%v", deletedAudits, err)
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

// TestEnqueueSyncReturnsBeforeCollectorCompletes 验证手工同步先返回排队任务，后台再完成采集。
func TestEnqueueSyncReturnsBeforeCollectorCompletes(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	collector := blockingCollector{entered: make(chan struct{}), release: make(chan struct{})}
	job, err := service.EnqueueSync(context.Background(), source.ID, "manual", collector)
	if err != nil || job.Status != "queued" {
		t.Fatal("异步同步必须立即返回排队任务")
	}
	<-collector.entered
	if _, err := service.EnqueueSync(context.Background(), source.ID, "manual", collectorStub{}); !errors.Is(err, ErrSyncAlreadyRunning) {
		t.Fatal("排队或运行中的同源任务必须拒绝重复入队")
	}
	close(collector.release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var persisted SyncJob
		_ = db.First(&persisted, job.ID).Error
		if persisted.Status == "success" {
			// HTTP 响应持有的排队快照不得被后台工作器并发改写。
			if job.Status != "queued" {
				t.Fatalf("入队返回值必须保持 queued，实际为 %s", job.Status)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("后台同步未完成任务")
}

// TestConnectionDoesNotPersistSnapshots 验证连接测试不会把探测结果写入资源表。
func TestConnectionDoesNotPersistSnapshots(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	result, err := service.TestConnection(context.Background(), source.ProjectID, source.ID, collectorStub{probeResults: []CollectionResult{{ResourceType: "ec2", Snapshots: []Snapshot{{ExternalID: "probe-only"}}}}})
	if err != nil || len(result.ReachableTypes) != 1 || result.ReachableTypes[0] != "ec2" {
		t.Fatal("连接测试必须只返回可达资源类型")
	}
	var count int64
	_ = db.Model(&Server{}).Count(&count).Error
	if count != 0 {
		t.Fatal("连接测试不得持久化探测快照")
	}
}

// TestConnectionUsesLightweightProbe 防止连接测试再次执行完整资源采集并超过页面请求超时。
func TestConnectionUsesLightweightProbe(t *testing.T) {
	service, _, source, _ := newResourceServiceTest(t)
	collectCalls, probeCalls := 0, 0
	collector := collectorStub{
		results:      []CollectionResult{{ResourceType: "ec2", Err: errors.New("完整采集不应执行")}},
		probeResults: []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "rds"}, {ResourceType: "elb"}},
		collectCalls: &collectCalls,
		probeCalls:   &probeCalls,
	}

	result, err := service.TestConnection(context.Background(), source.ProjectID, source.ID, collector)
	if err != nil || len(result.ReachableTypes) != 3 || len(result.FailedTypes) != 0 {
		t.Fatalf("连接测试必须返回轻量探测结果：%v，错误：%v", result, err)
	}
	if collectCalls != 0 || probeCalls != 1 {
		t.Fatalf("连接测试不得执行完整采集，完整采集 %d 次，轻量探测 %d 次", collectCalls, probeCalls)
	}
}

// TestConnectionReturnsEmptyJSONArrays 防止成功响应把空类型集合编码为 null 并导致前端读取 length 失败。
func TestConnectionReturnsEmptyJSONArrays(t *testing.T) {
	service, _, source, _ := newResourceServiceTest(t)
	result, err := service.TestConnection(context.Background(), source.ProjectID, source.ID, collectorStub{probeResults: []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "rds"}, {ResourceType: "elb"}}})
	if err != nil {
		t.Fatalf("连接探测失败：%v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), `"reachable_types":["ec2","rds","elb"]`) || !strings.Contains(string(encoded), `"failed_types":[]`) {
		t.Fatalf("连接结果必须使用 JSON 数组：%s，错误：%v", encoded, err)
	}
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
