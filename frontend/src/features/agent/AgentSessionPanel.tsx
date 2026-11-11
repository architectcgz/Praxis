import { useSyncExternalStore, type ReactNode } from 'react'
import { LoaderCircle } from 'lucide-react'
import type { Entry, AgentView, ModelOption } from '../../api'
import type { AgentOutputSource } from './agentStore'
import { CollaborationViews } from './output/CollaborationViews'
import { AgentConversation } from './AgentConversation'
import { PrimaryAgentTaskInput } from './PrimaryAgentTaskInput'
import { FilePreviewProvider } from './output/FilePreview'
import { ModelUsageProvider } from './SessionTokenUsage'

type AgentSessionPanelProps = {
    branchId: string
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
    input: string
    setInput: (value: string) => void
    models: ModelOption[]
    selectedProviderID: string
    selectedModelID: string
    reasoning: string
    busy: boolean
    canQueue: boolean
    beforeInput?: ReactNode
    onSend: () => void
    onStop: () => void
    onModelChange: (providerId: string, modelId: string) => void
    onReasoningChange: (level: string) => void
    commands: readonly { name: string; description: string }[]
    onCommand: (name: string) => Promise<boolean>
}

/** 展示当前 Session 中选中的 Agent 对话和输入控件。 */
export function AgentSessionPanel({ branchId, usageRevision, loading, agents, agent, entries, agentEntries, agentEntriesErrors, outputStore, onSelectAgent, entriesError, hasEarlier, historyLoading, onLoadEarlier, onRetryHistory, input, setInput, models, selectedProviderID, selectedModelID, reasoning, busy, canQueue, beforeInput, onSend, onStop, onModelChange, onReasoningChange, commands, onCommand }: AgentSessionPanelProps) {
    const { streamingOutputs, pendingUserMessages, awaitingAgentId } = useSyncExternalStore(outputStore.subscribeOutput, outputStore.getOutput)
    if (loading) {
        return <div className="agent-loading" aria-busy="true"><LoaderCircle size={18} className="is-spinning" aria-hidden="true" /><span>加载对话中</span></div>
    }
    if (!agent) {
        return <PrimaryAgentTaskInput input={input} setInput={setInput} busy={busy} onSend={onSend} commands={commands} onCommand={onCommand} />
    }

    const hasSubagents = agents.some((item) => item.Kind === 'subagent')
    return (
        <ModelUsageProvider key={`${agent.SessionID}-${branchId}`} sessionId={agent.SessionID} revision={usageRevision} entries={entries}>
        <FilePreviewProvider sessionId={agent.SessionID}>
        <AgentConversation
            key={`${agent.SessionID}-${branchId}`}
            branchId={branchId}
            agent={agent}
            entries={entries}
            collaboration={agent.Kind === 'main' && hasSubagents ? (
                <CollaborationViews
                    agents={agents}
                    agentEntries={agentEntries}
                    entriesErrors={agentEntriesErrors}
                    streamingOutputs={streamingOutputs}
                    onSelectAgent={onSelectAgent}
                />
            ) : undefined}
            streamingOutput={streamingOutputs[agent.ID] || null}
            pendingUserMessages={pendingUserMessages}
            awaitingOutput={awaitingAgentId === agent.ID}
            entriesError={entriesError}
            hasEarlier={hasEarlier} historyLoading={historyLoading} onLoadEarlier={onLoadEarlier} onRetryHistory={onRetryHistory}
            input={input}
            setInput={setInput}
            models={models}
            selectedProviderID={selectedProviderID}
            selectedModelID={selectedModelID}
            reasoning={reasoning}
            busy={busy}
            canQueue={canQueue}
            beforeInput={beforeInput}
            onSend={onSend}
            onStop={onStop}
            onModelChange={onModelChange}
            onReasoningChange={onReasoningChange}
            commands={commands}
            onCommand={onCommand}
        />
        </FilePreviewProvider>
        </ModelUsageProvider>
    )
}
