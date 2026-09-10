-- CMDB 异步同步迁移：增加排队状态及失败任务重试链路。
ALTER TABLE sync_jobs
  MODIFY COLUMN status ENUM('queued','running','success','partial_success','failed') NOT NULL,
  ADD COLUMN previous_job_id BIGINT UNSIGNED NULL AFTER source_id,
  ADD KEY idx_jobs_previous (previous_job_id);
