import type { SubmitEvent } from 'react'
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
    onLoadSession: (event: SubmitEvent<HTMLFormElement>) => void
    onToggleSearch: () => void
    onOpenProject: () => void
    onOpenSession: (project: ProjectGroup) => void
    onDeleteSession: (id: string) => Promise<boolean>
    onRenameSession: (id: string, title: string) => Promise<boolean>
    onSelectProject: (project: ProjectGroup) => void
    onSelectSession: (id: string) => void
    onToggleProject: (key: string) => void
    onOpenSettings: () => void
    settingsOpen: boolean
    /** 抽屉模式的关闭操作；桌面侧栏不传入。 */
    onCloseNavigation?: () => void
}
