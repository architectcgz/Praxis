import type { AgentHistoryItem, AgentSnapshot, ModelOption, SessionSnapshot } from '../../api'
import { PrimaryAgentTaskInput } from './PrimaryAgentTaskInput'
import { AgentConversation } from './AgentConversation'
import { SessionEmptyState } from './SessionEmptyState'
import type { StreamingOutput } from './types'

type SessionPanelProps = {
    loading: boolean
    session: SessionSnapshot | null
    sessionsCount: number
    agent: AgentSnapshot | null
    history: AgentHistoryItem[]
    streamingOutput: StreamingOutput | null
    awaitingOutput: boolean
    input: string
    setInput: (value: string) => void
    models: ModelOption[]
    selectedProviderID: string
    selectedModelID: string
    reasoning: string
    busy: boolean
    onSend: () => void
    onControl: (kind: 'pause' | 'close') => void
    onModelChange: (providerId: string, modelId: string) => void
    onReasoningChange: (level: string) => void
}

export function SessionPanel({ loading, session, sessionsCount, agent, history, streamingOutput, awaitingOutput, input, setInput, models, selectedProviderID, selectedModelID, reasoning, busy, onSend, onControl, onModelChange, onReasoningChange }: SessionPanelProps) {
    if (!session) {
        if (loading) {
            return <section className="session-loading" aria-busy="true" aria-label="加载会话中" />
        }
        return (
            <div className="scroll-region">
                <section className="page session-empty-state">
                    <SessionEmptyState sessionsCount={sessionsCount} />
                </section>
            </div>
        )
    }

    return (
        <section className="session-panel">
            {loading ? (
                <div className="session-loading" aria-busy="true" aria-label="加载代理中" />
            ) : !agent ? (
                <PrimaryAgentTaskInput input={input} setInput={setInput} busy={busy} onSend={onSend} />
            ) : (
                <AgentConversation agent={agent} history={history} streamingOutput={streamingOutput} awaitingOutput={awaitingOutput} input={input} setInput={setInput} models={models} selectedProviderID={selectedProviderID} selectedModelID={selectedModelID} reasoning={reasoning} busy={busy} onSend={onSend} onControl={onControl} onModelChange={onModelChange} onReasoningChange={onReasoningChange} />
            )}
        </section>
    )
}
