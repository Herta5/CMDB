/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type {
  AddProjectMemberRequest,
  AuditLogPage,
  ChangeMyPasswordRequest,
  ConnectionTestResult,
  CreateProjectRequest,
  CreateSourceRequest,
  CreateUserRequest,
  Health,
  ListAllResourcesParams,
  ListGlobalAuditLogsParams,
  ListProjectAuditLogsParams,
  ListProjectResourcesParams,
  ListSourcesParams,
  ListSyncJobsParams,
  LoginRequest,
  LoginResponse,
  MemberCandidate,
  Project,
  ProjectMember,
  PublicUser,
  ResourcePage,
  Source,
  SyncJob,
  SyncJobPage,
  UpdateMyProfileRequest,
  UpdateProjectMemberRoleRequest,
  UpdateProjectRequest,
  UpdateSourceRequest,
  UpdateUserRequest,
  UpdateUserStatusRequest
} from './models';

import { apiTransport } from '../transport';
type SecondParameter<T extends (...args: never) => unknown> = Parameters<T>[1];


  /**
 * @summary 检查进程存活
 */
export const health = (

 options?: SecondParameter<typeof apiTransport<Health>>,) => {
      return apiTransport<Health>(
      {url: `/health`, method: 'GET'
    },
      options);
    }

/**
 * @summary 使用用户名和密码登录
 */
export const login = (
    loginRequest: LoginRequest,
 options?: SecondParameter<typeof apiTransport<LoginResponse>>,) => {
      return apiTransport<LoginResponse>(
      {url: `/api/v1/auth/login`, method: 'POST',
      headers: {'Content-Type': 'application/json', },
      data: loginRequest
    },
      options);
    }

/**
 * @summary 查询当前公开身份
 */
export const getMe = (

 options?: SecondParameter<typeof apiTransport<PublicUser>>,) => {
      return apiTransport<PublicUser>(
      {url: `/api/v1/me`, method: 'GET'
    },
      options);
    }

/**
 * @summary 修改本人的显示名称
 */
export const updateMyProfile = (
    updateMyProfileRequest: UpdateMyProfileRequest,
 options?: SecondParameter<typeof apiTransport<PublicUser>>,) => {
      return apiTransport<PublicUser>(
      {url: `/api/v1/me/profile`, method: 'PUT',
      headers: {'Content-Type': 'application/json', },
      data: updateMyProfileRequest
    },
      options);
    }

/**
 * @summary 校验当前密码并修改本人密码，使所有旧会话失效
 */
export const changeMyPassword = (
    changeMyPasswordRequest: ChangeMyPasswordRequest,
 options?: SecondParameter<typeof apiTransport<void>>,) => {
      return apiTransport<void>(
      {url: `/api/v1/me/password`, method: 'PUT',
      headers: {'Content-Type': 'application/json', },
      data: changeMyPasswordRequest
    },
      options);
    }

/**
 * @summary 查询公开用户列表
 */
export const listUsers = (

 options?: SecondParameter<typeof apiTransport<PublicUser[]>>,) => {
      return apiTransport<PublicUser[]>(
      {url: `/api/v1/users`, method: 'GET'
    },
      options);
    }

/**
 * @summary 创建用户
 */
export const createUser = (
    createUserRequest: CreateUserRequest,
 options?: SecondParameter<typeof apiTransport<PublicUser>>,) => {
      return apiTransport<PublicUser>(
      {url: `/api/v1/users`, method: 'POST',
      headers: {'Content-Type': 'application/json', },
      data: createUserRequest
    },
      options);
    }

/**
 * @summary 更新用户资料与授权
 */
export const updateUser = (
    username: string,
    updateUserRequest: UpdateUserRequest,
 options?: SecondParameter<typeof apiTransport<PublicUser>>,) => {
      return apiTransport<PublicUser>(
      {url: `/api/v1/users/${username}`, method: 'PUT',
      headers: {'Content-Type': 'application/json', },
      data: updateUserRequest
    },
      options);
    }

/**
 * @summary 删除其他用户
 */
export const deleteUser = (
    username: string,
 options?: SecondParameter<typeof apiTransport<void>>,) => {
      return apiTransport<void>(
      {url: `/api/v1/users/${username}`, method: 'DELETE'
    },
      options);
    }

/**
 * @summary 启用或停用用户
 */
export const updateUserStatus = (
    username: string,
    updateUserStatusRequest: UpdateUserStatusRequest,
 options?: SecondParameter<typeof apiTransport<PublicUser>>,) => {
      return apiTransport<PublicUser>(
      {url: `/api/v1/users/${username}/status`, method: 'PUT',
      headers: {'Content-Type': 'application/json', },
      data: updateUserStatusRequest
    },
      options);
    }

/**
 * @summary 查询可见业务项目
 */
export const listProjects = (

 options?: SecondParameter<typeof apiTransport<Project[]>>,) => {
      return apiTransport<Project[]>(
      {url: `/api/v1/projects`, method: 'GET'
    },
      options);
    }

/**
 * @summary 创建业务项目
 */
export const createProject = (
    createProjectRequest: CreateProjectRequest,
 options?: SecondParameter<typeof apiTransport<Project>>,) => {
      return apiTransport<Project>(
      {url: `/api/v1/projects`, method: 'POST',
      headers: {'Content-Type': 'application/json', },
      data: createProjectRequest
    },
      options);
    }

/**
 * @summary 查询业务项目
 */
export const getProject = (
    id: number,
 options?: SecondParameter<typeof apiTransport<Project>>,) => {
      return apiTransport<Project>(
      {url: `/api/v1/projects/${id}`, method: 'GET'
    },
      options);
    }

/**
 * @summary 更新业务项目
 */
export const updateProject = (
    id: number,
    updateProjectRequest: UpdateProjectRequest,
 options?: SecondParameter<typeof apiTransport<Project>>,) => {
      return apiTransport<Project>(
      {url: `/api/v1/projects/${id}`, method: 'PUT',
      headers: {'Content-Type': 'application/json', },
      data: updateProjectRequest
    },
      options);
    }

/**
 * @summary 删除无依赖业务项目
 */
export const deleteProject = (
    id: number,
 options?: SecondParameter<typeof apiTransport<void>>,) => {
      return apiTransport<void>(
      {url: `/api/v1/projects/${id}`, method: 'DELETE'
    },
      options);
    }

/**
 * @summary 查询项目成员
 */
export const listProjectMembers = (
    id: number,
 options?: SecondParameter<typeof apiTransport<ProjectMember[]>>,) => {
      return apiTransport<ProjectMember[]>(
      {url: `/api/v1/projects/${id}/members`, method: 'GET'
    },
      options);
    }

/**
 * @summary 添加项目成员
 */
export const addProjectMember = (
    id: number,
    addProjectMemberRequest: AddProjectMemberRequest,
 options?: SecondParameter<typeof apiTransport<ProjectMember>>,) => {
      return apiTransport<ProjectMember>(
      {url: `/api/v1/projects/${id}/members`, method: 'POST',
      headers: {'Content-Type': 'application/json', },
      data: addProjectMemberRequest
    },
      options);
    }

/**
 * @summary 修改项目成员角色
 */
export const updateProjectMemberRole = (
    id: number,
    username: string,
    updateProjectMemberRoleRequest: UpdateProjectMemberRoleRequest,
 options?: SecondParameter<typeof apiTransport<ProjectMember>>,) => {
      return apiTransport<ProjectMember>(
      {url: `/api/v1/projects/${id}/members/${username}`, method: 'PUT',
      headers: {'Content-Type': 'application/json', },
      data: updateProjectMemberRoleRequest
    },
      options);
    }

/**
 * @summary 移除项目成员
 */
export const removeProjectMember = (
    id: number,
    username: string,
 options?: SecondParameter<typeof apiTransport<void>>,) => {
      return apiTransport<void>(
      {url: `/api/v1/projects/${id}/members/${username}`, method: 'DELETE'
    },
      options);
    }

/**
 * @summary 查询可添加成员最小资料
 */
export const listProjectMemberCandidates = (
    id: number,
 options?: SecondParameter<typeof apiTransport<MemberCandidate[]>>,) => {
      return apiTransport<MemberCandidate[]>(
      {url: `/api/v1/projects/${id}/member-candidates`, method: 'GET'
    },
      options);
    }

/**
 * @summary 查询全局及全部项目审计
 */
export const listGlobalAuditLogs = (
    params?: ListGlobalAuditLogsParams,
 options?: SecondParameter<typeof apiTransport<AuditLogPage>>,) => {
      return apiTransport<AuditLogPage>(
      {url: `/api/v1/audit-logs`, method: 'GET',
        params
    },
      options);
    }

/**
 * @summary 查询项目审计
 */
export const listProjectAuditLogs = (
    id: number,
    params?: ListProjectAuditLogsParams,
 options?: SecondParameter<typeof apiTransport<AuditLogPage>>,) => {
      return apiTransport<AuditLogPage>(
      {url: `/api/v1/projects/${id}/audit-logs`, method: 'GET',
        params
    },
      options);
    }

/**
 * @summary 跨项目查询资产
 */
export const listAllResources = (
    params?: ListAllResourcesParams,
 options?: SecondParameter<typeof apiTransport<ResourcePage>>,) => {
      return apiTransport<ResourcePage>(
      {url: `/api/v1/resources`, method: 'GET',
        params
    },
      options);
    }

/**
 * @summary 查询项目资产
 */
export const listProjectResources = (
    id: number,
    params?: ListProjectResourcesParams,
 options?: SecondParameter<typeof apiTransport<ResourcePage>>,) => {
      return apiTransport<ResourcePage>(
      {url: `/api/v1/projects/${id}/resources`, method: 'GET',
        params
    },
      options);
    }

/**
 * @summary 查询项目接入源
 */
export const listSources = (
    id: number,
    params?: ListSourcesParams,
 options?: SecondParameter<typeof apiTransport<Source[] | null>>,) => {
      return apiTransport<Source[] | null>(
      {url: `/api/v1/projects/${id}/sources`, method: 'GET',
        params
    },
      options);
    }

/**
 * @summary 验证云账号并创建接入源
 */
export const createSource = (
    id: number,
    createSourceRequest: CreateSourceRequest,
 options?: SecondParameter<typeof apiTransport<Source>>,) => {
      return apiTransport<Source>(
      {url: `/api/v1/projects/${id}/sources`, method: 'POST',
      headers: {'Content-Type': 'application/json', },
      data: createSourceRequest
    },
      options);
    }

/**
 * @summary 更新接入源并可选替换凭证
 */
export const updateSource = (
    id: number,
    sourceId: number,
    updateSourceRequest: UpdateSourceRequest,
 options?: SecondParameter<typeof apiTransport<Source>>,) => {
      return apiTransport<Source>(
      {url: `/api/v1/projects/${id}/sources/${sourceId}`, method: 'PUT',
      headers: {'Content-Type': 'application/json', },
      data: updateSourceRequest
    },
      options);
    }

/**
 * @summary 删除无依赖接入源
 */
export const deleteSource = (
    id: number,
    sourceId: number,
 options?: SecondParameter<typeof apiTransport<void>>,) => {
      return apiTransport<void>(
      {url: `/api/v1/projects/${id}/sources/${sourceId}`, method: 'DELETE'
    },
      options);
    }

/**
 * @summary 创建手工同步排队任务
 */
export const syncSource = (
    id: number,
    sourceId: number,
 options?: SecondParameter<typeof apiTransport<SyncJob>>,) => {
      return apiTransport<SyncJob>(
      {url: `/api/v1/projects/${id}/sources/${sourceId}/sync`, method: 'POST'
    },
      options);
    }

/**
 * @summary 测试各资源类型的只读访问能力
 */
export const testSourceConnection = (
    id: number,
    sourceId: number,
 options?: SecondParameter<typeof apiTransport<ConnectionTestResult>>,) => {
      return apiTransport<ConnectionTestResult>(
      {url: `/api/v1/projects/${id}/sources/${sourceId}/test`, method: 'POST'
    },
      options);
    }

/**
 * @summary 查询同步任务历史
 */
export const listSyncJobs = (
    id: number,
    params?: ListSyncJobsParams,
 options?: SecondParameter<typeof apiTransport<SyncJobPage>>,) => {
      return apiTransport<SyncJobPage>(
      {url: `/api/v1/projects/${id}/sync-jobs`, method: 'GET',
        params
    },
      options);
    }

/**
 * @summary 新建任务重试失败或部分成功同步
 */
export const retrySyncJob = (
    id: number,
    jobId: number,
 options?: SecondParameter<typeof apiTransport<SyncJob>>,) => {
      return apiTransport<SyncJob>(
      {url: `/api/v1/projects/${id}/sync-jobs/${jobId}/retry`, method: 'POST'
    },
      options);
    }

export type HealthResult = NonNullable<Awaited<ReturnType<typeof health>>>
export type LoginResult = NonNullable<Awaited<ReturnType<typeof login>>>
export type GetMeResult = NonNullable<Awaited<ReturnType<typeof getMe>>>
export type UpdateMyProfileResult = NonNullable<Awaited<ReturnType<typeof updateMyProfile>>>
export type ChangeMyPasswordResult = NonNullable<Awaited<ReturnType<typeof changeMyPassword>>>
export type ListUsersResult = NonNullable<Awaited<ReturnType<typeof listUsers>>>
export type CreateUserResult = NonNullable<Awaited<ReturnType<typeof createUser>>>
export type UpdateUserResult = NonNullable<Awaited<ReturnType<typeof updateUser>>>
export type DeleteUserResult = NonNullable<Awaited<ReturnType<typeof deleteUser>>>
export type UpdateUserStatusResult = NonNullable<Awaited<ReturnType<typeof updateUserStatus>>>
export type ListProjectsResult = NonNullable<Awaited<ReturnType<typeof listProjects>>>
export type CreateProjectResult = NonNullable<Awaited<ReturnType<typeof createProject>>>
export type GetProjectResult = NonNullable<Awaited<ReturnType<typeof getProject>>>
export type UpdateProjectResult = NonNullable<Awaited<ReturnType<typeof updateProject>>>
export type DeleteProjectResult = NonNullable<Awaited<ReturnType<typeof deleteProject>>>
export type ListProjectMembersResult = NonNullable<Awaited<ReturnType<typeof listProjectMembers>>>
export type AddProjectMemberResult = NonNullable<Awaited<ReturnType<typeof addProjectMember>>>
export type UpdateProjectMemberRoleResult = NonNullable<Awaited<ReturnType<typeof updateProjectMemberRole>>>
export type RemoveProjectMemberResult = NonNullable<Awaited<ReturnType<typeof removeProjectMember>>>
export type ListProjectMemberCandidatesResult = NonNullable<Awaited<ReturnType<typeof listProjectMemberCandidates>>>
export type ListGlobalAuditLogsResult = NonNullable<Awaited<ReturnType<typeof listGlobalAuditLogs>>>
export type ListProjectAuditLogsResult = NonNullable<Awaited<ReturnType<typeof listProjectAuditLogs>>>
export type ListAllResourcesResult = NonNullable<Awaited<ReturnType<typeof listAllResources>>>
export type ListProjectResourcesResult = NonNullable<Awaited<ReturnType<typeof listProjectResources>>>
export type ListSourcesResult = NonNullable<Awaited<ReturnType<typeof listSources>>>
export type CreateSourceResult = NonNullable<Awaited<ReturnType<typeof createSource>>>
export type UpdateSourceResult = NonNullable<Awaited<ReturnType<typeof updateSource>>>
export type DeleteSourceResult = NonNullable<Awaited<ReturnType<typeof deleteSource>>>
export type SyncSourceResult = NonNullable<Awaited<ReturnType<typeof syncSource>>>
export type TestSourceConnectionResult = NonNullable<Awaited<ReturnType<typeof testSourceConnection>>>
export type ListSyncJobsResult = NonNullable<Awaited<ReturnType<typeof listSyncJobs>>>
export type RetrySyncJobResult = NonNullable<Awaited<ReturnType<typeof retrySyncJob>>>
