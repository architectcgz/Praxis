import { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { ArrowDown, MessageSquare } from 'lucide-react'
import type { AgentHistoryItem, AgentMessageBlock, AgentSnapshot, ModelOption } from '../../api'
import type { StreamingOutput, PendingUserMessage as PendingUserMessageItem } from './types'
import { AgentTaskInput } from './AgentTaskInput'
import { AgentActivityView, HistoryItemView, PendingOutputView, PendingUserMessageView, StreamingTurnView } from './MessageViews'

export type AgentConversationProps = {
    agent: AgentSnapshot
    history: AgentHistoryItem[]
    streamingOutput: StreamingOutput | null
    pendingUserMessages: PendingUserMessageItem[]
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

export function AgentConversation({ agent, history, streamingOutput, pendingUserMessages, awaitingOutput, input, setInput, models, selectedProviderID, selectedModelID, reasoning, busy, onSend, onControl, onModelChange, onReasoningChange }: AgentConversationProps) {
    const messageThreadRef = useRef<HTMLDivElement>(null)
    const followingOutputRef = useRef(true)
    const previousAgentIDRef = useRef('')
    const [showScrollButton, setShowScrollButton] = useState(false)
    const active = agent.state === 'executing' || agent.state === 'pausing'
    const waitingForOutput = awaitingOutput || (agent.state === 'executing' && !busy && !streamingOutput)
    const closed = agent.state === 'closed'

    const orderedHistory = useMemo(() => {
        return [...history].sort((left, right) => {
            const sequenceDifference = left.sequence - right.sequence
            if (sequenceDifference !== 0) {
                return sequenceDifference
            }
            return new Date(left.at).getTime() - new Date(right.at).getTime()
        })
    }, [history])

    const { visibleHistory, toolResults } = useMemo(() => {
        const visibleHistory: AgentHistoryItem[] = []
        const toolResults = new Map<number, AgentMessageBlock[]>()
        const calls = new Map<string, number>()
        for (const item of orderedHistory) {
            const message = item.message
            if (message?.role === 'assistant') {
                for (const block of message.blocks || []) {
                    if (block.kind === 'tool_call' && block.callId) {
                        calls.set(`${message.executionId}:${block.callId}`, item.sequence)
                    }
                }
            }
            if (message?.role === 'tool') {
                const unpaired: AgentMessageBlock[] = []
                for (const block of message.blocks || []) {
                    const sequence = calls.get(`${message.executionId}:${block.callId}`)
                    if (block.kind !== 'tool_result' || sequence === undefined) {
                        unpaired.push(block)
                    } else {
                        toolResults.set(sequence, [...(toolResults.get(sequence) || []), block])
                    }
                }
                if (unpaired.length === 0) continue
                visibleHistory.push({ ...item, message: { ...message, blocks: unpaired } })
            } else {
                visibleHistory.push(item)
            }
        }
        return { visibleHistory, toolResults }
    }, [orderedHistory])

    const latestItem = orderedHistory[orderedHistory.length - 1]
    const latestItemKey = latestItem
        ? `${latestItem.kind}-${latestItem.sequence}-${latestItem.at}-${latestItem.message?.content || latestItem.execution?.id || ''}`
        : ''

    // Pending messages belong to the agent that was active when they were sent.
    const visiblePendingMessages = useMemo(() => pendingUserMessages.filter((message) => (
        message.agentId ? message.agentId === agent.id : message.sessionId === agent.sessionId
    )), [agent.id, agent.sessionId, pendingUserMessages])
    const pendingMessagesKey = visiblePendingMessages.map((message) => `${message.requestId}:${message.content}`).join('|')
    const threadEmpty = orderedHistory.length === 0 && visiblePendingMessages.length === 0 && !streamingOutput && !waitingForOutput

    useLayoutEffect(() => {
        const messageThread = messageThreadRef.current
        if (!messageThread) {
            return
        }
        const switchingAgent = previousAgentIDRef.current !== agent.id
        if (!switchingAgent && !followingOutputRef.current) {
            return
        }
        messageThread.scrollTo({
            top: messageThread.scrollHeight,
            behavior: 'auto',
        })
        followingOutputRef.current = true
        previousAgentIDRef.current = agent.id
    }, [agent.id, latestItemKey, pendingMessagesKey, streamingOutput, waitingForOutput])

    const updateOutputFollowing = useCallback(() => {
        const messageThread = messageThreadRef.current
        if (!messageThread) {
            return
        }
        const distanceFromBottom = messageThread.scrollHeight - messageThread.scrollTop - messageThread.clientHeight
        followingOutputRef.current = distanceFromBottom <= 24
        setShowScrollButton(distanceFromBottom > 100)
    }, [])

    const scrollToBottom = useCallback(() => {
        const messageThread = messageThreadRef.current
        if (!messageThread) {
            return
        }
        messageThread.scrollTo({
            top: messageThread.scrollHeight,
            behavior: 'smooth',
        })
        followingOutputRef.current = true
        setShowScrollButton(false)
    }, [])

    return (
        <div className="agent-conversation">
            <div
                className={`message-thread ${threadEmpty ? 'empty' : ''}`}
                ref={messageThreadRef}
                onScroll={updateOutputFollowing}
            >
                {threadEmpty ? (
                    <div className="message-empty" role="status">
                        <div className="message-empty-content">
                            <div className="message-empty-icon" aria-hidden="true">
                                <MessageSquare size={24} strokeWidth={1.6} />
                            </div>
                            <h3 className="message-empty-title">开始新对话</h3>
                            <p className="message-empty-text">
                                向 {agent.profile} 发送消息以开始协作。您可以提出问题、请求帮助或分配任务。
                            </p>
                        </div>
                    </div>
                ) : (
                    visibleHistory.map((item) => (
                        <HistoryItemView agentProfile={agent.profile} item={item} toolResults={toolResults.get(item.sequence)} key={`${item.kind}-${item.sequence}-${item.message?.executionId || item.execution?.id || item.at}`} />
                    ))
                )}
                {visiblePendingMessages.map((message) => (
                    <PendingUserMessageView content={message.content} at={message.at} key={message.requestId} />
                ))}
                {streamingOutput?.turns.map((turn) => (
                    <StreamingTurnView agentProfile={agent.profile} events={turn.events} key={`${streamingOutput.executionId}-${turn.turn}`} />
                ))}
                {streamingOutput?.error && <AgentActivityView error={streamingOutput.error} />}
                {waitingForOutput && <PendingOutputView agentProfile={agent.profile} />}
            </div>
            {showScrollButton && (
                <button
                    className="scroll-to-bottom-button"
                    type="button"
                    title="滚动到底部"
                    aria-label="滚动到底部"
                    onClick={scrollToBottom}
                >
                    <ArrowDown size={18} strokeWidth={2} aria-hidden="true" />
                </button>
            )}
            <AgentTaskInput
                active={active}
                closed={closed}
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
            />
        </div>
    )
}
