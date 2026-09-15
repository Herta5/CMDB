// 本文件逐字段转换公开 DTO，模型新增字段不能扩大公开或凭证暴露范围。
package api

import (
	"cmdb/internal/api/generated"
	"cmdb/internal/audit"
	"cmdb/internal/identity"
	"cmdb/internal/project"
	"cmdb/internal/resource"
	"encoding/json"
)

func toPermissions(values *[]generated.ProjectPermissionInput) []identity.ProjectPermission {
	if values == nil {
		return nil
	}
	result := make([]identity.ProjectPermission, 0, len(*values))
	for _, v := range *values {
		result = append(result, identity.ProjectPermission{ProjectID: v.ProjectId, ProjectName: value(v.ProjectName), Role: string(v.Role)})
	}
	return result
}
func toUser(v *identity.User) generated.PublicUser {
	var permissions *[]generated.ProjectPermission
	if v.ProjectPermissions != nil {
		items := make([]generated.ProjectPermission, 0, len(v.ProjectPermissions))
		for _, p := range v.ProjectPermissions {
			items = append(items, generated.ProjectPermission{ProjectId: p.ProjectID, ProjectName: p.ProjectName, Role: generated.ProjectRole(p.Role)})
		}
		permissions = &items
	}
	return generated.PublicUser{Username: v.Username, DisplayName: v.DisplayName, Email: v.Email, GlobalRole: generated.GlobalRole(v.GlobalRole), Status: generated.UserStatus(v.Status), ProjectPermissions: permissions}
}
func toProject(v *project.Project) generated.Project {
	var owner *string
	if v.OwnerUser != nil {
		owner = &v.OwnerUser.Username
	}
	return generated.Project{Id: v.ID, Code: v.Code, Name: v.Name, Description: v.Description, Status: generated.ProjectStatus(v.Status), OwnerUsername: owner, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, CurrentRole: nonzero(generated.ProjectRole(v.CurrentRole))}
}
func toMember(v *project.MemberRole) generated.ProjectMember {
	r := generated.ProjectMember{Role: generated.ProjectRole(v.Role)}
	if v.User != nil {
		r.Username = v.User.Username
		r.DisplayName = v.User.DisplayName
	}
	return r
}
func toSource(v *resource.Source) generated.Source {
	return generated.Source{Id: v.ID, ProjectId: v.ProjectID, Provider: generated.Provider(v.Provider), Name: v.Name, Region: v.Region, CredentialHint: v.CredentialHint, Config: v.Config, Enabled: v.Enabled, SyncIntervalMinutes: v.SyncIntervalMinutes, LastSyncAt: v.LastSyncAt, NextSyncAt: v.NextSyncAt, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func toResource(v resource.Resource) generated.Resource {
	var endpoints *[]generated.Endpoint
	if v.Endpoints != nil {
		items := make([]generated.Endpoint, 0, len(v.Endpoints))
		for _, p := range v.Endpoints {
			var ips *[]string
			if p.ResolvedIPs != nil {
				ips = &p.ResolvedIPs
			}
			items = append(items, generated.Endpoint{Address: p.Address, Kind: generated.EndpointKind(p.Kind), Port: p.Port, Protocol: p.Protocol, ResolvedIps: ips})
		}
		endpoints = &items
	}
	var disks *[]generated.ServerDisk
	if v.Disks != nil {
		items := make([]generated.ServerDisk, 0, len(v.Disks))
		for _, d := range v.Disks {
			items = append(items, generated.ServerDisk{Id: d.ID, Kind: generated.ServerDiskKind(d.Kind), Type: d.Type, SizeGib: d.SizeGiB, Device: d.Device, Encrypted: d.Encrypted})
		}
		disks = &items
	}
	return generated.Resource{Id: v.ID, ProjectId: v.ProjectID, SourceId: v.SourceID, Provider: generated.Provider(v.Provider), ResourceType: generated.ResourceType(v.ResourceType), ExternalId: v.ExternalID, Name: v.Name, Region: v.Region, Zone: v.Zone, CloudStatus: v.CloudStatus, AssetStatus: generated.ResourceAssetStatus(v.AssetStatus), FirstSeenAt: v.FirstSeenAt, LastSeenAt: v.LastSeenAt, MissingSince: v.MissingSince, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, SourceName: v.SourceName, ProjectName: nonzero(v.ProjectName), Engine: nonzero(v.Engine), EngineVersion: nonzero(v.EngineVersion), NetworkType: nonzero(v.NetworkType), InstanceType: nonzero(v.InstanceType), Vcpu: nonzero(v.VCPU), Memory: nonzero(v.Memory), StorageType: nonzero(v.StorageType), StorageSizeGib: v.StorageSizeGiB, Endpoints: endpoints, Disks: disks}
}
func toJob(v *resource.SyncJob) generated.SyncJob {
	// 统计是持久化 JSON 值，按公开字段解码一次；不对领域模型做 JSON 往返。
	var stats *generated.SyncStatistics
	if len(v.Statistics) > 0 {
		_ = json.Unmarshal(v.Statistics, &stats)
	}
	return generated.SyncJob{Id: v.ID, ProjectId: v.ProjectID, SourceId: v.SourceID, PreviousJobId: v.PreviousJobID, Status: generated.SyncJobStatus(v.Status), Trigger: generated.SyncJobTrigger(v.Trigger), Statistics: stats, ErrorSummary: v.ErrorSummary, StartedAt: v.StartedAt, FinishedAt: v.FinishedAt}
}
func toAuditPage(v audit.Page) generated.AuditLogPage {
	items := make([]generated.AuditLog, 0, len(v.Items))
	for _, a := range v.Items {
		items = append(items, generated.AuditLog{Id: a.ID, ActorUsername: a.ActorUsername, ActorDisplayName: a.ActorDisplayName, ProjectId: a.ProjectID, ProjectName: a.ProjectName, Action: a.Action, ResourceType: a.ResourceType, ResourceId: a.ResourceID, ResourceName: a.ResourceName, Detail: a.Detail, RequestIp: a.RequestIP, CreatedAt: a.CreatedAt})
	}
	return generated.AuditLogPage{Items: items, Total: v.Total, Page: v.Page, PageSize: v.PageSize, SnapshotId: v.SnapshotID}
}
