import { SessionSnapshot } from '../../api'

export type AgentsPanelProps = {
    loading: boolean
    session: SessionSnapshot | null
    selectedAgentID: string
    onSelectAgent: (id: string) => void
}
