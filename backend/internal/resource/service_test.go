// 本文件用真实 SQLite 数据库验证共享同步与生命周期规则，不依赖任何真实云账号。
package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"cmdb/internal/audit"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestSyncFailureConvergence 验证零成功、整体错误和最终事务失败共享调度规则，且不提交资产变化。
func TestSyncFailureConvergence(t *testing.T) {
	for _, scenario := range []string{"认证失败", "网络失败", "全部类型失败", "空结果", "缺少类型", "重复类型", "最终事务失败", "部分成功最终事务失败"} {
		for _, trigger := range []string{"scheduled", "manual"} {
			for _, retry := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/重试%t", scenario, trigger, retry), func(t *testing.T) {
					service, db, source, now := newResourceServiceTest(t)
					last, next, old := now.Add(-2*time.Hour), now.Add(-time.Hour), now.Add(-48*time.Hour)
					if err := db.Model(source).Updates(map[string]any{"last_sync_at": last, "next_sync_at": next}).Error; err != nil {
						t.Fatal("准备自动计划失败")
					}
					active := Server{AssetBase: AssetBase{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: "原有正常实例", AssetStatus: AssetStatusActive, FirstSeenAt: old, LastSeenAt: old}, PrivateIPs: json.RawMessage(`[]`), PublicIPs: json.RawMessage(`[]`)}
					lost := Server{AssetBase: AssetBase{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: "原有失联实例", AssetStatus: AssetStatusLost, MissingSince: &old, FirstSeenAt: old, LastSeenAt: old}, PrivateIPs: json.RawMessage(`[]`), PublicIPs: json.RawMessage(`[]`)}
					if err := db.Create(&active).Error; err != nil {
						t.Fatal("准备正常资产失败")
					}
					if err := db.Create(&lost).Error; err != nil {
						t.Fatal("准备失联资产失败")
					}
					var before []Server
					if err := db.Order("id").Find(&before).Error; err != nil {
						t.Fatal("读取原有资产失败")
					}
					job := &SyncJob{ProjectID: 1, SourceID: source.ID, Status: "queued", Trigger: trigger, StartedAt: *now}
					if retry {
						previous := uint64(99)
						job.PreviousJobID = &previous
					}
					if err := db.Create(job).Error; err != nil {
						t.Fatal("准备排队任务失败")
					}
					collector := collectorStub{rawResults: true}
					switch scenario {
					case "认证失败":
						collector.err = fmt.Errorf("虚构原始凭证错误：%w", ErrCloudAuthentication)
					case "网络失败":
						collector.err = fmt.Errorf("虚构原始网络载荷：%w", ErrCloudNetwork)
					case "全部类型失败":
						collector.results = []CollectionResult{{ResourceType: "ec2", Err: ErrCloudAuthentication}, {ResourceType: "rds", Err: ErrCloudNetwork}, {ResourceType: "alb", Err: errors.New("虚构原始云响应")}}
					case "缺少类型":
						collector.results = []CollectionResult{{ResourceType: "ec2"}}
					case "重复类型":
						collector.results = []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "ec2"}, {ResourceType: "alb"}}
					case "最终事务失败", "部分成功最终事务失败":
						collector.results = []CollectionResult{{ResourceType: "ec2", Snapshots: []Snapshot{{ExternalID: "新增但须回滚的实例"}}}, {ResourceType: "rds"}, {ResourceType: "alb"}}
						if scenario == "部分成功最终事务失败" {
							collector.results[1].Err = ErrCloudNetwork
						}
						// 在资源、统计和计划已写入后拒绝成功审计，验证最终事务整体回滚且失败审计可独立提交。
						if err := db.Exec(`CREATE TRIGGER reject_success_audit BEFORE INSERT ON audit_logs WHEN NEW.action = 'source.synced' AND json_extract(NEW.detail, '$.status') IN ('success', 'partial_success') BEGIN SELECT RAISE(ABORT, '虚构最终事务错误'); END`).Error; err != nil {
							t.Fatal("准备最终事务失败注入失败")
						}
					}
					finished := now.Add(17 * time.Minute)
					calls := 0
					service.now = func() time.Time {
						calls++
						if calls == 1 {
							return *now
						}
						return finished
					}
					_, syncErr := service.executeSync(context.Background(), source.ID, trigger, collector, job)
					if scenario == "网络失败" && syncErr == nil {
						t.Fatal("整体网络失败仍须向内部调用方返回安全错误")
					}
					if syncErr != nil && strings.Contains(syncErr.Error(), "虚构") {
						t.Fatal("同步返回错误不得透传云端原文")
					}
					var persisted SyncJob
					if err := db.First(&persisted, job.ID).Error; err != nil {
						t.Fatal("读取失败终态失败")
					}
					if persisted.Status != "failed" || persisted.FinishedAt == nil || !persisted.FinishedAt.Equal(finished) {
						t.Fatalf("失败必须保存准确终态与完成时间：%+v", persisted)
					}
					if persisted.ErrorSummary == "" || strings.Contains(persisted.ErrorSummary, "虚构") {
						t.Fatal("失败摘要必须为安全中文分类")
					}
					if scenario == "全部类型失败" {
						var statistics map[string]map[string]int
						if json.Unmarshal(persisted.Statistics, &statistics) != nil || len(statistics) != 3 {
							t.Fatal("全部失败应保留完整类型失败统计")
						}
						for _, counts := range statistics {
							if !reflect.DeepEqual(counts, map[string]int{"added": 0, "updated": 0, "restored": 0, "lost": 0, "deleted": 0, "failed": 1}) {
								t.Fatal("全部失败不能保留资产变更统计")
							}
						}
					} else if len(persisted.Statistics) != 0 {
						t.Fatal("整体或最终事务失败不能保留统计")
					}
					var current Source
					if err := db.First(&current, source.ID).Error; err != nil {
						t.Fatal("读取失败后计划失败")
					}
					wantNext := next
					if trigger == "scheduled" && !retry {
						wantNext = finished.Add(time.Hour)
					}
					if current.LastSyncAt == nil || !current.LastSyncAt.Equal(last) || current.NextSyncAt == nil || !current.NextSyncAt.Equal(wantNext) {
						t.Fatalf("失败只允许非重试自动任务按完成时间推进计划：最近=%v 下次=%v 期望=%v", current.LastSyncAt, current.NextSyncAt, wantNext)
					}
					var after []Server
					if err := db.Order("id").Find(&after).Error; err != nil {
						t.Fatal("读取失败后资产失败")
					}
					if !reflect.DeepEqual(before, after) {
						t.Fatal("失败必须保留资产、端点、状态和所有生命周期时间")
					}
					var logs []audit.Log
					if err := db.Find(&logs).Error; err != nil {
						t.Fatal("读取失败审计失败")
					}
					if len(logs) != 1 || logs[0].Action != audit.ActionSourceSynced || !strings.Contains(string(logs[0].Detail), `"status":"failed"`) || strings.Contains(string(logs[0].Detail), "虚构") {
						t.Fatal("失败仅能留下对应的安全失败审计")
					}
				})
			}
		}
	}
}

type blockingCollector struct {
	entered chan struct{}
	release chan struct{}
}

func (c blockingCollector) Collect(context.Context, Source, []byte) ([]CollectionResult, error) {
	close(c.entered)
	<-c.release
	return []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "rds"}, {ResourceType: "alb"}}, nil
}

// Probe 让阻塞采集器满足统一接口；并发同步测试不应进入连接探测路径。
func (c blockingCollector) Probe(context.Context, Source, []byte) ([]CollectionResult, error) {
	return nil, errors.New("并发同步测试不应执行连接探测")
}

type collectorStub struct {
	results []CollectionResult
	// rawResults 专用于空、缺失和重复类型的契约测试；其他用例补齐未关注类型的成功空快照。
	rawResults   bool
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
	if c.err != nil || c.rawResults {
		return c.results, c.err
	}
	results := append([]CollectionResult(nil), c.results...)
	for _, resourceType := range []string{"ec2", "rds", "alb"} {
		found := false
		for _, result := range results {
			if result.ResourceType == resourceType {
				found = true
				break
			}
		}
		if !found {
			results = append(results, CollectionResult{ResourceType: resourceType})
		}
	}
	return results, nil
}

// Probe 返回不含资源快照的轻量探测结果，用于区分连接测试与完整同步。
func (c collectorStub) Probe(context.Context, Source, []byte) ([]CollectionResult, error) {
	if c.probeCalls != nil {
		*c.probeCalls++
	}
	return c.probeResults, c.probeErr
}

// loadBalancerTypesAdapter 仅为具体负载均衡类型回归测试声明 AWS 六类资源范围。
type loadBalancerTypesAdapter struct {
	identityAdapterStub
}

// ResourceTypes 保持具体负载均衡类型独立，不让旧 elb 聚合类型进入同步契约。
func (loadBalancerTypesAdapter) ResourceTypes() []string {
	return []string{"ec2", "rds", "clb", "alb", "nlb", "gwlb"}
}

func newResourceServiceTest(t *testing.T, paths ...string) (*Service, *gorm.DB, *Source, *time.Time) {
	dsn := ":memory:"
	if len(paths) > 0 {
		dsn = paths[0]
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("打开资源测试数据库失败")
	}
	// SQLite 内存库按连接隔离；异步工作器测试固定单连接，避免读到另一份空数据库。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal("获取资源测试数据库连接失败")
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&Source{}, &Server{}, &Database{}, &LoadBalancer{}, &SyncJob{}, &audit.Log{}); err != nil {
		t.Fatal("创建资源测试表失败")
	}
	// 统一审计会读取项目名称快照，资源测试只建立必要的最小关联表。
	if err := db.Exec("CREATE TABLE projects (id integer primary key, name text, status text)").Error; err != nil {
		t.Fatal("创建审计项目关联表失败")
	}
	if err := db.Exec("INSERT INTO projects (id, name, status) VALUES (1, '身份测试项目', 'enabled'), (2, '另一测试项目', 'enabled')").Error; err != nil {
		t.Fatal("准备身份验证项目失败")
	}
	cipher := NewCredentialCipher("resource-service-test-key")
	encrypted, _ := cipher.Encrypt([]byte(`{"token":"example"}`))
	verified := time.Date(2026, 9, 9, 11, 0, 0, 0, time.UTC)
	source := &Source{ProjectID: 1, Provider: ProviderAWS, Name: "测试接入源", Region: "cn-test", EncryptedCredential: encrypted, Enabled: true, SyncIntervalMinutes: 60, CloudAccountID: "123456789012", IdentityVerifiedAt: &verified}
	if err := db.Create(source).Error; err != nil {
		t.Fatal("准备接入源失败")
	}
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	service := NewService(NewRepository(db), cipher, identityTestAdapters(), audit.NewRepository(db))
	service.now = func() time.Time { return now }
	return service, db, source, &now
}

// TestSyncAuditContainsChangesButNeverCredential 验证资源变化可追溯且审计内容不含凭证。
func TestSyncAuditContainsChangesButNeverCredential(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	old := now.Add(-24 * time.Hour)
	recent := now.Add(-time.Hour)
	resources := []Server{
		{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: source.Provider, ResourceType: "ec2", ExternalID: "i-updated", Name: "更新前", AssetStatus: AssetStatusActive, FirstSeenAt: recent, LastSeenAt: recent}, PrivateIPs: json.RawMessage(`[]`), PublicIPs: json.RawMessage(`[]`)},
		{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: source.Provider, ResourceType: "ec2", ExternalID: "i-restored", Name: "恢复实例", AssetStatus: AssetStatusLost, MissingSince: &recent, FirstSeenAt: old, LastSeenAt: old}, PrivateIPs: json.RawMessage(`[]`), PublicIPs: json.RawMessage(`[]`)},
		{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: source.Provider, ResourceType: "ec2", ExternalID: "i-lost", Name: "失联实例", AssetStatus: AssetStatusActive, FirstSeenAt: old, LastSeenAt: old}, PrivateIPs: json.RawMessage(`[]`), PublicIPs: json.RawMessage(`[]`)},
		{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: source.Provider, ResourceType: "ec2", ExternalID: "i-deleted", Name: "删除实例", AssetStatus: AssetStatusLost, MissingSince: &old, FirstSeenAt: old, LastSeenAt: old}, PrivateIPs: json.RawMessage(`[]`), PublicIPs: json.RawMessage(`[]`)},
	}
	if err := db.Create(&resources).Error; err != nil {
		t.Fatalf("准备同步审计资源失败：%v", err)
	}
	snapshots := []Snapshot{
		{ExternalID: "i-created-z", Name: "第二个新增实例"},
		{ExternalID: "i-restored", Name: "恢复实例"},
		{ExternalID: "i-updated", Name: "更新后"},
		{ExternalID: "i-created", Name: "新增实例"},
	}
	_, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "ec2", Snapshots: snapshots}}})
	if err != nil {
		t.Fatalf("同步审计准备失败：%v", err)
	}
	var logs []audit.Log
	if err := db.Order("id ASC").Find(&logs).Error; err != nil {
		t.Fatalf("读取同步审计失败：%v", err)
	}
	if len(logs) != 1 || logs[0].Action != audit.ActionSourceSynced {
		t.Fatalf("一次同步只能写入一条汇总审计：%+v", logs)
	}
	var detail struct {
		JobID      uint64                         `json:"job_id"`
		SourceName string                         `json:"source_name"`
		Provider   string                         `json:"provider"`
		Changes    map[string]map[string][]string `json:"changes"`
	}
	if err := json.Unmarshal(logs[0].Detail, &detail); err != nil {
		t.Fatalf("解析聚合同步审计失败：%v", err)
	}
	wantChanges := map[string]map[string][]string{
		"created":  {"ec2": {"i-created", "i-created-z"}},
		"updated":  {"ec2": {"i-updated"}},
		"restored": {"ec2": {"i-restored"}},
		"lost":     {"ec2": {"i-lost"}},
		"deleted":  {"ec2": {"i-deleted"}},
	}
	if detail.JobID == 0 || detail.SourceName != source.Name || detail.Provider != source.Provider || !reflect.DeepEqual(detail.Changes, wantChanges) {
		t.Fatalf("聚合详情必须完整且使用稳定分组：%+v", detail)
	}
	encoded, _ := json.Marshal(logs)
	if strings.Contains(string(encoded), "example") || strings.Contains(string(encoded), "token") {
		t.Fatal("同步审计不得包含凭证内容或字段")
	}
}

// TestSyncPersistsDatabaseSpecification 验证统一同步链路把 RDS 规格写入数据库资产表并保持容量单位。
func TestSyncPersistsDatabaseSpecification(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	snapshot := Snapshot{
		ResourceType:   "rds",
		ExternalID:     "db-specification",
		InstanceType:   "db.r6g.large",
		VCPU:           2,
		Memory:         16384,
		StorageType:    "gp3",
		StorageSizeGiB: 200,
	}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "rds", Snapshots: []Snapshot{snapshot}}}}); err != nil {
		t.Fatalf("同步 RDS 规格失败：%v", err)
	}
	var persisted Database
	if err := db.Where("external_id = ?", snapshot.ExternalID).First(&persisted).Error; err != nil {
		t.Fatalf("读取 RDS 规格失败：%v", err)
	}
	if persisted.InstanceType != "db.r6g.large" || persisted.VCPU == nil || *persisted.VCPU != 2 || persisted.Memory == nil || *persisted.Memory != 16384 || persisted.StorageType != "gp3" || persisted.StorageSizeGiB == nil || *persisted.StorageSizeGiB != 200 {
		t.Fatalf("RDS 规格没有完整持久化：%+v", persisted)
	}
}

// TestSyncUpdatesDatabaseStorageSpecification 防止 RDS 扩容后存储规格停留在首次采集值。
func TestSyncUpdatesDatabaseStorageSpecification(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	first := Snapshot{ResourceType: "rds", ExternalID: "db-resized", InstanceType: "db.r6g.large", VCPU: 2, Memory: 16384, StorageType: "gp2", StorageSizeGiB: 100}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "rds", Snapshots: []Snapshot{first}}}}); err != nil {
		t.Fatalf("准备 RDS 原规格失败：%v", err)
	}
	*now = now.Add(time.Hour)
	resized := first
	resized.StorageType = "gp3"
	resized.StorageSizeGiB = 200
	job, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "rds", Snapshots: []Snapshot{resized}}}})
	if err != nil {
		t.Fatalf("同步 RDS 扩容规格失败：%v", err)
	}
	if !strings.Contains(string(job.Statistics), `"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":1}`) {
		t.Fatalf("RDS 存储规格变化必须计为真实更新：%s", job.Statistics)
	}
	var persisted Database
	if err := db.Where("external_id = ?", first.ExternalID).First(&persisted).Error; err != nil {
		t.Fatalf("读取扩容后 RDS 失败：%v", err)
	}
	if persisted.StorageType != "gp3" || persisted.StorageSizeGiB == nil || *persisted.StorageSizeGiB != 200 {
		t.Fatalf("RDS 扩容规格没有更新：%+v", persisted)
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
	if string(firstJob.Statistics) != `{"alb":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"ec2":{"added":1,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0}}` {
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
	idempotentDetail := latestSyncAuditDetail(t, db, source.ID)
	if !reflect.DeepEqual(idempotentDetail.Changes, map[string]map[string][]string{
		"created": {}, "updated": {}, "restored": {}, "lost": {}, "deleted": {},
	}) {
		t.Fatalf("幂等刷新不得产生资源变化分组：%+v", idempotentDetail.Changes)
	}
	*now = now.Add(time.Hour)
	service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "ec2"}}})
	_ = db.First(&resources[0], resources[0].ID).Error
	if resources[0].AssetStatus != AssetStatusLost || resources[0].MissingSince == nil {
		t.Fatal("成功采集中的缺失资源必须标记失联")
	}
	lostDetail := latestSyncAuditDetail(t, db, source.ID)
	if !reflect.DeepEqual(lostDetail.Changes, map[string]map[string][]string{
		"created": {}, "updated": {}, "restored": {}, "lost": {"ec2": {"i-1"}}, "deleted": {},
	}) {
		t.Fatalf("失联资源必须进入当前同步汇总：%+v", lostDetail.Changes)
	}
	*now = now.Add(time.Hour)
	service.Sync(context.Background(), source.ID, "manual", collector)
	var restored Server
	_ = db.First(&restored, resources[0].ID).Error
	if restored.AssetStatus != AssetStatusActive || restored.MissingSince != nil {
		t.Fatal("重新出现的资源必须恢复原记录")
	}
	restoredDetail := latestSyncAuditDetail(t, db, source.ID)
	if !reflect.DeepEqual(restoredDetail.Changes, map[string]map[string][]string{
		"created": {}, "updated": {}, "restored": {"ec2": {"i-1"}}, "lost": {}, "deleted": {},
	}) {
		t.Fatalf("恢复资源必须进入当前同步汇总：%+v", restoredDetail.Changes)
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
	if string(job.Statistics) != `{"alb":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0}}` {
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
	detail := latestSyncAuditDetail(t, db, source.ID)
	if !reflect.DeepEqual(detail.Changes, map[string]map[string][]string{
		"created": {}, "updated": {}, "restored": {}, "lost": {}, "deleted": {},
	}) {
		t.Fatalf("相同快照不得产生资源变化审计：%+v", detail.Changes)
	}
}

// TestSyncVolatileRawAttributeOnlyRefreshesSnapshot 防止云端持续变化的观测字段虚增配置更新统计，同时保证原始快照保持最新。
func TestSyncVolatileRawAttributeOnlyRefreshesSnapshot(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	firstSnapshot := Snapshot{
		ResourceType:             "rds",
		ExternalID:               "db-volatile",
		Name:                     "稳定数据库",
		RawAttributes:            []byte(`{"DBInstanceClass":"db.t4g.small","LatestRestorableTime":"2026-09-09T12:00:00Z"}`),
		VolatileRawAttributeKeys: []string{"LatestRestorableTime"},
	}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "rds", Snapshots: []Snapshot{firstSnapshot}}}}); err != nil {
		t.Fatalf("准备易变属性首次同步失败：%v", err)
	}
	var before Database
	if err := db.Where("external_id = ?", firstSnapshot.ExternalID).First(&before).Error; err != nil {
		t.Fatalf("读取首次同步的 RDS 失败：%v", err)
	}

	*now = now.Add(time.Hour)
	secondSnapshot := firstSnapshot
	secondSnapshot.RawAttributes = []byte(`{"DBInstanceClass":"db.t4g.small","LatestRestorableTime":"2026-09-09T13:00:00Z"}`)
	job, err := service.Sync(context.Background(), source.ID, "scheduled", collectorStub{results: []CollectionResult{{ResourceType: "rds", Snapshots: []Snapshot{secondSnapshot}}}})
	if err != nil {
		t.Fatalf("同步易变 RDS 属性失败：%v", err)
	}
	if string(job.Statistics) != `{"alb":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0}}` {
		t.Fatalf("仅易变属性变化不得计为资源更新：%s", job.Statistics)
	}
	var persisted Database
	if err := db.Where("external_id = ?", firstSnapshot.ExternalID).First(&persisted).Error; err != nil {
		t.Fatalf("读取易变属性同步后的 RDS 失败：%v", err)
	}
	if !jsonValuesEqual(persisted.RawAttributes, secondSnapshot.RawAttributes) {
		t.Fatalf("易变属性仍须刷新到最新原始快照：raw=%s", persisted.RawAttributes)
	}
	if !persisted.LastSeenAt.Equal(*now) || !persisted.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("易变属性只能刷新快照和最近发现时间：before=%s updated=%s last_seen=%s", before.UpdatedAt, persisted.UpdatedAt, persisted.LastSeenAt)
	}
	detail := latestSyncAuditDetail(t, db, source.ID)
	if !reflect.DeepEqual(detail.Changes, map[string]map[string][]string{
		"created": {}, "updated": {}, "restored": {}, "lost": {}, "deleted": {},
	}) {
		t.Fatalf("仅易变属性变化不得产生资源变化审计：%+v", detail.Changes)
	}
}

// TestSyncVolatileRawAttributeDoesNotHideConfigurationChange 验证易变观测值同时变化时，稳定原始配置变化仍会计入更新。
func TestSyncVolatileRawAttributeDoesNotHideConfigurationChange(t *testing.T) {
	service, _, source, now := newResourceServiceTest(t)
	firstSnapshot := Snapshot{
		ResourceType:             "rds",
		ExternalID:               "db-config-change",
		RawAttributes:            []byte(`{"DBInstanceClass":"db.t4g.small","LatestRestorableTime":"2026-09-09T12:00:00Z"}`),
		VolatileRawAttributeKeys: []string{"LatestRestorableTime"},
	}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "rds", Snapshots: []Snapshot{firstSnapshot}}}}); err != nil {
		t.Fatalf("准备 RDS 配置变化测试失败：%v", err)
	}

	*now = now.Add(time.Hour)
	secondSnapshot := firstSnapshot
	secondSnapshot.RawAttributes = []byte(`{"DBInstanceClass":"db.r7g.large","LatestRestorableTime":"2026-09-09T13:00:00Z"}`)
	job, err := service.Sync(context.Background(), source.ID, "scheduled", collectorStub{results: []CollectionResult{{ResourceType: "rds", Snapshots: []Snapshot{secondSnapshot}}}})
	if err != nil {
		t.Fatalf("同步 RDS 配置变化失败：%v", err)
	}
	if string(job.Statistics) != `{"alb":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":1}}` {
		t.Fatalf("稳定配置变化必须计为资源更新：%s", job.Statistics)
	}
}

// TestSyncConfigurationChangeAlsoRefreshesVolatileRawAttribute 验证独立业务字段变化时不会遗漏同时推进的最新原始观测值。
func TestSyncConfigurationChangeAlsoRefreshesVolatileRawAttribute(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	firstSnapshot := Snapshot{
		ResourceType:             "rds",
		ExternalID:               "db-region-change",
		Region:                   "cn-north-1",
		RawAttributes:            []byte(`{"LatestRestorableTime":"2026-09-09T12:00:00Z"}`),
		VolatileRawAttributeKeys: []string{"LatestRestorableTime"},
	}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: []CollectionResult{{ResourceType: "rds", Snapshots: []Snapshot{firstSnapshot}}}}); err != nil {
		t.Fatalf("准备 RDS 区域变化测试失败：%v", err)
	}

	*now = now.Add(time.Hour)
	secondSnapshot := firstSnapshot
	secondSnapshot.Region = "cn-northwest-1"
	secondSnapshot.RawAttributes = []byte(`{"LatestRestorableTime":"2026-09-09T13:00:00Z"}`)
	job, err := service.Sync(context.Background(), source.ID, "scheduled", collectorStub{results: []CollectionResult{{ResourceType: "rds", Snapshots: []Snapshot{secondSnapshot}}}})
	if err != nil {
		t.Fatalf("同步 RDS 区域变化失败：%v", err)
	}
	var persisted Database
	if err := db.Where("external_id = ?", firstSnapshot.ExternalID).First(&persisted).Error; err != nil {
		t.Fatalf("读取区域变化后的 RDS 失败：%v", err)
	}
	if string(job.Statistics) != `{"alb":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":1}}` || persisted.Region != secondSnapshot.Region || !jsonValuesEqual(persisted.RawAttributes, secondSnapshot.RawAttributes) {
		t.Fatalf("真实配置变化必须计数并同时保存最新原始快照：statistics=%s region=%s raw=%s", job.Statistics, persisted.Region, persisted.RawAttributes)
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
	if string(job.Statistics) != `{"alb":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":1},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0}}` {
		t.Fatalf("真实业务变化必须计为一次更新：%s", job.Statistics)
	}
	var persisted Server
	if err := db.Where("external_id = ?", snapshot.ExternalID).First(&persisted).Error; err != nil || persisted.Name != "变更后" || !persisted.LastSeenAt.Equal(*now) {
		t.Fatalf("真实业务变化未正确持久化：resource=%+v err=%v", persisted, err)
	}
	detail := latestSyncAuditDetail(t, db, source.ID)
	if !reflect.DeepEqual(detail.Changes, map[string]map[string][]string{
		"created": {}, "updated": {"ec2": {"i-changed"}}, "restored": {}, "lost": {}, "deleted": {},
	}) {
		t.Fatalf("真实业务变化必须进入当前同步更新分组：%+v", detail.Changes)
	}
}

// TestSyncDetectsTypeSpecificFieldChanges 验证 RDS 和负载均衡专属字段参与真实变化判断。
func TestSyncDetectsTypeSpecificFieldChanges(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	first := []CollectionResult{
		{ResourceType: "rds", Snapshots: []Snapshot{{ExternalID: "db-specific", Engine: "mysql", EngineVersion: "8.0"}}},
		{ResourceType: "alb", Snapshots: []Snapshot{{ExternalID: "lb-specific", NetworkType: "internet-facing"}}},
	}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: first}); err != nil {
		t.Fatalf("准备类型专属字段测试失败：%v", err)
	}
	*now = now.Add(time.Hour)
	second := []CollectionResult{
		{ResourceType: "rds", Snapshots: []Snapshot{{ExternalID: "db-specific", Engine: "postgres", EngineVersion: "17"}}},
		{ResourceType: "alb", Snapshots: []Snapshot{{ExternalID: "lb-specific", NetworkType: "internal"}}},
	}
	job, err := service.Sync(context.Background(), source.ID, "scheduled", collectorStub{results: second})
	if err != nil {
		t.Fatalf("同步类型专属字段变化失败：%v", err)
	}
	if string(job.Statistics) != `{"alb":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":1},"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":1}}` {
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
	if string(job.Statistics) != `{"alb":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0}}` {
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
	if string(job.Statistics) != `{"alb":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0}}` {
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
	if string(job.Statistics) != `{"alb":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"ec2":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":1},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0}}` {
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
		{ResourceType: "alb", Snapshots: []Snapshot{{ExternalID: "lb-1", NetworkType: "internet-facing", Endpoints: []EndpointSnapshot{{Kind: "public", Address: "lb.example", Port: 443}}}}},
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

// TestSyncLoadBalancerTypesKeepIndependentLifecycleAndStatistics 验证共表负载均衡仍按具体类型隔离身份、生命周期和统计。
func TestSyncLoadBalancerTypesKeepIndependentLifecycleAndStatistics(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	service.adapters[ProviderAWS] = loadBalancerTypesAdapter{}
	const sharedExternalID = "lb-shared-identity"
	first := []CollectionResult{
		{ResourceType: "ec2"},
		{ResourceType: "rds"},
		{ResourceType: "clb", Snapshots: []Snapshot{{ExternalID: sharedExternalID, Name: "CLB 基线", NetworkType: "classic", RawAttributes: []byte(`{"kind":"clb"}`)}}},
		{ResourceType: "alb", Snapshots: []Snapshot{{ExternalID: sharedExternalID, Name: "ALB 基线", NetworkType: "internet-facing", RawAttributes: []byte(`{"kind":"alb","version":1}`)}}},
		{ResourceType: "nlb", Snapshots: []Snapshot{{ExternalID: sharedExternalID, Name: "NLB 基线", NetworkType: "internet-facing", RawAttributes: []byte(`{"kind":"nlb"}`)}}},
		{ResourceType: "gwlb", Snapshots: []Snapshot{{ExternalID: sharedExternalID, Name: "GWLB 基线", NetworkType: "internal", RawAttributes: []byte(`{"kind":"gwlb"}`)}}},
	}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: first, rawResults: true}); err != nil {
		t.Fatalf("准备具体负载均衡基线失败：%v", err)
	}

	var baseline []LoadBalancer
	if err := db.Where("source_id = ? AND external_id = ?", source.ID, sharedExternalID).Order("resource_type").Find(&baseline).Error; err != nil {
		t.Fatalf("读取具体负载均衡基线失败：%v", err)
	}
	if len(baseline) != 4 {
		t.Fatalf("相同云端 ID 必须按四种具体负载均衡类型创建四条记录，实际 %d 条", len(baseline))
	}
	baselineByType := make(map[string]LoadBalancer, len(baseline))
	for _, value := range baseline {
		baselineByType[value.ResourceType] = value
	}

	*now = now.Add(time.Hour)
	second := []CollectionResult{
		{ResourceType: "ec2"},
		{ResourceType: "rds"},
		{ResourceType: "clb"},
		{ResourceType: "alb", Err: errors.New("ALB 类型采集失败")},
		{ResourceType: "nlb", Snapshots: []Snapshot{{ExternalID: sharedExternalID, Name: "NLB 基线", NetworkType: "internet-facing", RawAttributes: []byte(`{"kind":"nlb"}`)}}},
		{ResourceType: "gwlb", Snapshots: []Snapshot{{ExternalID: sharedExternalID, Name: "GWLB 基线", NetworkType: "internet-facing", RawAttributes: []byte(`{"kind":"gwlb"}`)}}},
	}
	job, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: second, rawResults: true})
	if err != nil || job.Status != "partial_success" {
		t.Fatalf("单个具体负载均衡类型失败时任务必须部分成功：job=%+v err=%v", job, err)
	}

	currentByType := make(map[string]LoadBalancer, 4)
	for _, resourceType := range []string{"clb", "alb", "nlb", "gwlb"} {
		var current LoadBalancer
		if err := db.Where("source_id = ? AND resource_type = ? AND external_id = ?", source.ID, resourceType, sharedExternalID).First(&current).Error; err != nil {
			t.Fatalf("读取 %s 负载均衡失败：%v", resourceType, err)
		}
		currentByType[resourceType] = current
	}
	clb := currentByType["clb"]
	if clb.AssetStatus != AssetStatusLost || clb.MissingSince == nil {
		t.Fatalf("CLB 成功空采集必须只将 CLB 标记失联：%+v", clb)
	}
	alb, albBaseline := currentByType["alb"], baselineByType["alb"]
	if alb.AssetStatus != AssetStatusActive || alb.MissingSince != nil || !alb.LastSeenAt.Equal(albBaseline.LastSeenAt) || alb.Name != albBaseline.Name || alb.NetworkType != albBaseline.NetworkType || !jsonValuesEqual(alb.RawAttributes, albBaseline.RawAttributes) {
		t.Fatalf("ALB 类型失败必须完整保留原状态、最近发现时间和快照：before=%+v after=%+v", albBaseline, alb)
	}
	nlb := currentByType["nlb"]
	if nlb.AssetStatus != AssetStatusActive || nlb.MissingSince != nil || !nlb.LastSeenAt.Equal(*now) {
		t.Fatalf("NLB 成功重见必须保持正常并刷新最近发现时间：%+v", nlb)
	}
	gwlb := currentByType["gwlb"]
	if gwlb.AssetStatus != AssetStatusActive || gwlb.MissingSince != nil || gwlb.NetworkType != "internet-facing" {
		t.Fatalf("GWLB 真实业务字段变化必须独立更新且保持正常：%+v", gwlb)
	}

	var statistics map[string]map[string]int
	if err := json.Unmarshal(job.Statistics, &statistics); err != nil {
		t.Fatalf("解析六类型同步统计失败：%v", err)
	}
	wantStatistics := map[string]map[string]int{
		"ec2":  {"added": 0, "updated": 0, "restored": 0, "lost": 0, "deleted": 0, "failed": 0},
		"rds":  {"added": 0, "updated": 0, "restored": 0, "lost": 0, "deleted": 0, "failed": 0},
		"clb":  {"added": 0, "updated": 0, "restored": 0, "lost": 1, "deleted": 0, "failed": 0},
		"alb":  {"added": 0, "updated": 0, "restored": 0, "lost": 0, "deleted": 0, "failed": 1},
		"nlb":  {"added": 0, "updated": 0, "restored": 0, "lost": 0, "deleted": 0, "failed": 0},
		"gwlb": {"added": 0, "updated": 1, "restored": 0, "lost": 0, "deleted": 0, "failed": 0},
	}
	if !reflect.DeepEqual(statistics, wantStatistics) {
		t.Fatalf("六类型统计必须完全隔离且不得包含 elb：got=%v want=%v", statistics, wantStatistics)
	}
}

// TestSyncPersistsAndReturnsServerHardwareDetails 防止服务器规格或磁盘明细在统一持久化与查询链路中丢失。
func TestSyncPersistsAndReturnsServerHardwareDetails(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	snapshotDisks := []ServerDisk{
		{ID: "vol-root", Kind: "system", Type: "gp3", SizeGiB: 100, Device: "/dev/sda1", Encrypted: true},
		{ID: "vol-data", Kind: "data", Type: "gp3", SizeGiB: 400, Device: "/dev/sdf", Encrypted: true},
	}
	wantDisks := []ServerDisk{snapshotDisks[1], snapshotDisks[0]}
	results := []CollectionResult{
		{ResourceType: "ec2", Snapshots: []Snapshot{{ExternalID: "i-hardware", InstanceType: "c6a.xlarge", VCPU: 4, Memory: 8192, Disks: snapshotDisks}}},
		{ResourceType: "rds"},
		{ResourceType: "alb"},
	}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: results}); err != nil {
		t.Fatalf("同步服务器规格失败：%v", err)
	}

	var persisted Server
	if err := db.Where("external_id = ?", "i-hardware").First(&persisted).Error; err != nil {
		t.Fatalf("读取服务器规格失败：%v", err)
	}
	var persistedDisks []ServerDisk
	if json.Unmarshal(persisted.Disks, &persistedDisks) != nil || persisted.InstanceType != "c6a.xlarge" || persisted.VCPU != 4 || persisted.Memory != 8192 || !reflect.DeepEqual(persistedDisks, wantDisks) {
		t.Fatalf("服务器规格未完整持久化：%+v disks=%s", persisted, persisted.Disks)
	}

	resources, total, err := service.ListResources(context.Background(), source.ProjectID, ResourceListQuery{Providers: []string{ProviderAWS}, ResourceTypes: []string{"ec2"}, Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(resources) != 1 {
		t.Fatalf("查询服务器规格失败：total=%d values=%+v err=%v", total, resources, err)
	}
	got := resources[0]
	if got.InstanceType != "c6a.xlarge" || got.VCPU != 4 || got.Memory != 8192 || !reflect.DeepEqual(got.Disks, wantDisks) {
		t.Fatalf("统一资源接口丢失服务器规格：%+v", got)
	}
}

// TestListResourcesSupportsServerSearchFiltersAndStableSorting 验证服务器列表以一次项目级查询完成搜索、筛选、排序和分页。
func TestListResourcesSupportsServerSearchFiltersAndStableSorting(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	secondSource := &Source{ProjectID: source.ProjectID, Provider: ProviderAliyun, Name: "华东生产账号", Region: "cn-shanghai", EncryptedCredential: "cipher", Enabled: true, SyncIntervalMinutes: 60, CloudAccountID: "aliyun-account", IdentityVerifiedAt: now}
	if err := db.Create(secondSource).Error; err != nil {
		t.Fatalf("准备第二接入源失败：%v", err)
	}
	servers := []Server{
		{AssetBase: AssetBase{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: "i-z", Name: "A-订单节点", Region: "ap-southeast-1", CloudStatus: "running", AssetStatus: AssetStatusActive, LastSeenAt: *now}, InstanceType: "c6a.large", VCPU: 2, Memory: 4096, PrivateIPs: json.RawMessage(`["10.0.0.8"]`), PublicIPs: json.RawMessage(`["8.8.8.8"]`), Disks: json.RawMessage(`[{"id":"vol-z","kind":"system","type":"gp3","size_gib":140,"device":"/dev/sda","encrypted":true}]`)},
		{AssetBase: AssetBase{ProjectID: 1, SourceID: secondSource.ID, Provider: ProviderAliyun, ResourceType: "ecs", ExternalID: "i-a", Name: "B-支付节点", Region: "cn-shanghai", CloudStatus: "Running", AssetStatus: AssetStatusActive, LastSeenAt: now.Add(-time.Minute)}, InstanceType: "ecs.g7.large", VCPU: 2, Memory: 8192, PrivateIPs: json.RawMessage(`["10.0.0.9"]`), PublicIPs: json.RawMessage(`[]`), Disks: json.RawMessage(`[]`)},
	}
	if err := db.Create(&servers).Error; err != nil {
		t.Fatalf("准备服务器列表失败：%v", err)
	}

	values, total, err := service.ListResources(context.Background(), 1, ResourceListQuery{
		Providers: []string{ProviderAWS, ProviderAliyun}, ResourceTypes: []string{"ecs", "ec2"}, Keyword: "8.8.8", CloudStatus: "running", SortBy: "name", SortOrder: "asc", Page: 1, PageSize: 20,
	})
	if err != nil || total != 1 || len(values) != 1 {
		t.Fatalf("服务器组合查询失败：total=%d values=%+v err=%v", total, values, err)
	}
	if values[0].ExternalID != "i-z" || values[0].SourceName != source.Name {
		t.Fatalf("搜索结果必须包含脱敏接入源名称：%+v", values[0])
	}

	values, total, err = service.ListResources(context.Background(), 1, ResourceListQuery{ResourceTypes: []string{"ecs", "ec2"}, SortBy: "name", SortOrder: "asc", Page: 1, PageSize: 1})
	if err != nil || total != 2 || len(values) != 1 || values[0].Name != "A-订单节点" {
		t.Fatalf("名称升序与服务端分页不正确：total=%d values=%+v err=%v", total, values, err)
	}
	values, _, err = service.ListResources(context.Background(), 1, ResourceListQuery{ResourceTypes: []string{"ecs", "ec2"}, SortBy: "disk_size", SortOrder: "desc", Page: 1, PageSize: 1})
	if err != nil || len(values) != 1 || values[0].ExternalID != "i-z" {
		t.Fatalf("磁盘总容量必须由数据库排序后再分页：values=%+v err=%v", values, err)
	}
	values, _, err = service.ListResources(context.Background(), 1, ResourceListQuery{ResourceTypes: []string{"ecs", "ec2"}, SortBy: "vcpu", SortOrder: "desc", Page: 1, PageSize: 2})
	if err != nil || len(values) != 2 || values[0].SourceID != source.ID || values[1].SourceID != secondSource.ID {
		t.Fatalf("主排序同值时资源身份次级排序必须固定升序：values=%+v err=%v", values, err)
	}
	values, total, err = service.ListResources(context.Background(), 1, ResourceListQuery{ResourceTypes: []string{"ecs", "ec2"}, Keyword: "ECS.G7", SortBy: "name", SortOrder: "asc", Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(values) != 1 || values[0].ExternalID != "i-a" {
		t.Fatalf("服务器实例类型必须参与不区分大小写的全量搜索：total=%d values=%+v err=%v", total, values, err)
	}
}

// TestListResourcesFiltersDatabaseEngine 验证数据库引擎精确筛选参与完整结果集的搜索、计数和分页。
func TestListResourcesFiltersDatabaseEngine(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	databases := []Database{
		{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "rds", ExternalID: "db-postgresql", Name: "订单数据库", AssetStatus: AssetStatusActive, LastSeenAt: *now}, Engine: "PostgreSQL", EngineVersion: "16.3", InstanceType: "db.r6g.large", Endpoints: json.RawMessage(`[{"kind":"hostname","address":"orders.example.com","port":5432,"protocol":"tcp","resolved_ips":[]}]`)},
		{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "rds", ExternalID: "db-mysql", Name: "用户数据库", AssetStatus: AssetStatusActive, LastSeenAt: *now}, Engine: "MySQL", EngineVersion: "8.0.35", InstanceType: "db.m6g.large", Endpoints: json.RawMessage(`[{"kind":"hostname","address":"users.example.com","port":3306,"protocol":"tcp","resolved_ips":[]}]`)},
	}
	if err := db.Create(&databases).Error; err != nil {
		t.Fatalf("准备数据库列表失败：%v", err)
	}
	server := Server{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: "i-orders", Name: "订单节点", AssetStatus: AssetStatusActive, LastSeenAt: *now}, InstanceType: "db.r6g.large", PrivateIPs: json.RawMessage(`[]`), PublicIPs: json.RawMessage(`[]`), Disks: json.RawMessage(`[]`)}
	if err := db.Create(&server).Error; err != nil {
		t.Fatalf("准备非数据库资产失败：%v", err)
	}

	values, total, err := service.ListResources(context.Background(), source.ProjectID, ResourceListQuery{
		ResourceTypes: []string{"rds"}, Engine: "postgresql", Keyword: "orders.example.com:5432", SortBy: "name", SortOrder: "asc", Page: 1, PageSize: 20,
	})
	if err != nil || total != 1 || len(values) != 1 || values[0].ExternalID != "db-postgresql" || values[0].Engine != "PostgreSQL" || values[0].EngineVersion != "16.3" {
		t.Fatalf("数据库引擎和访问地址组合筛选不正确：total=%d values=%+v err=%v", total, values, err)
	}
	values, total, err = service.ListResources(context.Background(), source.ProjectID, ResourceListQuery{Engine: "POSTGRESQL", SortBy: "name", SortOrder: "asc", Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(values) != 1 || values[0].ExternalID != "db-postgresql" {
		t.Fatalf("引擎筛选不得让非数据库资产进入结果：total=%d values=%+v err=%v", total, values, err)
	}
	values, total, err = service.ListResources(context.Background(), source.ProjectID, ResourceListQuery{ResourceTypes: []string{"ec2"}, Engine: "POSTGRESQL", SortBy: "name", SortOrder: "asc", Page: 1, PageSize: 20})
	if err != nil || total != 0 || len(values) != 0 {
		t.Fatalf("数据库引擎与服务器类型组合必须返回空结果：total=%d values=%+v err=%v", total, values, err)
	}
	values, total, err = service.ListResources(context.Background(), source.ProjectID, ResourceListQuery{
		ResourceTypes: []string{"rds"}, Keyword: "R6G.LARGE", SortBy: "name", SortOrder: "asc", Page: 1, PageSize: 20,
	})
	if err != nil || total != 1 || len(values) != 1 || values[0].ExternalID != "db-postgresql" {
		t.Fatalf("数据库实例类型必须参与不区分大小写的全量搜索：total=%d values=%+v err=%v", total, values, err)
	}
}

// TestListResourcesFiltersLoadBalancerNetworkType 验证负载均衡网络类型和完整访问地址参与服务端筛选、搜索与分页。
func TestListResourcesFiltersLoadBalancerNetworkType(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	loadBalancers := []LoadBalancer{
		{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "alb", ExternalID: "alb-internal", Name: "内部入口", Region: "ap-southeast-1", CloudStatus: "active", AssetStatus: AssetStatusActive, LastSeenAt: *now}, NetworkType: "internal", Endpoints: json.RawMessage(`[{"kind":"hostname","address":"internal.example.com","port":443,"protocol":"https","resolved_ips":["10.0.0.8"]}]`)},
		{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "nlb", ExternalID: "nlb-public", Name: "公网入口", Region: "ap-southeast-1", CloudStatus: "provisioning", AssetStatus: AssetStatusActive, LastSeenAt: now.Add(-time.Minute)}, NetworkType: "internet-facing", Endpoints: json.RawMessage(`[{"kind":"hostname","address":"public.example.com","port":80,"protocol":"tcp","resolved_ips":[]}]`)},
		{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: ProviderAliyun, ResourceType: "alb", ExternalID: "alb-create-failed", Name: "创建失败入口", Region: "cn-shanghai", CloudStatus: "CreateFailed", AssetStatus: AssetStatusActive, LastSeenAt: now.Add(-2 * time.Minute)}, NetworkType: "internet", Endpoints: json.RawMessage(`[]`)},
	}
	if err := db.Create(&loadBalancers).Error; err != nil {
		t.Fatalf("准备负载均衡列表失败：%v", err)
	}
	server := Server{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: "i-internal", Name: "内部节点", AssetStatus: AssetStatusActive, LastSeenAt: *now}, PrivateIPs: json.RawMessage(`[]`), PublicIPs: json.RawMessage(`[]`), Disks: json.RawMessage(`[]`)}
	if err := db.Create(&server).Error; err != nil {
		t.Fatalf("准备非负载均衡资产失败：%v", err)
	}

	values, total, err := service.ListResources(context.Background(), source.ProjectID, ResourceListQuery{
		ResourceTypes: []string{"slb", "clb", "alb", "nlb", "gwlb"}, NetworkType: "private", CloudStatus: "running", Keyword: "internal.example.com:443", SortBy: "name", SortOrder: "asc", Page: 1, PageSize: 20,
	})
	if err != nil || total != 1 || len(values) != 1 || values[0].ExternalID != "alb-internal" || values[0].NetworkType != "internal" {
		t.Fatalf("负载均衡网络类型与完整访问地址组合查询不正确：total=%d values=%+v err=%v", total, values, err)
	}
	values, total, err = service.ListResources(context.Background(), source.ProjectID, ResourceListQuery{ResourceTypes: []string{"ec2"}, NetworkType: "internal", SortBy: "name", SortOrder: "asc", Page: 1, PageSize: 20})
	if err != nil || total != 0 || len(values) != 0 {
		t.Fatalf("网络类型与服务器类型组合必须返回空结果：total=%d values=%+v err=%v", total, values, err)
	}
	values, total, err = service.ListResources(context.Background(), source.ProjectID, ResourceListQuery{ResourceTypes: []string{"alb"}, CloudStatus: "failed", SortBy: "name", SortOrder: "asc", Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(values) != 1 || values[0].ExternalID != "alb-create-failed" {
		t.Fatalf("失败筛选必须覆盖阿里云 CreateFailed：total=%d values=%+v err=%v", total, values, err)
	}
}

// TestListLoadBalancerResourcesPaginatesAfterEndpointSearch 验证完整地址搜索后再计数分页，并以资源身份稳定排列同名资产。
func TestListLoadBalancerResourcesPaginatesAfterEndpointSearch(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	values := make([]LoadBalancer, 21)
	for index := range values {
		values[index] = LoadBalancer{
			AssetBase:   AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "alb", ExternalID: fmt.Sprintf("alb-%02d", index), Name: "共享入口", CloudStatus: "active", AssetStatus: AssetStatusActive, LastSeenAt: *now},
			NetworkType: "internet-facing",
			Endpoints:   json.RawMessage(fmt.Sprintf(`[{"kind":"hostname","address":"edge-%02d.example.com","port":443,"protocol":"https","resolved_ips":[]}]`, index)),
		}
	}
	if err := db.Create(&values).Error; err != nil {
		t.Fatalf("准备负载均衡分页数据失败：%v", err)
	}
	statements := make([]string, 0)
	const callbackName = "test:capture-load-balancer-list-sql"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		statements = append(statements, strings.ToLower(tx.Statement.SQL.String()))
	}); err != nil {
		t.Fatalf("注册负载均衡查询观察器失败：%v", err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(callbackName, func(tx *gorm.DB) {
		statements = append(statements, strings.ToLower(tx.Statement.SQL.String()))
	}); err != nil {
		t.Fatalf("注册负载均衡分页观察器失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName); _ = db.Callback().Row().Remove(callbackName) })

	page, total, err := service.ListAllResources(context.Background(), ResourceListQuery{ResourceTypes: []string{"alb"}, Keyword: ".example.com:443", SortBy: "name", SortOrder: "asc", Page: 2, PageSize: 10})
	if err != nil || total != 21 || len(page) != 10 || page[0].ExternalID != "alb-10" || page[9].ExternalID != "alb-19" {
		t.Fatalf("负载均衡搜索后分页或稳定排序不正确：total=%d page=%+v err=%v", total, page, err)
	}
	listSQL := strings.Join(statements, "\n")
	if !strings.Contains(listSQL, "limit 10") || strings.Contains(listSQL, "select * from `resources_load_balancers`") || strings.Contains(listSQL, "raw_attributes") {
		t.Fatalf("负载均衡列表必须数据库分页且只读取公开投影，实际 SQL：%s", listSQL)
	}
}

// TestListResourcesRejectsUnsupportedQuery 验证未知筛选值和排序字段不会下沉成数据库故障。
func TestListResourcesRejectsUnsupportedQuery(t *testing.T) {
	service, _, source, _ := newResourceServiceTest(t)
	for _, query := range []ResourceListQuery{
		{Providers: []string{"unknown"}},
		{ResourceTypes: []string{"unknown"}},
		{AssetStatus: "deleted"},
		{SortBy: "raw_attributes"},
		{SortBy: "name", SortOrder: "sideways"},
		{Page: int(^uint(0) >> 1), PageSize: 100},
	} {
		if _, _, err := service.ListResources(context.Background(), source.ProjectID, query); !errors.Is(err, ErrInvalidResourceQuery) {
			t.Fatalf("非法资源查询应返回稳定参数错误：query=%+v err=%v", query, err)
		}
	}
}

// TestListAllResourcesSearchesBeforeGlobalPagination 验证系统管理员“所有项目”列表由后端统一搜索、排序和分页。
func TestListAllResourcesSearchesBeforeGlobalPagination(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	otherSource := &Source{ProjectID: 2, Provider: ProviderAliyun, Name: "另一项目来源", Region: "cn-shanghai", EncryptedCredential: "cipher", Enabled: true, SyncIntervalMinutes: 60, CloudAccountID: "other-account", IdentityVerifiedAt: now}
	if err := db.Create(otherSource).Error; err != nil {
		t.Fatalf("准备另一项目来源失败：%v", err)
	}
	servers := []Server{
		{AssetBase: AssetBase{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: "i-project-a", Name: "A 节点", AssetStatus: AssetStatusActive}, PrivateIPs: json.RawMessage(`["10.0.0.1"]`), PublicIPs: json.RawMessage(`[]`), Disks: json.RawMessage(`[]`)},
		{AssetBase: AssetBase{ProjectID: 2, SourceID: otherSource.ID, Provider: ProviderAliyun, ResourceType: "ecs", ExternalID: "i-project-b", Name: "B 节点", AssetStatus: AssetStatusActive}, PrivateIPs: json.RawMessage(`["10.0.0.2"]`), PublicIPs: json.RawMessage(`[]`), Disks: json.RawMessage(`[]`)},
	}
	if err := db.Create(&servers).Error; err != nil {
		t.Fatalf("准备跨项目资源失败：%v", err)
	}
	values, total, err := service.ListAllResources(context.Background(), ResourceListQuery{ResourceTypes: []string{"ecs", "ec2"}, Keyword: "节点", SortBy: "name", SortOrder: "asc", Page: 2, PageSize: 1})
	if err != nil || total != 2 || len(values) != 1 || values[0].ProjectName != "另一测试项目" {
		t.Fatalf("跨项目搜索必须先于统一分页并返回项目名称：total=%d values=%+v err=%v", total, values, err)
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
	partialDetail := latestSyncAuditDetail(t, db, source.ID)
	if !reflect.DeepEqual(partialDetail.Changes, map[string]map[string][]string{
		"created": {}, "updated": {}, "restored": {}, "lost": {}, "deleted": {},
	}) {
		t.Fatalf("单类失败同步不得记录资源变化：%+v", partialDetail.Changes)
	}
	service.Sync(context.Background(), source.ID, "manual", collectorStub{err: ErrAuthenticationFailed})
	_ = db.First(&active, active.ID).Error
	if active.AssetStatus != AssetStatusActive {
		t.Fatal("认证失败不得标记失联")
	}
	failedDetail := latestSyncAuditDetail(t, db, source.ID)
	if !reflect.DeepEqual(failedDetail.Changes, map[string]map[string][]string{
		"created": {}, "updated": {}, "restored": {}, "lost": {}, "deleted": {},
	}) {
		t.Fatalf("认证失败同步的五个变化分组必须为空：%+v", failedDetail.Changes)
	}
	var remaining int64
	_ = db.Model(&Server{}).Where("external_id = ?", expiredLost.ExternalID).Count(&remaining).Error
	if remaining != 1 {
		t.Fatalf("类型失败和认证失败都不得清理过期失联资源：remaining=%d", remaining)
	}
}

// TestSyncServerPermissionFailureStillCommitsOtherTypes 验证服务器补充权限失败时保留旧快照并提交其他成功类型。
func TestSyncServerPermissionFailureStillCommitsOtherTypes(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	firstSeen := *now
	first := []CollectionResult{
		{ResourceType: "ec2", Snapshots: []Snapshot{{ExternalID: "i-partial", InstanceType: "c6a.xlarge", VCPU: 4, Memory: 8192, Disks: []ServerDisk{{ID: "vol-1", Kind: "system", Type: "gp3", SizeGiB: 100, Device: "/dev/sda1", Encrypted: true}}}}},
		{ResourceType: "rds", Snapshots: []Snapshot{{ExternalID: "db-partial", Name: "变更前", Engine: "postgres", EngineVersion: "16"}}},
	}
	if _, err := service.Sync(context.Background(), source.ID, "manual", collectorStub{results: first}); err != nil {
		t.Fatalf("准备部分成功测试数据失败：%v", err)
	}
	*now = now.Add(time.Hour)
	second := []CollectionResult{
		{ResourceType: "ec2", Err: ErrCloudPermission},
		{ResourceType: "rds", Snapshots: []Snapshot{{ExternalID: "db-partial", Name: "变更后", Engine: "postgres", EngineVersion: "17"}}},
	}
	job, err := service.Sync(context.Background(), source.ID, "scheduled", collectorStub{results: second})
	if err != nil || job.Status != "partial_success" {
		t.Fatalf("服务器权限失败且其他类型成功时必须部分成功：job=%+v err=%v", job, err)
	}

	var server Server
	if err := db.Where("external_id = ?", "i-partial").First(&server).Error; err != nil {
		t.Fatalf("读取服务器旧快照失败：%v", err)
	}
	if server.AssetStatus != AssetStatusActive || !server.LastSeenAt.Equal(firstSeen) || server.InstanceType != "c6a.xlarge" || server.VCPU != 4 || server.Memory != 8192 {
		t.Fatalf("失败的服务器类型不得改变旧快照：%+v", server)
	}
	var database Database
	if err := db.Where("external_id = ?", "db-partial").First(&database).Error; err != nil || database.Name != "变更后" || database.EngineVersion != "17" || !database.LastSeenAt.Equal(*now) {
		t.Fatalf("成功的数据库类型必须在部分成功事务中提交：database=%+v err=%v", database, err)
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
	var auditEntry audit.Log
	if err := db.Where("action = ? AND resource_id = ?", audit.ActionSourceSynced, source.ID).Order("id DESC").First(&auditEntry).Error; err != nil {
		t.Fatalf("失败同步也必须进入审计：%v", err)
	}
	if !strings.Contains(string(auditEntry.Detail), `"status":"failed"`) || strings.Contains(string(auditEntry.Detail), "example") {
		t.Fatalf("失败同步审计必须只有安全状态摘要：%s", auditEntry.Detail)
	}
	assertSourceAuditName(t, []audit.Log{auditEntry}, audit.ActionSourceSynced, source.Name)
}

// TestSyncFinalStateRollsBackWhenAuditWriteFails 防止同步最终状态和调度时间先于汇总审计提交。
func TestSyncFinalStateRollsBackWhenAuditWriteFails(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		trigger   string
		collector collectorStub
	}{
		{name: "手工成功同步", trigger: "manual", collector: collectorStub{}},
		{name: "手工失败同步", trigger: "manual", collector: collectorStub{err: ErrAuthenticationFailed}},
		{name: "自动成功同步", trigger: "scheduled", collector: collectorStub{}},
		{name: "自动失败同步", trigger: "scheduled", collector: collectorStub{err: ErrAuthenticationFailed}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatalf("创建同步事务测试数据库失败：%v", err)
			}
			if err := db.AutoMigrate(&Source{}, &SyncJob{}, &Server{}, &Database{}, &LoadBalancer{}); err != nil {
				t.Fatalf("创建同步测试表失败：%v", err)
			}
			if err := db.Exec("CREATE TABLE projects (id integer primary key, name text)").Error; err != nil {
				t.Fatalf("创建项目测试表失败：%v", err)
			}
			if err := db.Exec("INSERT INTO projects (id, name) VALUES (?, ?)", 1, "同步项目").Error; err != nil {
				t.Fatalf("准备项目快照失败：%v", err)
			}
			cipher := NewCredentialCipher("sync-atomic-key")
			encrypted, encryptErr := cipher.Encrypt([]byte(`{"token":"example"}`))
			if encryptErr != nil {
				t.Fatalf("准备同步凭证失败：%v", encryptErr)
			}
			verified := time.Now()
			source := &Source{ProjectID: 1, Provider: ProviderAWS, Name: "同步接入源", EncryptedCredential: encrypted, Enabled: true, SyncIntervalMinutes: 60, CloudAccountID: "123456789012", IdentityVerifiedAt: &verified}
			if err := db.Create(source).Error; err != nil {
				t.Fatalf("准备同步接入源失败：%v", err)
			}
			service := NewService(NewRepository(db), cipher, identityTestAdapters(), audit.NewRepository(db))

			if _, err := service.Sync(context.Background(), source.ID, testCase.trigger, testCase.collector); err == nil {
				t.Fatal("汇总审计写入失败时同步必须返回错误")
			}
			var persistedJob SyncJob
			if err := db.Order("id DESC").First(&persistedJob).Error; err != nil {
				t.Fatalf("读取同步任务回滚结果失败：%v", err)
			}
			if persistedJob.Status != "running" || persistedJob.FinishedAt != nil {
				t.Fatalf("汇总审计失败时不得提交同步最终状态：%+v", persistedJob)
			}
			var persistedSource Source
			if err := db.First(&persistedSource, source.ID).Error; err != nil {
				t.Fatalf("读取接入源调度回滚结果失败：%v", err)
			}
			if persistedSource.LastSyncAt != nil || persistedSource.NextSyncAt != nil {
				t.Fatalf("汇总审计失败时不得提交新的调度时间：%+v", persistedSource)
			}
		})
	}
}

// TestConnectionFailureWritesSafeAudit 验证失败连接测试可追溯，但审计不保存云端底层错误或凭证。
func TestConnectionFailureWritesSafeAudit(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	if _, err := service.TestConnection(context.Background(), source.ProjectID, source.ID, collectorStub{probeErr: ErrAuthenticationFailed}); !errors.Is(err, ErrAuthenticationFailed) {
		t.Fatalf("连接测试必须保留认证失败业务语义：%v", err)
	}
	var auditEntry audit.Log
	if err := db.Where("action = ? AND resource_id = ?", audit.ActionSourceConnectionTested, source.ID).Order("id DESC").First(&auditEntry).Error; err != nil {
		t.Fatalf("失败连接测试必须进入审计：%v", err)
	}
	if !strings.Contains(string(auditEntry.Detail), `"status":"failed"`) || strings.Contains(string(auditEntry.Detail), "example") {
		t.Fatalf("失败连接审计必须只有安全状态摘要：%s", auditEntry.Detail)
	}
	assertSourceAuditName(t, []audit.Log{auditEntry}, audit.ActionSourceConnectionTested, source.Name)
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
	if string(job.Statistics) != `{"alb":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0},"ec2":{"added":0,"deleted":1,"failed":0,"lost":0,"restored":1,"updated":0},"rds":{"added":0,"deleted":0,"failed":0,"lost":0,"restored":0,"updated":0}}` {
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
	detail := latestSyncAuditDetail(t, db, source.ID)
	if !reflect.DeepEqual(detail.Changes, map[string]map[string][]string{
		"created": {}, "updated": {}, "restored": {"ec2": {"c"}}, "lost": {}, "deleted": {"ec2": {"a"}},
	}) {
		t.Fatalf("同一次同步的恢复和删除必须进入唯一汇总记录：%+v", detail.Changes)
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
	detail := latestSyncAuditDetail(t, db, source.ID)
	if !reflect.DeepEqual(detail.Changes, map[string]map[string][]string{
		"created": {}, "updated": {}, "restored": {}, "lost": {}, "deleted": {},
	}) {
		t.Fatalf("失败审计不得包含已回滚事务的删除 ID：%+v", detail.Changes)
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
	var entries []audit.Log
	if err := db.Where("action = ?", audit.ActionSourceConnectionTested).Find(&entries).Error; err != nil {
		t.Fatalf("读取成功连接审计失败：%v", err)
	}
	assertSourceAuditName(t, entries, audit.ActionSourceConnectionTested, source.Name)
}

// assertSourceAuditName 直接检查已持久化详情，确保所有接入源审计使用统一公开快照键。
func assertSourceAuditName(t *testing.T, entries []audit.Log, action, want string) {
	t.Helper()
	for _, entry := range entries {
		if entry.Action != action {
			continue
		}
		var detail map[string]any
		if err := json.Unmarshal(entry.Detail, &detail); err != nil {
			t.Fatalf("解析接入源审计详情失败：%v", err)
		}
		if detail["source_name"] != want {
			t.Fatalf("%s 审计必须保存操作时的接入源名称：%+v", action, detail)
		}
		return
	}
	t.Fatalf("未找到 %s 接入源审计", action)
}

// persistedSyncAuditDetail 表示测试从数据库解码的同步汇总详情。
type persistedSyncAuditDetail struct {
	JobID      uint64                         `json:"job_id"`
	SourceName string                         `json:"source_name"`
	Provider   string                         `json:"provider"`
	Changes    map[string]map[string][]string `json:"changes"`
}

// latestSyncAuditDetail 读取指定接入源最新一次同步审计，供生命周期测试直接验证持久化结果。
func latestSyncAuditDetail(t *testing.T, db *gorm.DB, sourceID uint64) persistedSyncAuditDetail {
	t.Helper()
	var entry audit.Log
	if err := db.Where("action = ? AND resource_id = ?", audit.ActionSourceSynced, sourceID).Order("id DESC").First(&entry).Error; err != nil {
		t.Fatalf("读取最新同步审计失败：%v", err)
	}
	var detail persistedSyncAuditDetail
	if err := json.Unmarshal(entry.Detail, &detail); err != nil {
		t.Fatalf("解析最新同步审计详情失败：%v", err)
	}
	return detail
}

// TestConnectionKeepsConcreteLoadBalancerTypes 验证连接探测只报告具体类型并保持各结果集合的输入顺序。
func TestConnectionKeepsConcreteLoadBalancerTypes(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	probeResults := []CollectionResult{
		{ResourceType: "ec2"},
		{ResourceType: "rds"},
		{ResourceType: "clb"},
		{ResourceType: "alb", Err: ErrCloudPermission},
		{ResourceType: "nlb"},
		{ResourceType: "gwlb"},
	}
	result, err := service.TestConnection(context.Background(), source.ProjectID, source.ID, collectorStub{probeResults: probeResults})
	if err != nil {
		t.Fatalf("具体负载均衡类型连接探测失败：%v", err)
	}
	wantReachable := []string{"ec2", "rds", "clb", "nlb", "gwlb"}
	wantFailed := []string{"alb"}
	if !reflect.DeepEqual(result.ReachableTypes, wantReachable) || !reflect.DeepEqual(result.FailedTypes, wantFailed) {
		t.Fatalf("连接结果必须按输入顺序保留具体类型且不得聚合为 elb：got=%+v", result)
	}
	for _, resourceType := range append(append([]string{}, result.ReachableTypes...), result.FailedTypes...) {
		if resourceType == "elb" {
			t.Fatal("连接结果不得包含旧 elb 聚合类型")
		}
	}
	var count int64
	if err := db.Model(&LoadBalancer{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("连接探测不得写入任何负载均衡资源：count=%d err=%v", count, err)
	}
}

// TestConnectionUsesLightweightProbe 防止连接测试再次执行完整资源采集并超过页面请求超时。
func TestConnectionUsesLightweightProbe(t *testing.T) {
	service, _, source, _ := newResourceServiceTest(t)
	collectCalls, probeCalls := 0, 0
	collector := collectorStub{
		results:      []CollectionResult{{ResourceType: "ec2", Err: errors.New("完整采集不应执行")}},
		probeResults: []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "rds"}, {ResourceType: "alb"}},
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
	result, err := service.TestConnection(context.Background(), source.ProjectID, source.ID, collectorStub{probeResults: []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "rds"}, {ResourceType: "alb"}}})
	if err != nil {
		t.Fatalf("连接探测失败：%v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(encoded), `"reachable_types":["ec2","rds","alb"]`) || !strings.Contains(string(encoded), `"failed_types":[]`) {
		t.Fatalf("连接结果必须使用 JSON 数组：%s，错误：%v", encoded, err)
	}
}

// TestSyncDueSourcesOnlyRunsEnabledDueSources 验证调度只处理已启用且到期的接入源。
func TestSyncDueSourcesOnlyRunsEnabledDueSources(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	_ = db.Model(source).Updates(map[string]any{"next_sync_at": past, "enabled": true}).Error
	// 两个排除样本也必须已验证，避免身份门禁掩盖启用状态和到期时间的调度规则。
	disabled := Source{ProjectID: 1, Provider: ProviderAWS, Name: "停用源", EncryptedCredential: source.EncryptedCredential, Enabled: false, SyncIntervalMinutes: 60, NextSyncAt: &past, CloudAccountID: "123456789013", IdentityVerifiedAt: source.IdentityVerifiedAt}
	upcoming := Source{ProjectID: 1, Provider: ProviderAWS, Name: "未到期源", EncryptedCredential: source.EncryptedCredential, Enabled: true, SyncIntervalMinutes: 60, NextSyncAt: &future, CloudAccountID: "123456789014", IdentityVerifiedAt: source.IdentityVerifiedAt}
	_ = db.Create(&disabled).Error
	_ = db.Create(&upcoming).Error
	service.SyncDueSources(context.Background())
	var jobs []SyncJob
	_ = db.Find(&jobs).Error
	if len(jobs) != 1 || jobs[0].SourceID != source.ID || jobs[0].Trigger != "scheduled" {
		t.Fatal("调度器必须仅为到期且启用的接入源创建定时任务")
	}
}
