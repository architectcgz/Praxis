import { useCallback, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ArrowDown, MessageSquare } from 'lucide-react'
import type { ContextEntry, AgentView, ModelOption } from '../../api'
import type { StreamingOutput, PendingUserMessage as PendingUserMessageItem } from './types'
import { AgentTaskInput } from './AgentTaskInput'
import { ApprovalGate, ToolApprovalsProvider } from './ToolApprovals'
import { AgentActivityView, HistoryItemView, PendingOutputView, PendingUserMessageView, StreamingTurnView } from './output/MessageViews'

export type AgentConversationProps = {
    projectId: string
    agent: AgentView
    history: ContextEntry[]
    collaboration?: ReactNode
    streamingOutput: StreamingOutput | null
    pendingUserMessages: PendingUserMessageItem[]
    awaitingOutput: boolean
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

export function AgentConversation({ projectId, agent, history, collaboration, streamingOutput, pendingUserMessages, awaitingOutput, historyError, input, setInput, models, selectedProviderID, selectedModelID, reasoning, busy, canQueue, beforeInput, onSend, onControl, onModelChange, onReasoningChange, commands, onCommand }: AgentConversationProps) {
    const messageThreadRef = useRef<HTMLDivElement>(null)
    const followingOutputRef = useRef(true)
    const previousAgentIDRef = useRef('')
    const pendingRequestIDsRef = useRef<ReadonlySet<string>>(new Set())
    const [showScrollButton, setShowScrollButton] = useState(false)
    const active = agent.state === 'executing'
    const waitingForOutput = awaitingOutput || (agent.state === 'executing' && !busy && !streamingOutput)
    const visibleStreamingOutput = streamingOutput
    const latestExecution = agent.latestExecution
    const executionError = latestExecution?.Status === 'ended' && latestExecution.Outcome === 'failed'
        ? latestExecution.FailureMessage || `执行失败：${latestExecution.FailureCode}` : ''
    const orderedHistory = history
    const visibleHistory = history

    // Pending messages belong to the agent that was active when they were sent.
    const visiblePendingMessages = useMemo(() => pendingUserMessages.filter((message) => (
        message.agentId ? message.agentId === agent.ID : message.sessionId === agent.SessionID
    )), [agent.ID, agent.SessionID, pendingUserMessages])
    const threadEmpty = orderedHistory.length === 0 && visiblePendingMessages.length === 0 && !streamingOutput && !waitingForOutput && !collaboration

    useLayoutEffect(() => {
        const messageThread = messageThreadRef.current
        if (!messageThread) {
            return
        }
        const switchingAgent = previousAgentIDRef.current !== agent.ID
        // 刚发出的用户消息是明确意图：即使此前正在浏览历史，也要回到最新输出并恢复跟随。
        const justSent = visiblePendingMessages.some((message) => !pendingRequestIDsRef.current.has(message.requestId))
        pendingRequestIDsRef.current = new Set(visiblePendingMessages.map((message) => message.requestId))
        if (!switchingAgent && !justSent && !followingOutputRef.current) {
            return
        }
        messageThread.scrollTo({
            top: messageThread.scrollHeight,
            behavior: 'auto',
        })
        followingOutputRef.current = true
        previousAgentIDRef.current = agent.ID
    }, [agent.ID, orderedHistory, visiblePendingMessages, streamingOutput, waitingForOutput, collaboration])

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
    }, [agent.ID])

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

    return <ToolApprovalsProvider key={agent.ID} agentId={agent.ID}>
        <div className="agent-conversation">
            <div className="message-thread-container">
                <div
                    className={`message-thread thread ${threadEmpty ? 'empty' : ''}`}
                    ref={messageThreadRef}
                    onScroll={updateOutputFollowing}
                >
                    {historyError && <p role="alert">{historyError}</p>}
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
                            const continuation = sameAssistantTask(visibleHistory[index - 1], item)
                            const hasContinuation = sameAssistantTask(item, visibleHistory[index + 1])
                            let start = index
                            if (!hasContinuation) {
                                while (sameAssistantTask(visibleHistory[start - 1], item)) start -= 1
                            }
                            const copyContent = visibleHistory.slice(start, index + 1).map((entry) => entry.content.filter((block) => block.kind === 'text').map((block) => block.text || '').join('')).filter(Boolean).join('\n\n')
                            return <HistoryItemView
                                agentName={agent.name}
                                item={item}
                                continuation={continuation}
                                hasContinuation={hasContinuation}
                                copyContent={copyContent}
                                key={item.id}
                            />
                        })
                    )}
                    {collaboration}
                    {visiblePendingMessages.map((message) => (
                        <PendingUserMessageView content={message.content} at={message.at} key={message.requestId} />
                    ))}
                    {visibleStreamingOutput?.turns.map((turn, index) => (
                        <StreamingTurnView
                            agentName={agent.name}
                            events={turn.events}
                            executionId={visibleStreamingOutput.executionId}
                            turnId={turn.turnId}
                            active={active && index === visibleStreamingOutput.turns.length - 1 && !visibleStreamingOutput.error}
                            continuation={index > 0}
                            hasContinuation={index < visibleStreamingOutput.turns.length - 1}
                            key={`${visibleStreamingOutput.executionId}-${turn.turnId}`}
                        />
                    ))}
                    {!visibleStreamingOutput && executionError && <AgentActivityView error={executionError} />}
                    {visibleStreamingOutput?.error && <AgentActivityView error={visibleStreamingOutput.error} />}
                    {waitingForOutput && <PendingOutputView agentName={agent.name} executionId={agent.currentExecutionId} />}
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
            <ApprovalGate />
            <div className="command-band agent-input-accessory">{beforeInput}</div>
            <AgentTaskInput
                projectId={projectId}
                sessionId={agent.SessionID}
                active={active}
                canQueue={canQueue}
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
    </ToolApprovalsProvider>
}

/** 仅衔接同一次执行中相邻的助手消息；用户消息、错误和不同任务都保留独立边界。 */
function sameAssistantTask(left?: ContextEntry, right?: ContextEntry): boolean {
    return left?.role === 'assistant' && right?.role === 'assistant' &&
        !!left.executionId && left.executionId === right.executionId
}
