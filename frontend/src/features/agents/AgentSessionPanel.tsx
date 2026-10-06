import { LoaderCircle } from 'lucide-react'
import type { AgentHistoryItem, AgentSnapshot, ModelOption } from '../../api'
import type { PendingUserMessage, StreamingOutput } from './types'
import { CollaborationViews } from './output/CollaborationViews'
import { AgentConversation } from './AgentConversation'
import { PrimaryAgentTaskInput } from './PrimaryAgentTaskInput'
import { OperationTimingsProvider } from '../timing/OperationTimings'
import { FilePreviewProvider } from './output/FilePreview'
import { ModelUsageProvider } from './SessionTokenUsage'

type AgentSessionPanelProps = {
    loading: boolean
    agents: AgentSnapshot[]
    agent: AgentSnapshot | null
    history: AgentHistoryItem[]
    agentHistories: Record<string, AgentHistoryItem[]>
    agentHistoryErrors: Record<string, string>
    streamingOutputs: Record<string, StreamingOutput>
    onSelectAgent: (id: string) => void
    streamingOutput: StreamingOutput | null
    pendingUserMessages: PendingUserMessage[]
    awaitingOutput: boolean
    input: string
    setInput: (value: string) => void
    models: ModelOption[]
    selectedProviderID: string
    selectedModelID: string
    reasoning: string
    busy: boolean
    onSend: () => void
    onControl: (kind: 'pause' | 'cancel') => void
    onModelChange: (providerId: string, modelId: string) => void
    onReasoningChange: (level: string) => void
    commands: readonly { name: string; description: string }[]
    onCommand: (name: string) => Promise<boolean>
}

/** 展示当前 Session 中选中的 Agent 对话和输入控件。 */
export function AgentSessionPanel({ loading, agents, agent, history, agentHistories, agentHistoryErrors, streamingOutputs, onSelectAgent, streamingOutput, pendingUserMessages, awaitingOutput, input, setInput, models, selectedProviderID, selectedModelID, reasoning, busy, onSend, onControl, onModelChange, onReasoningChange, commands, onCommand }: AgentSessionPanelProps) {
    if (loading) {
        return <div className="agent-loading" aria-busy="true"><LoaderCircle size={18} className="is-spinning" aria-hidden="true" /><span>加载对话中</span></div>
    }
    if (!agent) {
        return <PrimaryAgentTaskInput input={input} setInput={setInput} busy={busy} onSend={onSend} commands={commands} onCommand={onCommand} />
    }

    const hasDelegates = agents.some((item) => item.definitionId === 'delegate' || item.profile === 'delegate')
    return (
        <ModelUsageProvider key={agent.sessionId} sessionId={agent.sessionId}>
        <OperationTimingsProvider key={agent.sessionId} agentIds={agents.map((item) => item.id)}>
        <FilePreviewProvider sessionId={agent.sessionId}>
        <AgentConversation
            agent={agent}
            history={history}
            collaboration={agent.definitionId === 'primary' && hasDelegates ? (
                <CollaborationViews
                    agents={agents}
                    histories={agentHistories}
                    historyErrors={agentHistoryErrors}
                    streamingOutputs={streamingOutputs}
                    onSelectAgent={onSelectAgent}
                />
            ) : undefined}
            streamingOutput={streamingOutput}
            pendingUserMessages={pendingUserMessages}
            awaitingOutput={awaitingOutput}
            input={input}
            setInput={setInput}
            models={models}
            selectedProviderID={selectedProviderID}
            selectedModelID={selectedModelID}
            reasoning={reasoning}
            busy={busy}
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
