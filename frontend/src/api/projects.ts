import { getProjectBinding } from './bindings'

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

export async function listProjects() {
    return await getProjectBinding().ListProjects() ?? []
}

export function createProject(request: CreateProjectRequest) {
    return getProjectBinding().CreateProject(request)
}

export function selectProjectPath() {
    return getProjectBinding().SelectProjectPath()
}
