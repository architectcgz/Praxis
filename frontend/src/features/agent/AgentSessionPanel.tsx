import { useSyncExternalStore, type ReactNode } from 'react'
import { LoaderCircle } from 'lucide-react'
import type { HistoryMessage, AgentView } from '../../api'
import type { AgentOutputSource } from '../session/sessionStore'
import { CollaborationViews } from './output/CollaborationViews'
import { AgentConversation } from './AgentConversation'
import type { RequestActivities } from './history/entries'

type AgentSessionPanelProps = {
    submissionRevision: number
    loading: boolean
    agents: AgentView[]
    agent: AgentView | null
    entries: HistoryMessage[]
    requests: RequestActivities
    agentEntries: Record<string, HistoryMessage[]>
    agentEntriesErrors: Record<string, string>
    outputStore: AgentOutputSource
    onSelectAgent: (id: string) => void
    onStop?: () => void
    entriesError: string
    hasEarlier: boolean
    historyLoading: boolean
    onLoadEarlier: () => Promise<void>
    onRetryHistory: () => Promise<void>
    busy: boolean
    composer: ReactNode
}

/** 展示当前 Session 中选中的 Agent 对话和输入控件。 */
export function AgentSessionPanel({ submissionRevision, loading, agents, agent, entries, requests, agentEntries, agentEntriesErrors, outputStore, onSelectAgent, onStop, entriesError, hasEarlier, historyLoading, onLoadEarlier, onRetryHistory, busy, composer }: AgentSessionPanelProps) {
    const { streamingOutputs } = useSyncExternalStore(outputStore.subscribeOutput, outputStore.getOutput)
    if (loading) {
        return <div className="agent-loading" aria-busy="true"><LoaderCircle size={18} className="is-spinning" aria-hidden="true" /><span>加载对话中</span></div>
    }
    if (!agent) {
        return composer
    }

    const hasSubagents = agents.some((item) => item.Kind === 'subagent')
    return (
        <AgentConversation
            key={agent.SessionID}
            submissionRevision={submissionRevision}
            agent={agent}
            entries={entries}
            requests={requests}
            collaboration={agent.Kind === 'main' && hasSubagents ? (
                <CollaborationViews
                    agents={agents}
                    agentEntries={agentEntries}
                    entriesErrors={agentEntriesErrors}
                    streamingOutputs={streamingOutputs}
                    onSelectAgent={onSelectAgent}
                />
            ) : undefined}
            onReturnToMain={agent.Kind === 'subagent' ? () => onSelectAgent(agent.SessionID) : undefined}
            onOpenSubagent={onSelectAgent}
            onStop={onStop}
            streamingOutput={streamingOutputs[agent.ID] || null}
            entriesError={entriesError}
            hasEarlier={hasEarlier} historyLoading={historyLoading} onLoadEarlier={onLoadEarlier} onRetryHistory={onRetryHistory}
            busy={busy}
            composer={composer}
        />
    )
}
