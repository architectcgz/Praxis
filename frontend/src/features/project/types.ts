import type { SubmitEvent } from 'react'
import type { SessionSummary } from '../../api'

export type ProjectGroup = {
    id: string
    name: string
    path: string
    sessions: SessionSummary[]
}

export type ProjectPanelProps = {
    projectGroups: ProjectGroup[]
    sessionsCount: number
    selectedSessionID: string
    selectedProjectID: string
    activeProjectID: string
    lookupID: string
    searchOpen: boolean
    collapsedProjects: Record<string, boolean>
    busy: boolean
    bridgeAvailable: boolean
    onLookupChange: (value: string) => void
    onLoadSession: (event: SubmitEvent<HTMLFormElement>) => void
    onToggleSearch: () => void
    onOpenProject: () => void
    onOpenSession: (project: ProjectGroup) => void
    onDeleteSession: (id: string) => Promise<boolean>
    onRenameSession: (id: string, title: string) => Promise<boolean>
    onRevealSessionFile: (id: string) => Promise<boolean>
    onSelectProject: (project: ProjectGroup) => void
    onSelectSession: (id: string) => void
    onToggleProject: (key: string) => void
    /** 收起同层项目目录侧栏。 */
    onCloseNavigation?: () => void
}
