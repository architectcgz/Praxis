import { useSyncExternalStore, type ReactNode } from 'react'
import { LoaderCircle } from 'lucide-react'
import type { Entry, AgentView } from '../../api'
import type { AgentOutputSource } from './agentStore'
import { CollaborationViews } from './output/CollaborationViews'
import { AgentConversation } from './AgentConversation'
import { FilePreviewProvider } from './output/FilePreview'
import { ModelUsageProvider } from './SessionTokenUsage'

type AgentSessionPanelProps = {
    branchId: string
    historyEntryId: string | null
    submissionRevision: number
    usageRevision: number
    loading: boolean
    agents: AgentView[]
    agent: AgentView | null
    entries: Entry[]
    agentEntries: Record<string, Entry[]>
    agentEntriesErrors: Record<string, string>
    outputStore: AgentOutputSource
    onSelectAgent: (id: string) => void
    entriesError: string
    hasEarlier: boolean
    historyLoading: boolean
    onLoadEarlier: () => Promise<void>
    onRetryHistory: () => Promise<void>
    busy: boolean
    composer: ReactNode
}

/** 展示当前 Session 中选中的 Agent 对话和输入控件。 */
export function AgentSessionPanel({ branchId, historyEntryId, submissionRevision, usageRevision, loading, agents, agent, entries, agentEntries, agentEntriesErrors, outputStore, onSelectAgent, entriesError, hasEarlier, historyLoading, onLoadEarlier, onRetryHistory, busy, composer }: AgentSessionPanelProps) {
    const { streamingOutputs } = useSyncExternalStore(outputStore.subscribeOutput, outputStore.getOutput)
    if (loading) {
        return <div className="agent-loading" aria-busy="true"><LoaderCircle size={18} className="is-spinning" aria-hidden="true" /><span>加载对话中</span></div>
    }
    if (!agent) {
        return composer
    }

    const historical = historyEntryId !== null
    const hasSubagents = agents.some((item) => item.Kind === 'subagent')
    return (
        <ModelUsageProvider key={`${agent.SessionID}-${branchId}`} sessionId={agent.SessionID} revision={usageRevision} entries={entries}>
        <FilePreviewProvider sessionId={agent.SessionID}>
        <AgentConversation
            key={`${agent.SessionID}-${branchId}`}
            branchId={branchId}
            historyEntryId={historyEntryId}
            submissionRevision={submissionRevision}
            agent={agent}
            entries={entries}
            collaboration={!historical && agent.Kind === 'main' && hasSubagents ? (
                <CollaborationViews
                    agents={agents}
                    agentEntries={agentEntries}
                    entriesErrors={agentEntriesErrors}
                    streamingOutputs={streamingOutputs}
                    onSelectAgent={onSelectAgent}
                />
            ) : undefined}
            streamingOutput={historical ? null : streamingOutputs[agent.ID] || null}
            entriesError={entriesError}
            hasEarlier={hasEarlier} historyLoading={historyLoading} onLoadEarlier={onLoadEarlier} onRetryHistory={onRetryHistory}
            busy={busy}
            composer={composer}
        />
        </FilePreviewProvider>
        </ModelUsageProvider>
    )
}
