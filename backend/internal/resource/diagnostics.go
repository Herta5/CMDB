// 本文件只输出同步控制层的固定事件和内部任务定位信息，不记录云端载荷或任意错误文本。
package resource

import (
	"cmdb/internal/platform/diagnostics"
	"context"
	"log/slog"
	"time"
)

func (s *Service) logSync(ctx context.Context, level slog.Level, message, event string, sourceID uint64, job *SyncJob, elapsed time.Duration) {
	fields := []any{"event", event, "request_id", diagnostics.RequestID(ctx), "source_id", sourceID, "duration_ms", elapsed.Milliseconds(), "active_syncs", len(s.execution.slots)}
	if job != nil {
		status := "unknown"
		switch job.Status {
		case "queued", "running", "success", "partial_success", "failed":
			status = job.Status
		}
		fields = append(fields, "job_id", job.ID, "project_id", job.ProjectID, "status", status)
	}
	s.logger.Log(ctx, level, message, fields...)
}
