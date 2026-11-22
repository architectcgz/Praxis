import { useSyncExternalStore, type ReactNode } from 'react'
import { LoaderCircle } from 'lucide-react'
import type { HistoryMessage, AgentView, ToolApproval } from '../../api'
import type { AgentOutputSource } from '../session/sessionStore'
import { AgentConversation } from './AgentConversation'
import type { RequestActivities } from './history/entries'

type AgentSessionPanelProps = {
    submissionRevision: number
    loading: boolean
    agent: AgentView | null
    entries: HistoryMessage[]
    requests: RequestActivities
    pendingApprovals: ToolApproval[]
    outputStore: AgentOutputSource
    onOpenSubagent: (id: string) => void
    entriesError: string
    hasEarlier: boolean
    historyLoading: boolean
    onLoadEarlier: () => Promise<void>
    onRetryHistory: () => Promise<void>
    busy: boolean
    composer: ReactNode
}

/** 局部订阅流式输出，确保高频文本增量不触发工作区壳层重渲染。 */
export function AgentSessionPanel({
    submissionRevision,
    loading,
    agent,
    entries,
    requests,
    pendingApprovals,
    outputStore,
    onOpenSubagent,
    entriesError,
    hasEarlier,
    historyLoading,
    onLoadEarlier,
    onRetryHistory,
    busy,
    composer,
}: AgentSessionPanelProps) {
    const { streamingOutputs } = useSyncExternalStore(outputStore.subscribeOutput, outputStore.getOutput)
    if (loading) {
        return <div className="agent-loading" aria-busy="true"><LoaderCircle size={18} className="is-spinning" aria-hidden="true" /><span>加载对话中</span></div>
    }
    if (!agent) {
        return composer
    }

    return (
        <AgentConversation
            key={`${agent.SessionID}:${agent.ID}`}
            submissionRevision={submissionRevision}
            agent={agent}
            entries={entries}
            requests={requests}
            pendingApprovals={pendingApprovals}
            onOpenSubagent={onOpenSubagent}
            streamingOutput={streamingOutputs[agent.ID] || null}
            entriesError={entriesError}
            hasEarlier={hasEarlier}
            historyLoading={historyLoading}
            onLoadEarlier={onLoadEarlier}
            onRetryHistory={onRetryHistory}
            busy={busy}
            composer={composer}
        />
    )
}
