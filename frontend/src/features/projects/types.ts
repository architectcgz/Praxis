import {FormEvent} from 'react'
import {SessionSnapshot, SessionSummary} from '../../shared/api'

export type ProjectGroup = {
    key: string
    name: string
    path: string
    sessions: SessionSummary[]
}

export type ProjectPanelProps = {
    projectGroups: ProjectGroup[]
    sessionsCount: number
    selectedSessionID: string
    lookupID: string
    searchOpen: boolean
    collapsedProjects: Record<string, boolean>
    busy: boolean
    bridgeAvailable: boolean
    healthReady: boolean
    onLookupChange: (value: string) => void
    onLoadSession: (event: FormEvent<HTMLFormElement>) => void
    onToggleSearch: () => void
    onOpenProject: () => void
    onOpenSession: (workspaceKey: string) => void
    onSelectSession: (id: string) => void
    onToggleProject: (key: string) => void
}

export type AgentsPanelProps = {
    loading: boolean
    session: SessionSnapshot | null
    selectedAgentID: string
    onSelectAgent: (id: string) => void
}

export type StreamingOutput = {
    executionId: string
    content: string
}
