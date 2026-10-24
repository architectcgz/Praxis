import { useSyncExternalStore, type ReactNode } from 'react'
import { LoaderCircle } from 'lucide-react'
import type { ContextEntry, AgentView, ModelOption } from '../../api'
import type { AgentOutputSource } from './agentStore'
import { CollaborationViews } from './output/CollaborationViews'
import { AgentConversation } from './AgentConversation'
import { PrimaryAgentTaskInput } from './PrimaryAgentTaskInput'
import { OperationTimingsProvider } from '../timing/OperationTimings'
import { FilePreviewProvider } from './output/FilePreview'
import { ModelUsageProvider } from './SessionTokenUsage'

type AgentSessionPanelProps = {
    projectId: string
    loading: boolean
    agents: AgentView[]
    agent: AgentView | null
    history: ContextEntry[]
    agentHistories: Record<string, ContextEntry[]>
    agentHistoryErrors: Record<string, string>
    outputStore: AgentOutputSource
    onSelectAgent: (id: string) => void
    historyError: string
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
    onControl: (kind: 'pause' | 'cancel') => void
    onModelChange: (providerId: string, modelId: string) => void
    onReasoningChange: (level: string) => void
    commands: readonly { name: string; description: string }[]
    onCommand: (name: string) => Promise<boolean>
}

/** 展示当前 Session 中选中的 Agent 对话和输入控件。 */
export function AgentSessionPanel({ projectId, loading, agents, agent, history, agentHistories, agentHistoryErrors, outputStore, onSelectAgent, historyError, input, setInput, models, selectedProviderID, selectedModelID, reasoning, busy, canQueue, beforeInput, onSend, onControl, onModelChange, onReasoningChange, commands, onCommand }: AgentSessionPanelProps) {
    const { streamingOutputs, pendingUserMessages, awaitingAgentId } = useSyncExternalStore(outputStore.subscribeOutput, outputStore.getOutput)
    if (loading) {
        return <div className="agent-loading" aria-busy="true"><LoaderCircle size={18} className="is-spinning" aria-hidden="true" /><span>加载对话中</span></div>
    }
    if (!agent) {
        return <PrimaryAgentTaskInput input={input} setInput={setInput} busy={busy} onSend={onSend} commands={commands} onCommand={onCommand} />
    }

    const hasSubagents = agents.some((item) => item.Kind === 'subagent')
    return (
        <ModelUsageProvider key={agent.SessionID} sessionId={agent.SessionID}>
        <OperationTimingsProvider key={agent.SessionID} agentIds={agents.map((item) => item.ID)}>
        <FilePreviewProvider sessionId={agent.SessionID}>
        <AgentConversation
            projectId={projectId}
            agent={agent}
            history={history}
            collaboration={agent.Kind === 'main' && hasSubagents ? (
                <CollaborationViews
                    agents={agents}
                    histories={agentHistories}
                    historyErrors={agentHistoryErrors}
                    streamingOutputs={streamingOutputs}
                    onSelectAgent={onSelectAgent}
                />
            ) : undefined}
            streamingOutput={streamingOutputs[agent.ID] || null}
            pendingUserMessages={pendingUserMessages}
            awaitingOutput={awaitingAgentId === agent.ID}
            historyError={historyError}
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
            onControl={onControl}
            onModelChange={onModelChange}
            onReasoningChange={onReasoningChange}
            commands={commands}
            onCommand={onCommand}
        />
        </FilePreviewProvider>
        </OperationTimingsProvider>
        </ModelUsageProvider>
    )
}
