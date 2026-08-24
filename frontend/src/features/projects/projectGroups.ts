import {SessionSummary} from '../../shared/api'
import {ProjectGroup} from './types'

export function groupSessions(items: SessionSummary[]): ProjectGroup[] {
    const groups = new Map<string, ProjectGroup>()
    for (const item of items) {
        const key = item.workspaceKey || 'unknown-workspace'
        const path = item.workspaceKey || 'Workspace not recorded'
        const current = groups.get(key)
        if (current) {
            current.sessions.push(item)
            continue
        }
        groups.set(key, {
            key,
            name: projectName(path),
            path,
            sessions: [item],
        })
    }
    return Array.from(groups.values())
}

export function projectNameFromWorkspace(path: string) {
    return projectName(path || 'Project')
}

function projectName(path: string) {
    const normalized = path.replace(/[\\/]+$/, '')
    const parts = normalized.split(/[\\/]/)
    return parts[parts.length - 1] || normalized || 'Workspace'
}
