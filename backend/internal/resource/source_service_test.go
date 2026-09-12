// 本文件验证接入源创建、加密存储和项目隔离查询。
package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"cmdb/internal/audit"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// identityAdapterStub 模拟平台边界的稳定身份响应；校验继续经过共享严格 JSON 解析。
type identityAdapterStub struct {
	collectorStub
	aliyun  bool
	resolve func(context.Context, Source, []byte) (string, error)
}

// ValidateCredential 保留平台格式校验的业务边界，不让模拟身份绕过完整凭证要求。
func (a identityAdapterStub) ValidateCredential(raw json.RawMessage) error {
	if a.aliyun {
		_, err := DecodeStrictStringObject(raw, []string{"access_key_id", "access_key_secret"}, nil)
		return err
	}
	_, err := DecodeStrictStringObject(raw, []string{"access_key_id", "secret_access_key"}, []string{"session_token"})
	return err
}

// ValidateConfig 使用共享严格空配置规则，阻止未知字段进入数据库。
func (a identityAdapterStub) ValidateConfig(raw json.RawMessage) error {
	return ValidateEmptyConfig(raw)
}

// ResourceTypes 提供同步测试使用的 AWS 首期资源范围。
func (a identityAdapterStub) ResourceTypes() []string { return []string{"ec2", "rds", "elb"} }

// ResolveCloudAccountID 替代联网身份解析，返回测试显式指定的安全响应。
func (a identityAdapterStub) ResolveCloudAccountID(ctx context.Context, source Source, plain []byte) (string, error) {
	if a.resolve != nil {
		return a.resolve(ctx, source, plain)
	}
	return "123456789012", nil
}

func identityTestAdapters() map[string]ProviderAdapter {
	return map[string]ProviderAdapter{ProviderAWS: identityAdapterStub{}, ProviderAliyun: identityAdapterStub{aliyun: true}}
}

func identityInput(projectID uint64) CreateSourceInput {
	return CreateSourceInput{ProjectID: projectID, Provider: ProviderAWS, Name: "身份测试账号", Region: "ap-east-1", Credential: json.RawMessage(`{"access_key_id":"虚构旧标识","secret_access_key":"虚构旧密钥"}`)}
}

// TestSourceIdentityCreationAtomicity 防止识别失败、非法配置或同账号重复接入留下记录与成功审计。
func TestSourceIdentityCreationAtomicity(t *testing.T) {
	for _, scenario := range []string{"认证失败", "未知凭证字段", "敏感配置", "已验证身份", "同项目重复", "跨项目重复", "并发重复"} {
		t.Run(scenario, func(t *testing.T) {
			service, db, existing, _ := newResourceServiceTest(t)
			if err := db.Delete(existing).Error; err != nil {
				t.Fatal("清理独立测试夹具失败")
			}
			input := identityInput(1)
			wantErr := error(nil)
			switch scenario {
			case "认证失败":
				service.adapters[ProviderAWS] = identityAdapterStub{resolve: func(context.Context, Source, []byte) (string, error) { return "", ErrCloudAuthentication }}
				wantErr = ErrCloudAuthentication
			case "未知凭证字段":
				input.Credential = json.RawMessage(`{"access_key_id":"虚构","secret_access_key":"虚构","unknown":"虚构"}`)
				wantErr = ErrInvalidProviderCredential
			case "敏感配置":
				input.Config = json.RawMessage(`{"secret_access_key":"虚构"}`)
				wantErr = ErrInvalidProviderConfig
			}
			if scenario == "并发重复" {
				// 两个独立服务实例在解析阶段会合，避免只验证单服务内存锁。
				var ready sync.WaitGroup
				ready.Add(2)
				adapter := identityAdapterStub{resolve: func(context.Context, Source, []byte) (string, error) {
					ready.Done()
					ready.Wait()
					return "123456789012", nil
				}}
				outcomes := make(chan error, 2)
				for i := 0; i < 2; i++ {
					other := NewService(NewRepository(db), service.cipher, map[string]ProviderAdapter{ProviderAWS: adapter}, audit.NewRepository(db))
					go func() { _, err := other.CreateSource(context.Background(), input); outcomes <- err }()
				}
				first, second := <-outcomes, <-outcomes
				if !((first == nil && errors.Is(second, ErrCloudAccountConflict)) || (second == nil && errors.Is(first, ErrCloudAccountConflict))) {
					t.Fatal("并发创建同账号必须只有一次成功，其余返回稳定冲突")
				}
			} else {
				created, err := service.CreateSource(context.Background(), input)
				if !errors.Is(err, wantErr) {
					t.Fatalf("创建应返回安全领域结果：%v", err)
				}
				if wantErr == nil {
					if created.IdentityStatus != IdentityStatusVerified || created.CloudAccountID != "123456789012" || created.IdentityVerifiedAt == nil {
						t.Fatal("新接入源必须保存已验证账号身份")
					}
					encoded, _ := json.Marshal(created)
					if strings.Contains(string(encoded), "123456789012") || strings.Contains(string(encoded), "identity_verified_at") {
						t.Fatal("响应不得公开账号标识与验证时间")
					}
					if scenario == "同项目重复" || scenario == "跨项目重复" {
						if scenario == "跨项目重复" {
							input.ProjectID = 2
						}
						if _, err := service.CreateSource(context.Background(), input); !errors.Is(err, ErrCloudAccountConflict) {
							t.Fatal("同云账号不得通过其他接入源重复归属")
						}
					}
				}
			}
			var sourceCount, auditCount int64
			db.Model(&Source{}).Count(&sourceCount)
			db.Model(&audit.Log{}).Count(&auditCount)
			wantCount := int64(1)
			if wantErr != nil {
				wantCount = 0
			}
			if sourceCount != wantCount || auditCount != wantCount {
				t.Fatalf("失败路径不得留下来源或成功审计：来源=%d，审计=%d", sourceCount, auditCount)
			}
		})
	}
}

// TestSourceIdentityConflictWithTranslatedDatabaseErrors 覆盖生产开启 GORM 错误转换后的真实唯一冲突。
func TestSourceIdentityConflictWithTranslatedDatabaseErrors(t *testing.T) {
	service, db, _, _ := newResourceServiceTest(t)
	db.Config.TranslateError = true
	if _, err := service.CreateSource(context.Background(), identityInput(2)); !errors.Is(err, ErrCloudAccountConflict) {
		t.Fatal("数据库错误转换后仍必须返回稳定云账号冲突")
	}
}

// TestCredentialWritesNeverReachDatabaseLogs 防止数据库成功或失败日志旁路披露完整密文。
func TestCredentialWritesNeverReachDatabaseLogs(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	var output bytes.Buffer
	db.Config.Logger = logger.New(log.New(&output, "", 0), logger.Config{LogLevel: logger.Info})
	service.adapters[ProviderAWS] = identityAdapterStub{resolve: func(context.Context, Source, []byte) (string, error) { return "999999999999", nil }}
	created, err := service.CreateSource(context.Background(), identityInput(2))
	if err != nil {
		t.Fatal("准备凭证日志验证失败")
	}
	service.adapters[ProviderAWS] = identityAdapterStub{}
	updated, err := service.UpdateSource(context.Background(), 1, source.ID, UpdateSourceInput{Name: "更新日志测试", Enabled: true, SyncIntervalMinutes: 60, Credential: identityInput(1).Credential})
	if err != nil {
		t.Fatal("更新凭证日志验证失败")
	}
	if strings.Contains(output.String(), created.EncryptedCredential) || strings.Contains(output.String(), updated.EncryptedCredential) {
		t.Fatal("接入源创建或凭证替换不得通过数据库日志公开完整密文")
	}
}

// TestSourceIdentityCredentialReplacement 防止换凭证改变账号，或事务失败留下部分配置和验证时间。
func TestSourceIdentityCredentialReplacement(t *testing.T) {
	for _, scenario := range []string{"原账号", "其他账号", "云失败", "审计失败"} {
		t.Run(scenario, func(t *testing.T) {
			service, db, source, now := newResourceServiceTest(t)
			before, _ := service.repository.FindSource(context.Background(), source.ID)
			*now = now.Add(time.Hour)
			wantErr := error(nil)
			switch scenario {
			case "其他账号":
				service.adapters[ProviderAWS] = identityAdapterStub{resolve: func(context.Context, Source, []byte) (string, error) { return "999999999999", nil }}
				wantErr = ErrSourceIdentityMismatch
			case "云失败":
				service.adapters[ProviderAWS] = identityAdapterStub{resolve: func(context.Context, Source, []byte) (string, error) { return "", ErrCloudNetwork }}
				wantErr = ErrCloudNetwork
			case "审计失败":
				if err := db.Exec("CREATE TRIGGER reject_identity_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT, '身份审计写入失败'); END").Error; err != nil {
					t.Fatal("准备审计失败失败")
				}
			}
			input := UpdateSourceInput{Name: "新配置", Region: "ap-southeast-1", Enabled: true, SyncIntervalMinutes: 120, Credential: json.RawMessage(`{"access_key_id":"虚构新标识","secret_access_key":"虚构新密钥"}`)}
			_, err := service.UpdateSource(context.Background(), source.ProjectID, source.ID, input)
			if scenario == "审计失败" {
				if err == nil {
					t.Fatal("审计失败必须回滚凭证替换")
				}
			} else if !errors.Is(err, wantErr) {
				t.Fatalf("换凭证返回非预期结果：%v", err)
			}
			after, readErr := service.repository.FindSource(context.Background(), source.ID)
			if readErr != nil {
				t.Fatal("读取替换结果失败")
			}
			if scenario == "原账号" {
				plain, decryptErr := service.cipher.Decrypt(after.EncryptedCredential)
				if decryptErr != nil || string(plain) != string(input.Credential) || after.Name != "新配置" || after.CloudAccountID != before.CloudAccountID || after.IdentityVerifiedAt == nil || !after.IdentityVerifiedAt.Equal(*now) {
					t.Fatal("原账号新凭证、配置与验证时间必须一起更新")
				}
			} else if after.EncryptedCredential != before.EncryptedCredential || after.Name != before.Name || after.Region != before.Region || string(after.Config) != string(before.Config) || after.IdentityVerifiedAt == nil || !after.IdentityVerifiedAt.Equal(*before.IdentityVerifiedAt) {
				t.Fatal("换凭证失败必须保留原密文、配置和验证时间")
			}
		})
	}
}

// TestPendingIdentityOnlyAllowsReadAndVerification 防止历史来源绕过身份确认执行任何普通写入或采集。
func TestPendingIdentityOnlyAllowsReadAndVerification(t *testing.T) {
	service, db, source, _ := newResourceServiceTest(t)
	if err := db.Model(source).Updates(map[string]any{"identity_status": "pending", "cloud_account_id": nil, "identity_verified_at": nil}).Error; err != nil {
		t.Fatal("准备历史待验证来源失败")
	}
	ctx := context.Background()
	if _, err := service.FindSourceForProject(ctx, 1, source.ID); err != nil {
		t.Fatal("待验证来源必须允许项目内查询")
	}
	job := &SyncJob{ProjectID: 1, SourceID: source.ID, Status: "failed", Trigger: "manual"}
	if err := db.Create(job).Error; err != nil {
		t.Fatal("准备历史任务失败")
	}
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"编辑和启停", func() error {
			_, err := service.UpdateSource(ctx, 1, source.ID, UpdateSourceInput{Name: "新名称", Enabled: false, SyncIntervalMinutes: 60})
			return err
		}},
		{"删除", func() error { return service.DeleteSource(ctx, 1, source.ID) }},
		{"连接测试", func() error { _, err := service.TestConnection(ctx, 1, source.ID, collectorStub{}); return err }},
		{"同步", func() error { _, err := service.Sync(ctx, source.ID, "manual", collectorStub{}); return err }},
		{"排队", func() error { _, err := service.EnqueueSync(ctx, source.ID, "manual", collectorStub{}); return err }},
		{"重试", func() error { _, err := service.RetryJob(ctx, 1, job.ID); return err }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			if !errors.Is(operation.run(), ErrSourceIdentityPending) {
				t.Fatal("待验证来源必须拒绝该操作")
			}
		})
	}
	due, err := service.repository.ListDueSources(ctx, time.Now().Add(time.Hour))
	if err != nil || len(due) != 0 {
		t.Fatal("自动调度不得选择待验证来源")
	}
	var jobs int64
	db.Model(&SyncJob{}).Count(&jobs)
	if jobs != 1 {
		t.Fatal("待验证来源不得创建任何新任务")
	}
}

// TestVerifyHistoricalSourceIdentity 验证历史来源可用原凭证或完整新凭证确认身份，所有失败均保持 pending。
func TestVerifyHistoricalSourceIdentity(t *testing.T) {
	for _, scenario := range []string{"原凭证", "新凭证", "冲突", "云失败", "项目停用", "项目不存在", "审计失败", "不完整凭证"} {
		t.Run(scenario, func(t *testing.T) {
			service, db, source, _ := newResourceServiceTest(t)
			plain := identityInput(1).Credential
			encrypted, _ := service.cipher.Encrypt(plain)
			if err := db.Model(source).Updates(map[string]any{"identity_status": "pending", "cloud_account_id": nil, "identity_verified_at": nil, "encrypted_credential": encrypted}).Error; err != nil {
				t.Fatal("准备待验证来源失败")
			}
			credential := json.RawMessage(`{"access_key_id":"虚构新标识","secret_access_key":"虚构新密钥"}`)
			switch scenario {
			case "原凭证":
				credential = nil
			case "冲突":
				if _, err := service.CreateSource(context.Background(), identityInput(2)); err != nil {
					t.Fatal("准备冲突账号失败")
				}
			case "云失败":
				service.adapters[ProviderAWS] = identityAdapterStub{resolve: func(context.Context, Source, []byte) (string, error) { return "", ErrCloudNetwork }}
			case "项目停用":
				db.Exec("UPDATE projects SET status = 'disabled' WHERE id = 1")
			case "项目不存在":
				db.Exec("DELETE FROM projects WHERE id = 1")
			case "审计失败":
				db.Exec("CREATE TRIGGER reject_verification_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT, '验证审计失败'); END")
			case "不完整凭证":
				credential = json.RawMessage(`{"access_key_id":"虚构"}`)
			}
			verified, err := service.VerifySourceIdentity(context.Background(), 1, source.ID, credential)
			if scenario == "原凭证" || scenario == "新凭证" {
				if err != nil || verified.IdentityStatus != IdentityStatusVerified || verified.CloudAccountID != "123456789012" || verified.IdentityVerifiedAt == nil {
					t.Fatal("历史来源应保存已确认账号身份")
				}
				if scenario == "原凭证" && verified.EncryptedCredential != encrypted {
					t.Fatal("使用原凭证验证不得生成新密文")
				}
				if scenario == "新凭证" {
					decrypted, _ := service.cipher.Decrypt(verified.EncryptedCredential)
					if string(decrypted) != string(credential) {
						t.Fatal("新凭证必须随身份原子更新")
					}
				}
			} else {
				if err == nil {
					t.Fatal("验证失败必须返回错误")
				}
				if scenario == "冲突" && !errors.Is(err, ErrCloudAccountConflict) {
					t.Fatal("重复账号必须返回稳定冲突")
				}
				after, _ := service.repository.FindSource(context.Background(), source.ID)
				if after.IdentityStatus != IdentityStatusPending || after.CloudAccountID != "" || after.IdentityVerifiedAt != nil || after.EncryptedCredential != encrypted {
					t.Fatal("验证失败不得留下身份、验证时间或新密文")
				}
			}
			var verificationAudits int64
			if err := db.Model(&audit.Log{}).Where("resource_id = ? AND action = ?", source.ID, audit.ActionSourceUpdated).Count(&verificationAudits).Error; err != nil {
				t.Fatal("查询身份验证审计失败")
			}
			wantAudits := int64(0)
			if scenario == "原凭证" || scenario == "新凭证" {
				wantAudits = 1
			}
			if verificationAudits != wantAudits {
				t.Fatal("身份验证审计必须与状态提交结果一致")
			}
		})
	}
}

// TestPendingSourceStartupRecoveryPreservesAssets 防止重启继续采集历史 pending 来源，或者遗留统计误报资产变化。
func TestPendingSourceStartupRecoveryPreservesAssets(t *testing.T) {
	service, db, source, now := newResourceServiceTest(t)
	if err := db.Model(source).Updates(map[string]any{"identity_status": "pending", "cloud_account_id": nil, "identity_verified_at": nil}).Error; err != nil {
		t.Fatal("准备待验证来源失败")
	}
	asset := Server{AssetBase: AssetBase{ProjectID: 1, SourceID: source.ID, Provider: ProviderAWS, ResourceType: "ec2", ExternalID: "历史资产", AssetStatus: AssetStatusActive, LastSeenAt: *now}}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal("准备历史资产失败")
	}
	for _, status := range []string{"queued", "running"} {
		if err := db.Create(&SyncJob{ProjectID: 1, SourceID: source.ID, Status: status, Trigger: "manual", Statistics: json.RawMessage(`{"ec2":{"added":9}}`)}).Error; err != nil {
			t.Fatal("准备遗留任务失败")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := service.StartScheduler(ctx); err != nil {
		t.Fatal("待验证来源安全恢复失败")
	}
	var jobs []SyncJob
	if err := db.Find(&jobs).Error; err != nil {
		t.Fatal("读取恢复任务失败")
	}
	for _, job := range jobs {
		if job.Status != "failed" || len(job.Statistics) != 0 || job.FinishedAt == nil || !strings.Contains(job.ErrorSummary, "身份") {
			t.Fatal("历史待验证任务必须安全失败并清空统计")
		}
	}
	var persisted Server
	if err := db.First(&persisted, asset.ID).Error; err != nil || persisted.AssetStatus != AssetStatusActive || !persisted.LastSeenAt.Equal(*now) {
		t.Fatal("历史任务恢复不得改变资产")
	}
}

// TestSchedulerRecoveryErrorsAbortStartup 验证恢复查询、任务保存或审计失败会阻断启动并保留可再次恢复的记录。
func TestSchedulerRecoveryErrorsAbortStartup(t *testing.T) {
	for _, scenario := range []string{"查询失败", "任务保存失败", "审计失败"} {
		t.Run(scenario, func(t *testing.T) {
			service, db, source, _ := newResourceServiceTest(t)
			if err := db.Model(source).Updates(map[string]any{"identity_status": IdentityStatusPending, "cloud_account_id": nil, "identity_verified_at": nil}).Error; err != nil {
				t.Fatal("准备待验证来源失败")
			}
			job := SyncJob{ProjectID: source.ProjectID, SourceID: source.ID, Status: "queued", Trigger: "manual"}
			if err := db.Create(&job).Error; err != nil {
				t.Fatal("准备排队任务失败")
			}
			const rawFailure = "模拟底层恢复错误正文"
			var logs bytes.Buffer
			db.Config.Logger = logger.New(log.New(&logs, "", 0), logger.Config{LogLevel: logger.Info})
			switch scenario {
			case "查询失败":
				if err := db.Callback().Query().Before("gorm:query").Register("test:startup_query_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "sync_jobs" {
						tx.AddError(errors.New(rawFailure))
					}
				}); err != nil {
					t.Fatal("准备恢复查询失败失败")
				}
			case "任务保存失败":
				if err := db.Exec("CREATE TRIGGER reject_recovery_job BEFORE UPDATE ON sync_jobs BEGIN SELECT RAISE(ABORT, '模拟底层恢复错误正文'); END").Error; err != nil {
					t.Fatal("准备任务保存失败失败")
				}
			case "审计失败":
				if err := db.Exec("CREATE TRIGGER reject_recovery_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT, '模拟底层恢复错误正文'); END").Error; err != nil {
					t.Fatal("准备审计保存失败失败")
				}
			}
			logs.Reset()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := service.StartScheduler(ctx)
			if err == nil || err.Error() != "恢复同步任务失败，服务未启动" {
				t.Fatal("恢复失败必须明确向启动方返回中文安全错误")
			}
			if strings.Contains(logs.String(), rawFailure) {
				t.Fatal("启动恢复不得通过数据库日志旁路披露底层错误")
			}
			if scenario == "查询失败" {
				db.Callback().Query().Remove("test:startup_query_failure")
			}
			persisted, readErr := service.repository.FindJob(context.Background(), job.ID)
			if readErr != nil || persisted.Status != "queued" || persisted.FinishedAt != nil {
				t.Fatal("恢复失败必须保留原任务，供修复后再次恢复")
			}
			var auditCount int64
			if err := db.Model(&audit.Log{}).Count(&auditCount).Error; err != nil || auditCount != 0 {
				t.Fatal("恢复失败不得保留不一致的失败审计")
			}
		})
	}
}

// TestSourceMutationsRollBackWhenAuditWriteFails 防止接入源新增、编辑或删除与审计分开提交。
func TestSourceMutationsRollBackWhenAuditWriteFails(t *testing.T) {
	for _, operation := range []string{"新增", "编辑", "删除"} {
		t.Run(operation, func(t *testing.T) {
			db := sourceAuditFailureDatabase(t)
			repository := NewRepository(db)
			cipher := NewCredentialCipher("source-atomic-key")
			setup := NewService(repository, cipher, identityTestAdapters())
			var source *Source
			if operation != "新增" {
				var err error
				source, err = setup.CreateSource(context.Background(), CreateSourceInput{ProjectID: 3, Provider: ProviderAWS, Name: "原接入源", Credential: json.RawMessage(`{"access_key_id":"id","secret_access_key":"secret"}`)})
				if err != nil {
					t.Fatalf("准备接入源失败：%v", err)
				}
			}
			service := NewService(repository, cipher, identityTestAdapters(), audit.NewRepository(db))
			var err error
			switch operation {
			case "新增":
				_, err = service.CreateSource(context.Background(), CreateSourceInput{ProjectID: 3, Provider: ProviderAWS, Name: "新接入源", Credential: json.RawMessage(`{"access_key_id":"id","secret_access_key":"secret"}`)})
			case "编辑":
				_, err = service.UpdateSource(context.Background(), 3, source.ID, UpdateSourceInput{Name: "新名称", Enabled: true, SyncIntervalMinutes: 60})
			case "删除":
				err = service.DeleteSource(context.Background(), 3, source.ID)
			}
			if err == nil {
				t.Fatalf("审计写入失败时接入源%s必须返回错误", operation)
			}
			var values []Source
			if err := db.Order("id ASC").Find(&values).Error; err != nil {
				t.Fatalf("查询接入源回滚结果失败：%v", err)
			}
			switch operation {
			case "新增":
				if len(values) != 0 {
					t.Fatalf("审计写入失败时必须回滚接入源新增：%+v", values)
				}
			case "编辑":
				if len(values) != 1 || values[0].Name != "原接入源" {
					t.Fatalf("审计写入失败时必须回滚接入源编辑：%+v", values)
				}
			case "删除":
				if len(values) != 1 || values[0].ID != source.ID {
					t.Fatalf("审计写入失败时必须回滚接入源删除：%+v", values)
				}
			}
		})
	}
}

// TestDeleteSourceDoesNotKeepAuditWhenBusinessDeleteFails 防止删除失败却留下“已删除”审计。
func TestDeleteSourceDoesNotKeepAuditWhenBusinessDeleteFails(t *testing.T) {
	db := sourceAuditFailureDatabase(t)
	if err := db.AutoMigrate(&audit.Log{}); err != nil {
		t.Fatalf("创建审计测试表失败：%v", err)
	}
	repository := NewRepository(db)
	cipher := NewCredentialCipher("source-delete-failure-key")
	setup := NewService(repository, cipher, identityTestAdapters())
	source, err := setup.CreateSource(context.Background(), CreateSourceInput{ProjectID: 3, Provider: ProviderAWS, Name: "删除失败接入源", Credential: json.RawMessage(`{"access_key_id":"id","secret_access_key":"secret"}`)})
	if err != nil {
		t.Fatalf("准备接入源失败：%v", err)
	}
	if err := db.Exec("CREATE TRIGGER prevent_source_delete BEFORE DELETE ON resource_sources BEGIN SELECT RAISE(ABORT, '禁止测试删除'); END").Error; err != nil {
		t.Fatalf("创建删除失败触发器失败：%v", err)
	}
	service := NewService(repository, cipher, identityTestAdapters(), audit.NewRepository(db))

	if err := service.DeleteSource(context.Background(), 3, source.ID); err == nil {
		t.Fatal("底层删除失败必须返回错误")
	}
	var count int64
	if err := db.Model(&audit.Log{}).Where("action = ? AND resource_id = ?", audit.ActionSourceDeleted, source.ID).Count(&count).Error; err != nil {
		t.Fatalf("查询删除审计失败：%v", err)
	}
	if count != 0 {
		t.Fatalf("接入源未删除时不得保留删除审计：%d", count)
	}
}

// sourceAuditFailureDatabase 创建缺少 audit_logs 的真实数据库，只让审计写入在事务末端失败。
func sourceAuditFailureDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("创建接入源事务测试数据库失败：%v", err)
	}
	if err := db.AutoMigrate(&Source{}); err != nil {
		t.Fatalf("创建接入源测试表失败：%v", err)
	}
	if err := db.Exec("CREATE TABLE projects (id integer primary key, name text)").Error; err != nil {
		t.Fatalf("创建项目测试表失败：%v", err)
	}
	if err := db.Exec("INSERT INTO projects (id, name) VALUES (?, ?)", 3, "事务项目").Error; err != nil {
		t.Fatalf("准备项目快照失败：%v", err)
	}
	return db
}

// TestCreateSourceEncryptsCredentialAndDefaultsInterval 验证凭证不以明文落库且同步周期默认为一小时。
func TestCreateSourceEncryptsCredentialAndDefaultsInterval(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	_ = db.AutoMigrate(&Source{})
	cipher := NewCredentialCipher("source-test-deployment-key")
	service := NewService(NewRepository(db), cipher, identityTestAdapters())
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
	service := NewService(NewRepository(db), NewCredentialCipher("source-provider-key"), identityTestAdapters())
	_, err := service.CreateSource(context.Background(), CreateSourceInput{ProjectID: 7, Provider: "kubernetes", Name: "旧集群", Credential: json.RawMessage(`{"token":"value"}`)})
	if err == nil {
		t.Fatal("已移除的 Kubernetes provider 必须被拒绝")
	}
}

// TestUpdateSourceKeepsCredentialAndDeleteCascades 验证未提交新凭证时保留密文，并可删除项目内接入源。
func TestUpdateSourceKeepsCredentialAndDeleteCascades(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	_ = db.Exec("PRAGMA foreign_keys = ON").Error
	_ = db.AutoMigrate(&Source{}, &Server{}, &Database{}, &LoadBalancer{}, &SyncJob{}, &audit.Log{})
	service := NewService(NewRepository(db), NewCredentialCipher("source-update-key"), identityTestAdapters())
	created, err := service.CreateSource(context.Background(), CreateSourceInput{ProjectID: 3, Provider: ProviderAWS, Name: "旧名称", Credential: json.RawMessage(`{"access_key_id":"虚构标识","secret_access_key":"虚构密钥"}`)})
	if err != nil {
		t.Fatal("准备接入源失败")
	}
	originalCiphertext := created.EncryptedCredential
	updated, err := service.UpdateSource(context.Background(), 3, created.ID, UpdateSourceInput{Name: "新名称", Region: "ap-east-1", Config: json.RawMessage(`{}`), Enabled: false, SyncIntervalMinutes: 120})
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
	service := NewService(NewRepository(db), NewCredentialCipher("job-list-key"), identityTestAdapters())
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
	service := NewService(NewRepository(db), NewCredentialCipher("source-test-key"), identityTestAdapters())
	service.adapters = map[string]ProviderAdapter{ProviderAWS: identityAdapterStub{resolve: func(_ context.Context, source Source, _ []byte) (string, error) { return source.Name, nil }}, ProviderAliyun: identityAdapterStub{aliyun: true}}
	for _, input := range []CreateSourceInput{{ProjectID: 1, Provider: ProviderAWS, Name: "AWS"}, {ProjectID: 1, Provider: ProviderAliyun, Name: "阿里云"}, {ProjectID: 2, Provider: ProviderAWS, Name: "其他项目"}} {
		input.Credential = json.RawMessage(`{"access_key_id":"虚构标识","secret_access_key":"虚构密钥"}`)
		if input.Provider == ProviderAliyun {
			input.Credential = json.RawMessage(`{"access_key_id":"虚构标识","access_key_secret":"虚构密钥"}`)
		}
		if _, err := service.CreateSource(context.Background(), input); err != nil {
			t.Fatal("准备接入源失败")
		}
	}
	values, err := service.ListSources(context.Background(), 1, ProviderAWS)
	if err != nil || len(values) != 1 || values[0].Name != "AWS" {
		t.Fatal("接入源列表未正确执行项目和平台过滤")
	}
}
