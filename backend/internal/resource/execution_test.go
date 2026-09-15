// 本文件验证跨接入源并发、排队计时与超时失败收敛的真实持久化行为。
package resource

import (
	"cmdb/internal/audit"
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

type controlledCollector struct {
	collectorStub
	entered chan uint64
	release chan struct{}
	active  atomic.Int32
	peak    atomic.Int32
}

func (c *controlledCollector) Collect(ctx context.Context, source Source, plain []byte) ([]CollectionResult, error) {
	active := c.active.Add(1)
	defer c.active.Add(-1)
	for peak := c.peak.Load(); active > peak && !c.peak.CompareAndSwap(peak, active); peak = c.peak.Load() {
	}
	select {
	case c.entered <- source.ID:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.release:
		return []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "rds"}, {ResourceType: "alb"}}, nil
	}
}

func executionTestService(t *testing.T, max int, timeout time.Duration) (*Service, *gorm.DB, *Source) {
	t.Helper()
	// 取消会淘汰数据库连接，文件夹具避免内存库随最后连接关闭而消失。
	base, db, source, _ := newResourceServiceTest(t, filepath.Join(t.TempDir(), "execution.db"))
	service, err := NewServiceWithExecution(base.repository, base.cipher, base.adapters, ExecutionConfig{MaxConcurrent: max, Timeout: timeout}, audit.NewRepository(db))
	if err != nil {
		t.Fatal("创建同步运行配置失败")
	}
	t.Cleanup(func() { service.Stop(); service.Wait() })
	return service, db, source
}

func awaitJob(t *testing.T, db *gorm.DB, id uint64, status string) SyncJob {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var job SyncJob
		if err := db.First(&job, id).Error; err != nil {
			t.Fatal("读取任务失败")
		}
		if job.Status == status {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("任务未进入预期状态 %s", status)
	return SyncJob{}
}

func TestExecutionKeepsExcessJobsQueued(t *testing.T) {
	service, db, first := executionTestService(t, 1, time.Second)
	second := *first
	second.ID = 0
	second.CloudAccountID = "123456789013"
	if err := db.Create(&second).Error; err != nil {
		t.Fatal("创建第二来源失败")
	}
	collector := &controlledCollector{entered: make(chan uint64, 2), release: make(chan struct{})}
	defer close(collector.release)
	job1, err := service.EnqueueSync(context.Background(), first.ID, "manual", collector)
	if err != nil {
		t.Fatal("首任务入队失败")
	}
	awaitExecutionCollector(t, collector.entered)
	job2, err := service.EnqueueSync(context.Background(), second.ID, "manual", collector)
	if err != nil {
		t.Fatal("并发额度满时仍须持久化排队")
	}
	select {
	case <-collector.entered:
		t.Fatal("超出并发限制的任务不得采集")
	case <-time.After(30 * time.Millisecond):
	}
	awaitJob(t, db, job2.ID, "queued")
	if _, err := service.EnqueueSync(context.Background(), second.ID, "manual", collector); !errors.Is(err, ErrSyncAlreadyRunning) {
		t.Fatal("排队任务必须占用同源互斥")
	}
	releaseExecutionCollector(t, collector.release)
	awaitJob(t, db, job1.ID, "success")
	awaitExecutionCollector(t, collector.entered)
	releaseExecutionCollector(t, collector.release)
	awaitJob(t, db, job2.ID, "success")
	if collector.peak.Load() != 1 {
		t.Fatal("全局并发数量超过限制")
	}
}

func TestExecutionTimeoutPersistsFailureWithoutAssetChanges(t *testing.T) {
	for _, trigger := range []string{"manual", "scheduled"} {
		t.Run(trigger, func(t *testing.T) {
			service, db, source := executionTestService(t, 1, 35*time.Millisecond)
			originalNext := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
			db.Model(source).Update("next_sync_at", originalNext)
			collector := &controlledCollector{entered: make(chan uint64, 1), release: make(chan struct{})}
			job, err := service.EnqueueSync(context.Background(), source.ID, trigger, collector)
			if err != nil {
				t.Fatal("任务入队失败")
			}
			persisted := awaitJob(t, db, job.ID, "failed")
			if persisted.ErrorSummary != "同步执行超时，可重新执行" || len(persisted.Statistics) != 0 {
				t.Fatal("超时任务必须保留安全摘要且清空统计")
			}
			var entries []audit.Log
			if err := db.Where("action = ?", audit.ActionSourceSynced).Find(&entries).Error; err != nil || len(entries) != 1 {
				t.Fatal("超时后独立事务必须保存唯一失败审计")
			}
			var count int64
			db.Model(&Server{}).Count(&count)
			if count != 0 {
				t.Fatal("超时不得写入资源")
			}
			var current Source
			db.First(&current, source.ID)
			if current.LastSyncAt != nil {
				t.Fatal("超时不得刷新最近成功同步时间")
			}
			if trigger == "manual" && !current.NextSyncAt.Equal(originalNext) {
				t.Fatal("手工超时不得改变自动计划")
			}
			if trigger == "scheduled" && (current.NextSyncAt == nil || !current.NextSyncAt.Equal(persisted.FinishedAt.Add(time.Hour))) {
				t.Fatal("自动超时必须推进下次计划")
			}
		})
	}
}

func TestExecutionShutdownLeavesWaitingJobRecoverable(t *testing.T) {
	service, db, first := executionTestService(t, 1, time.Minute)
	second := *first
	second.ID = 0
	second.CloudAccountID = "123456789013"
	db.Create(&second)
	collector := &controlledCollector{entered: make(chan uint64, 2), release: make(chan struct{})}
	job1, _ := service.EnqueueSync(context.Background(), first.ID, "manual", collector)
	awaitExecutionCollector(t, collector.entered)
	job2, _ := service.EnqueueSync(context.Background(), second.ID, "manual", collector)
	service.Stop()
	service.Wait()
	awaitJob(t, db, job1.ID, "failed")
	awaitJob(t, db, job2.ID, "queued")
	if _, err := service.EnqueueSync(context.Background(), second.ID, "manual", collector); !errors.Is(err, ErrSyncStopping) {
		t.Fatal("关闭后不得受理新任务")
	}
}

// TestExecutionRejectsPersistedActiveJob 防止工作器异常退出后遗留任务被第二次受理覆盖。
func TestExecutionRejectsPersistedActiveJob(t *testing.T) {
	service, db, source := executionTestService(t, 1, time.Second)
	previous := &SyncJob{ProjectID: source.ProjectID, SourceID: source.ID, Status: "queued", Trigger: "manual", StartedAt: time.Now()}
	if err := service.repository.CreateJob(context.Background(), previous); err != nil {
		t.Fatal("准备持久化排队任务失败")
	}
	_, err := service.EnqueueSync(context.Background(), source.ID, "manual", collectorStub{})
	if !errors.Is(err, ErrSyncAlreadyRunning) {
		t.Fatal("数据库已有活动任务时必须拒绝新任务")
	}
	var count int64
	db.Model(&SyncJob{}).Count(&count)
	if count != 1 {
		t.Fatal("拒绝重复入队不得额外创建任务")
	}
}

// TestExecutionTimeoutBeforeCollectionConverges 验证开始执行时的数据库超时也能独立落库失败。
func TestExecutionTimeoutBeforeCollectionConverges(t *testing.T) {
	for _, phase := range []string{"读取来源", "更新运行状态"} {
		t.Run(phase, func(t *testing.T) {
			service, db, source := executionTestService(t, 1, 25*time.Millisecond)
			job := &SyncJob{ProjectID: source.ProjectID, SourceID: source.ID, Status: "queued", Trigger: "manual", StartedAt: time.Now()}
			if err := service.repository.CreateJob(context.Background(), job); err != nil {
				t.Fatal("创建测试任务失败")
			}
			var once atomic.Bool
			callback := func(tx *gorm.DB) {
				target := "resource_sources"
				if phase == "更新运行状态" {
					target = "sync_jobs"
				}
				if tx.Statement.Table == target && once.CompareAndSwap(false, true) {
					<-tx.Statement.Context.Done()
					tx.AddError(tx.Statement.Context.Err())
				}
			}
			if phase == "读取来源" {
				db.Callback().Query().Before("gorm:query").Register("测试执行读取超时", callback)
			} else {
				db.Callback().Update().Before("gorm:update").Register("测试状态更新超时", callback)
			}
			_, err := service.executeSync(context.Background(), source.ID, "manual", collectorStub{}, job)
			if err == nil {
				t.Fatal("开始执行超时必须返回失败")
			}
			awaitJob(t, db, job.ID, "failed")
		})
	}
}

// executionTestAdapter 以受控采集函数复用平台契约，验证调度入口而不访问真实云账号。
type executionTestAdapter struct {
	identityAdapterStub
	collect func(context.Context, Source, []byte) ([]CollectionResult, error)
}

func (a executionTestAdapter) Collect(ctx context.Context, source Source, credential []byte) ([]CollectionResult, error) {
	return a.collect(ctx, source, credential)
}

// TestExecutionTimeoutPreservesExistingAssetLifecycle 防止取消后返回的空快照触发正常资产失联或到期资产删除。
func TestExecutionTimeoutPreservesExistingAssetLifecycle(t *testing.T) {
	service, db, source := executionTestService(t, 1, 100*time.Millisecond)
	now := time.Now().UTC().Truncate(time.Second)
	service.now = func() time.Time { return now }
	old := now.Add(-25 * time.Hour)
	assets := []Server{
		{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: source.Provider, ResourceType: "ec2", ExternalID: "原有正常资产", AssetStatus: AssetStatusActive, FirstSeenAt: old, LastSeenAt: old}, PrivateIPs: json.RawMessage(`[]`), PublicIPs: json.RawMessage(`[]`)},
		{AssetBase: AssetBase{ProjectID: source.ProjectID, SourceID: source.ID, Provider: source.Provider, ResourceType: "ec2", ExternalID: "失联满二十五小时资产", AssetStatus: AssetStatusLost, MissingSince: &old, FirstSeenAt: old, LastSeenAt: old}, PrivateIPs: json.RawMessage(`[]`), PublicIPs: json.RawMessage(`[]`)},
	}
	if err := db.Create(&assets).Error; err != nil {
		t.Fatal("准备正常与已到清理时限的资产失败")
	}
	var before []Server
	if err := db.Order("id").Find(&before).Error; err != nil {
		t.Fatal("读取超时前资产快照失败")
	}
	collector := executionTestAdapter{collect: func(ctx context.Context, _ Source, _ []byte) ([]CollectionResult, error) {
		<-ctx.Done()
		// 模拟适配器取消后仍返回完整空结果；共享执行层必须阻止其进入生命周期处理。
		return []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "rds"}, {ResourceType: "alb"}}, nil
	}}
	job, err := service.EnqueueSync(context.Background(), source.ID, "manual", collector)
	if err != nil {
		t.Fatal("超时场景入队失败")
	}
	failed := awaitJob(t, db, job.ID, "failed")
	if failed.ErrorSummary != "同步执行超时，可重新执行" || len(failed.Statistics) != 0 {
		t.Fatal("超时返回的快照不得形成成功统计")
	}
	var after []Server
	if err := db.Order("id").Find(&after).Error; err != nil {
		t.Fatal("读取超时后的资产快照失败")
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("超时必须保留正常和到期失联资产的全部字段，不得失联、删除或刷新观测时间")
	}
}

// TestExecutionQueueWaitDoesNotConsumeTimeout 验证前一工作器缓慢响应取消时，后续任务的预算仍从领取名额开始。
func TestExecutionQueueWaitDoesNotConsumeTimeout(t *testing.T) {
	const budget = 200 * time.Millisecond
	service, db, first := executionTestService(t, 1, budget)
	second := *first
	second.ID, second.CloudAccountID = 0, "排队预算第二账号"
	if err := db.Create(&second).Error; err != nil {
		t.Fatal("创建排队预算测试来源失败")
	}
	firstEntered := make(chan struct{})
	firstCanceled := make(chan struct{})
	finishFirst := make(chan struct{})
	defer close(finishFirst)
	remainingBudget := make(chan time.Duration, 1)
	collector := executionTestAdapter{collect: func(ctx context.Context, source Source, _ []byte) ([]CollectionResult, error) {
		if source.ID == first.ID {
			close(firstEntered)
			<-ctx.Done()
			close(firstCanceled)
			// 受控延迟取消收尾，确保第二任务排队时间确实超过完整执行预算。
			<-finishFirst
			return nil, ctx.Err()
		}
		deadline, exists := ctx.Deadline()
		if !exists {
			remainingBudget <- 0
		} else {
			remainingBudget <- time.Until(deadline)
		}
		return []CollectionResult{{ResourceType: "ec2"}, {ResourceType: "rds"}, {ResourceType: "alb"}}, nil
	}}
	job1, err := service.EnqueueSync(context.Background(), first.ID, "manual", collector)
	if err != nil {
		t.Fatal("首任务入队失败")
	}
	select {
	case <-firstEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("首任务未进入采集")
	}
	queuedAt := time.Now()
	job2, err := service.EnqueueSync(context.Background(), second.ID, "manual", collector)
	if err != nil {
		t.Fatal("第二任务入队失败")
	}
	select {
	case <-firstCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("首任务未收到执行超时")
	}
	// 第一任务仍占用名额，等待到第二任务排队时长明确超过预算。
	if wait := time.Until(queuedAt.Add(budget + 50*time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
	awaitJob(t, db, job2.ID, "queued")
	releaseExecutionCollector(t, finishFirst)
	select {
	case remaining := <-remainingBudget:
		if remaining < budget/2 {
			t.Fatalf("第二任务领取名额后应保留独立预算，实际仅剩 %s", remaining)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("等待超过执行预算的任务仍须获得采集机会")
	}
	awaitJob(t, db, job1.ID, "failed")
	awaitJob(t, db, job2.ID, "success")
}

// TestExecutionRecoverySharesLimitAndResumesDisabledSource 验证停用前受理的任务继续恢复，并与当前手工任务共享全局并发名额。
func TestExecutionRecoverySharesLimitAndResumesDisabledSource(t *testing.T) {
	service, db, first := executionTestService(t, 1, 5*time.Second)
	second := *first
	second.ID, second.CloudAccountID = 0, "恢复第二账号"
	third := *first
	third.ID, third.CloudAccountID = 0, "当前手工账号"
	for _, source := range []*Source{&second, &third} {
		if err := db.Create(source).Error; err != nil {
			t.Fatal("创建恢复并发来源失败")
		}
	}
	for _, source := range []*Source{first, &second, &third} {
		if err := db.Model(source).Update("next_sync_at", time.Now().Add(time.Hour)).Error; err != nil {
			t.Fatal("设置未来自动计划失败")
		}
	}
	jobs := []*SyncJob{
		{ProjectID: first.ProjectID, SourceID: first.ID, Status: "queued", Trigger: "manual", StartedAt: time.Now()},
		{ProjectID: second.ProjectID, SourceID: second.ID, Status: "queued", Trigger: "manual", StartedAt: time.Now()},
	}
	for _, job := range jobs {
		if err := service.repository.CreateJob(context.Background(), job); err != nil {
			t.Fatal("准备恢复排队任务失败")
		}
	}
	if err := db.Model(first).Update("enabled", false).Error; err != nil {
		t.Fatal("模拟任务受理后停用来源失败")
	}
	collector := &controlledCollector{entered: make(chan uint64, 3), release: make(chan struct{})}
	defer close(collector.release)
	service.adapters[ProviderAWS] = executionTestAdapter{collect: collector.Collect}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.StartScheduler(ctx); err != nil {
		t.Fatal("启动恢复失败")
	}
	var firstRunningSource uint64
	select {
	case firstRunningSource = <-collector.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("恢复任务未进入采集")
	}
	manualJob, err := service.EnqueueSync(context.Background(), third.ID, "manual", collector)
	if err != nil {
		t.Fatal("恢复期间仍须受理其他来源的手工任务")
	}
	jobs = append(jobs, manualJob)
	for _, job := range jobs {
		if job.SourceID != firstRunningSource {
			awaitJob(t, db, job.ID, "queued")
		}
	}
	select {
	case <-collector.entered:
		t.Fatal("恢复和手工任务不得各自占用独立并发配额")
	case <-time.After(30 * time.Millisecond):
	}
	seen := map[uint64]bool{firstRunningSource: true}
	for range 2 {
		releaseExecutionCollector(t, collector.release)
		select {
		case sourceID := <-collector.entered:
			if seen[sourceID] {
				t.Fatal("恢复不得重复执行同一来源")
			}
			seen[sourceID] = true
		case <-time.After(3 * time.Second):
			t.Fatal("名额释放后未继续执行恢复或手工任务")
		}
	}
	releaseExecutionCollector(t, collector.release)
	for _, job := range jobs {
		awaitJob(t, db, job.ID, "success")
	}
	if !seen[first.ID] || collector.peak.Load() != 1 {
		t.Fatal("停用来源必须恢复，且所有路径只能共享一个执行名额")
	}
	var persisted Source
	if err := db.First(&persisted, first.ID).Error; err != nil || persisted.Enabled {
		t.Fatal("恢复既有任务不得重新启用已停用来源")
	}
}

// awaitExecutionCollector 为进入采集的屏障设置上界，避免执行层回归时测试永久阻塞。
func awaitExecutionCollector(t *testing.T, entered <-chan uint64) uint64 {
	t.Helper()
	select {
	case sourceID := <-entered:
		return sourceID
	case <-time.After(3 * time.Second):
		t.Fatal("任务未在限定时间内进入采集")
		return 0
	}
}

// releaseExecutionCollector 在工作器提前超时或退出时及时失败，不向无人接收的通道永久发送。
func releaseExecutionCollector(t *testing.T, release chan<- struct{}) {
	t.Helper()
	select {
	case release <- struct{}{}:
	case <-time.After(3 * time.Second):
		t.Fatal("工作器未在限定时间内接收采集释放信号")
	}
}
