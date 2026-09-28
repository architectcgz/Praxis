import { FormEvent } from 'react'
import { SessionSummary } from '../../api'

export type ProjectGroup = {
    id: string
    workspaceID: string
    name: string
    path: string
    sessions: SessionSummary[]
}

export type ProjectPanelProps = {
    projectGroups: ProjectGroup[]
    sessionsCount: number
    selectedSessionID: string
    selectedProjectID: string
    lookupID: string
    searchOpen: boolean
    collapsedProjects: Record<string, boolean>
    busy: boolean
    bridgeAvailable: boolean
    onLookupChange: (value: string) => void
    onLoadSession: (event: FormEvent<HTMLFormElement>) => void
    onToggleSearch: () => void
    onOpenProject: () => void
    onOpenSession: (project: ProjectGroup) => void
    onSelectProject: (project: ProjectGroup) => void
    onSelectSession: (id: string) => void
    onToggleProject: (key: string) => void
    onOpenSettings: () => void
    settingsOpen: boolean
}
