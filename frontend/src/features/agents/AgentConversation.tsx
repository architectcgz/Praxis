import { useCallback, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ArrowDown, MessageSquare } from 'lucide-react'
import type { AgentHistoryItem, AgentMessageBlock, AgentSnapshot, ModelOption } from '../../api'
import type { StreamingOutput, PendingUserMessage as PendingUserMessageItem } from './types'
import { AgentTaskInput } from './AgentTaskInput'
import { HistoryItemView, PendingOutputView, PendingUserMessageView, StreamingStepView } from './output/MessageViews'

export type AgentConversationProps = {
    agent: AgentSnapshot
    history: AgentHistoryItem[]
    collaboration?: ReactNode
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
    onControl: (kind: 'pause' | 'cancel') => void
    onModelChange: (providerId: string, modelId: string) => void
    onReasoningChange: (level: string) => void
    commands: readonly { name: string; description: string }[]
    onCommand: (name: string) => Promise<boolean>
}

export function AgentConversation({ agent, history, collaboration, streamingOutput, pendingUserMessages, awaitingOutput, input, setInput, models, selectedProviderID, selectedModelID, reasoning, busy, onSend, onControl, onModelChange, onReasoningChange, commands, onCommand }: AgentConversationProps) {
    const messageThreadRef = useRef<HTMLDivElement>(null)
    const followingOutputRef = useRef(true)
    const previousAgentIDRef = useRef('')
    const [showScrollButton, setShowScrollButton] = useState(false)
    const active = agent.state === 'executing' || agent.state === 'pausing'
    const waitingForOutput = awaitingOutput || (agent.state === 'executing' && !busy && !streamingOutput)

    const orderedHistory = useMemo(() => {
        return [...history].sort((left, right) => {
            if (left.sequence !== undefined && right.sequence !== undefined && left.sequence !== right.sequence) {
                return left.sequence - right.sequence
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
                    if (block.kind === 'tool_call' && block.callId && item.sequence !== undefined) {
                        calls.set(`${message.turnId}:${block.callId}`, item.sequence)
                    }
                }
            }
            if (message?.role === 'tool') {
                const unpaired: AgentMessageBlock[] = []
                for (const block of message.blocks || []) {
                    const sequence = calls.get(`${message.turnId}:${block.callId}`)
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

    // Pending messages belong to the agent that was active when they were sent.
    const visiblePendingMessages = useMemo(() => pendingUserMessages.filter((message) => (
        message.agentId ? message.agentId === agent.id : message.sessionId === agent.sessionId
    )), [agent.id, agent.sessionId, pendingUserMessages])
    const pendingMessagesKey = visiblePendingMessages.map((message) => `${message.requestId}:${message.content}`).join('|')
    const threadEmpty = orderedHistory.length === 0 && visiblePendingMessages.length === 0 && !streamingOutput && !waitingForOutput && !collaboration

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
    }, [agent.id, orderedHistory, pendingMessagesKey, streamingOutput, waitingForOutput, collaboration])

    useLayoutEffect(() => {
        const messageThread = messageThreadRef.current
        if (!messageThread) {
            return
        }
        const observer = new MutationObserver(() => {
            if (followingOutputRef.current) {
                messageThread.scrollTop = messageThread.scrollHeight
            }
        })
        observer.observe(messageThread, { childList: true, characterData: true, subtree: true })
        return () => observer.disconnect()
    }, [agent.id])

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
            <div className="message-thread-container">
                <div
                    className={`message-thread thread ${threadEmpty ? 'empty' : ''}`}
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
                                    向 {agent.name} 发送消息以开始协作。您可以提出问题、请求帮助或分配任务。
                                </p>
                            </div>
                        </div>
                    ) : (
                        visibleHistory.map((item, index) => {
                            const continuation = sameAssistantTurn(visibleHistory[index - 1], item)
                            const hasContinuation = sameAssistantTurn(item, visibleHistory[index + 1])
                            let start = index
                            if (!hasContinuation) {
                                while (sameAssistantTurn(visibleHistory[start - 1], item)) start -= 1
                            }
                            const copyContent = visibleHistory.slice(start, index + 1).map((entry) => entry.message?.content || '').filter(Boolean).join('\n\n')
                            return <HistoryItemView
                                agentProfile={agent.name}
                                item={item}
                                continuation={continuation}
                                hasContinuation={hasContinuation}
                                copyContent={copyContent}
                                toolResults={item.sequence === undefined ? undefined : toolResults.get(item.sequence)}
                                key={`${item.kind}-${item.sequence ?? 'at'}-${item.message?.turnId || item.turn?.id || item.at}`}
                            />
                        })
                    )}
                    {collaboration}
                    {visiblePendingMessages.map((message) => (
                        <PendingUserMessageView content={message.content} at={message.at} key={message.requestId} />
                    ))}
                    {streamingOutput?.steps.map((step, index) => (
                        <StreamingStepView
                            agentProfile={agent.name}
                            events={step.events}
                            turnId={streamingOutput.turnId}
                            step={step.step}
                            active={active}
                            continuation={index > 0}
                            hasContinuation={index < streamingOutput.steps.length - 1}
                            key={`${streamingOutput.turnId}-${step.step}`}
                        />
                    ))}
                    {waitingForOutput && <PendingOutputView agentProfile={agent.name} turnId={agent.currentTurnId} />}
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
            </div>
            <AgentTaskInput
                sessionId={agent.sessionId}
                active={active}
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
        </div>
    )
}

/** 仅衔接同一次执行中相邻的助手消息；用户消息、错误和不同任务都保留独立边界。 */
function sameAssistantTurn(left?: AgentHistoryItem, right?: AgentHistoryItem): boolean {
    return left?.kind === 'message' && right?.kind === 'message' &&
        left.message?.role === 'assistant' && right.message?.role === 'assistant' &&
        !!left.message.turnId && left.message.turnId === right.message.turnId
}
