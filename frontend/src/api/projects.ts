import { getProjectBinding } from './bindings'

export type CreateProjectRequest = { projectName: string }

export type CreateProjectResponse = {
    projectId: string
    name: string
    workspaceId: string
    path: string
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
