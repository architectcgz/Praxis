import { ProjectSummary, SessionSummary } from '../../api'
import { ProjectGroup } from './types'

export function groupSessions(items: SessionSummary[], projects: ProjectSummary[] = []): ProjectGroup[] {
    const groups = new Map<string, ProjectGroup>()
    for (const project of projects) {
        groups.set(project.id, {
            id: project.id,
            workspaceID: project.defaultWorkspaceId,
            name: project.name,
            path: project.path,
            sessions: [],
        })
    }
    for (const item of items) {
        const id = item.projectId
        const current = groups.get(id)
        if (current) {
            current.sessions.push(item)
            continue
        }
        groups.set(id, {
            id,
            workspaceID: item.workspaceId,
            name: '项目',
            path: '',
            sessions: [item],
        })
    }
    return Array.from(groups.values())
}
