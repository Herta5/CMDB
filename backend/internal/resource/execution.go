// 本文件统一管理单实例同步的并发名额、执行超时和服务关闭边界。
package resource

import (
	"cmdb/internal/platform/diagnostics"
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"cmdb/internal/audit"
)

// ErrSyncStopping 表示服务已停止受理新同步，已持久化排队任务留待重启恢复。
var ErrSyncStopping = errors.New("服务正在关闭，暂不受理同步任务")

// ExecutionConfig 是跨平台共享的运行限额，超时只从获得并发名额后开始计算。
type ExecutionConfig struct {
	MaxConcurrent int
	Timeout       time.Duration
}

// DefaultExecutionConfig 为单机部署预留数据库与云 API 的并发余量。
func DefaultExecutionConfig() ExecutionConfig {
	return ExecutionConfig{MaxConcurrent: 4, Timeout: 15 * time.Minute}
}

// executionRuntime 仅管理本进程工作器，持久化任务仍由共享仓储维护。
type executionRuntime struct {
	slots    chan struct{}
	timeout  time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	stopping bool
	workers  sync.WaitGroup
}

// NewServiceWithExecution 在启动前固定运行参数，避免工作器执行期间改变限额导致竞争。
func NewServiceWithExecution(repository *Repository, cipher *CredentialCipher, adapters map[string]ProviderAdapter, limits ExecutionConfig, recorders ...audit.Recorder) (*Service, error) {
	if limits.MaxConcurrent < 1 || limits.MaxConcurrent > 64 || limits.Timeout <= 0 || limits.Timeout > 24*time.Hour {
		return nil, errors.New("同步运行参数无效")
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{repository: repository, cipher: cipher, adapters: adapters, now: time.Now, logger: slog.Default(),
		execution: &executionRuntime{slots: make(chan struct{}, limits.MaxConcurrent), timeout: limits.Timeout, ctx: ctx, cancel: cancel}}
	if len(recorders) > 0 {
		service.auditRecorder = recorders[0]
	}
	return service, nil
}

// Stop 停止新任务领取并取消正在运行的采集；未开始任务保持 queued，供重启恢复。
func (s *Service) Stop() {
	s.execution.mu.Lock()
	defer s.execution.mu.Unlock()
	s.execution.stopping = true
	s.execution.cancel()
}

// Wait 在 Stop 后等待工作器完成失败收敛，不等待排队任务重新执行。
func (s *Service) Wait() { s.execution.workers.Wait() }

func (s *Service) acceptingSync() bool {
	return s.execution.ctx.Err() == nil
}

// executeSync 将所有触发路径置于同一名额和时限内；等待名额不会提前标记 running。
func (s *Service) executeSync(parent context.Context, sourceID uint64, trigger string, collector Collector, job *SyncJob) (result *SyncJob, resultErr error) {
	runtime := s.execution
	runtime.mu.Lock()
	if runtime.stopping {
		runtime.mu.Unlock()
		return job, ErrSyncStopping
	}
	runtime.workers.Add(1)
	runtime.mu.Unlock()
	defer runtime.workers.Done()

	waiting, cancelWaiting := context.WithCancel(parent)
	stopCancellation := context.AfterFunc(runtime.ctx, cancelWaiting)
	defer stopCancellation()
	defer cancelWaiting()
	select {
	case runtime.slots <- struct{}{}:
		defer func() { <-runtime.slots }()
	case <-waiting.Done():
		return job, ErrSyncStopping
	}
	// 关闭与名额释放同时发生时仍禁止开始等待中的任务。
	if waiting.Err() != nil || runtime.ctx.Err() != nil {
		return job, ErrSyncStopping
	}
	ctx, cancel := context.WithTimeout(waiting, runtime.timeout)
	defer cancel()
	started := time.Now()
	if job == nil {
		job = &SyncJob{}
	}
	defer func() {
		// SDK 或事务回调异常必须留在工作器边界内，不能把 panic 原文和堆栈交给运行时输出。
		if recover() != nil {
			s.logSync(ctx, slog.LevelError, "同步执行出现异常", "sync_panicked", sourceID, job, time.Since(started))
			result = job
			resultErr = errors.New("同步执行异常，可重新执行")
			if job.ID != 0 {
				result, resultErr = s.finishPanicked(ctx, job)
			}
		}
		level := slog.LevelInfo
		if resultErr != nil || (result != nil && (result.Status == "failed" || result.Status == "partial_success")) {
			level = slog.LevelWarn
		}
		s.logSync(ctx, level, "同步任务执行结束", "sync_finished", sourceID, result, time.Since(started))
	}()
	return s.executeSyncBody(ctx, sourceID, trigger, collector, job)
}

// finishPanicked 对异常后的二次落库仍设置恢复边界；落库失败由启动恢复继续处理。
func (s *Service) finishPanicked(ctx context.Context, job *SyncJob) (result *SyncJob, err error) {
	result, err = job, errors.New("同步执行异常，可重新执行")
	defer func() {
		if recover() != nil {
			s.logSync(ctx, slog.LevelError, "同步失败结果保存失败，需检查数据库", "sync_failure_persist_failed", job.SourceID, job, 0)
		}
	}()
	return s.finishFailed(ctx, job, "同步执行异常，可重新执行", err)
}

// failureContext 只保留最小审计身份，并为已取消任务提供独立、有限的失败落库时间。
func failureContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(diagnostics.CopyRequestID(audit.DetachedContext(ctx), ctx), 10*time.Second)
}
