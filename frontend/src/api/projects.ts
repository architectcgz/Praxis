import { getProjectBinding } from './bindings'
import type { dto } from '../../wailsjs/go/models'

export type CreateProjectRequest = {
    projectId: string
    workspaceId: string
    projectName: string
    path: string
    requestId: string
}

export type ProjectSummary = {
    id: string
    name: string
    defaultWorkspaceId: string
    path: string
    state: string
}

export function listProjects() {
    return getProjectBinding().ListProjects()
}

export function createProject(request: CreateProjectRequest) {
    return getProjectBinding().CreateProject(request)
}

export function selectProjectPath() {
    return getProjectBinding().SelectProjectPath()
}

export type ApprovalMode = 'ask' | 'auto' | 'yolo'
export type ProjectApprovalMode = {
    projectId: string
    globalMode: ApprovalMode
    projectMode: ApprovalMode | null
    effectiveMode: ApprovalMode
}

function approvalMode(value: string): ApprovalMode {
    if (value !== 'ask' && value !== 'auto' && value !== 'yolo') throw new Error('后端返回了未知的 Approval mode')
    return value
}

function projectApprovalMode(value: dto.ProjectApprovalMode): ProjectApprovalMode {
    return {
        projectId: value.projectId,
        globalMode: approvalMode(value.globalMode),
        projectMode: value.projectMode == null ? null : approvalMode(value.projectMode),
        effectiveMode: approvalMode(value.effectiveMode),
    }
}

/** 读取后端解析的项目选择和有效模式，不在前端推断授权结果。 */
export async function getProjectApprovalMode(projectId: string): Promise<ProjectApprovalMode> {
    return projectApprovalMode(await getProjectBinding().GetProjectApprovalMode(projectId))
}

/** 保存项目覆盖；null 恢复继承，只有后端提交成功才返回新选择。 */
export async function setProjectApprovalMode(projectId: string, mode: ApprovalMode | null): Promise<ProjectApprovalMode> {
    return projectApprovalMode(await getProjectBinding().SetProjectApprovalMode({ projectId, mode: mode ?? undefined }))
}
