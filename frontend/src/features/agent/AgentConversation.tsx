import { useCallback, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ArrowDown, MessageSquare } from 'lucide-react'
import type { Entry, AgentView, ModelOption } from '../../api'
import type { StreamingOutput, PendingUserMessage as PendingUserMessageItem } from './types'
import { AgentTaskInput } from './AgentTaskInput'
import { ApprovalGate, ToolApprovalsProvider } from './ToolApprovals'
import { AgentActivityView, EntryView, PendingOutputView, PendingUserMessageView, StreamingTurnView } from './output/MessageViews'

export type AgentConversationProps = {
    branchId: string
    agent: AgentView
    entries: Entry[]
    collaboration?: ReactNode
    streamingOutput: StreamingOutput | null
    pendingUserMessages: PendingUserMessageItem[]
    awaitingOutput: boolean
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

export function AgentConversation({ branchId, agent, entries, collaboration, streamingOutput, pendingUserMessages, awaitingOutput, entriesError, hasEarlier, historyLoading, onLoadEarlier, onRetryHistory, input, setInput, models, selectedProviderID, selectedModelID, reasoning, busy, canQueue, beforeInput, onSend, onStop, onModelChange, onReasoningChange, commands, onCommand }: AgentConversationProps) {
    const messageThreadRef = useRef<HTMLDivElement>(null)
    const followingOutputRef = useRef(true)
    const previousAgentIDRef = useRef('')
    const pendingRequestIDsRef = useRef<ReadonlySet<string>>(new Set())
    const [showScrollButton, setShowScrollButton] = useState(false)
    const active = agent.state === 'executing'
    const waitingForOutput = awaitingOutput || (agent.state === 'executing' && !busy && !streamingOutput)
    // Assistant Entry 接纳后承接同一回合的流式展示。
    const visibleStreamingOutput = streamingOutput ? {
        ...streamingOutput,
        turns: streamingOutput.turns.filter((turn) => !entries.some((entry) => (
            entry.role === 'assistant' && entry.runId === streamingOutput.runId && entry.turnId === turn.turnId
        ))),
    } : null
    const latestRun = agent.latestRun
    const runError = !agent.currentRunId && latestRun?.Status === 'ended' && latestRun.Outcome === 'failed' &&
        !entries.some((entry) => entry.runId === latestRun.ID && entry.error)
        ? latestRun.FailureMessage || `执行失败：${latestRun.FailureCode}` : ''

    // 乐观消息只展示在提交时所属的 Agent 中。
    const visiblePendingMessages = useMemo(() => pendingUserMessages.filter((message) => (
        message.agentId ? message.agentId === agent.ID : message.sessionId === agent.SessionID
    )), [agent.ID, agent.SessionID, pendingUserMessages])
    const threadEmpty = entries.length === 0 && visiblePendingMessages.length === 0 && !streamingOutput && !waitingForOutput && !collaboration && !entriesError

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
    }, [agent.ID, entries, visiblePendingMessages, streamingOutput, waitingForOutput, collaboration])

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

    return <ToolApprovalsProvider key={`${agent.SessionID}-${branchId}-${agent.ID}`} agentId={agent.ID} runId={agent.currentRunId} sessionId={agent.SessionID}>
        <div className="agent-conversation">
            <div className="message-thread-container">
                <div
                    className={`message-thread thread ${threadEmpty ? 'empty' : ''}`}
                    ref={messageThreadRef}
                    onScroll={updateOutputFollowing}
                >
                    {hasEarlier && <button className="quiet-button" type="button" disabled={historyLoading} onClick={() => { followingOutputRef.current = false; void onLoadEarlier() }} aria-busy={historyLoading}>{historyLoading ? '正在加载历史…' : '加载更早的历史'}</button>}
                    {entriesError && <p role="alert">{entriesError} <button className="quiet-button" type="button" disabled={historyLoading} onClick={() => void onRetryHistory()}>重试加载</button></p>}
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
                        entries.map((item, index) => {
                            const continuation = sameAssistantTask(entries[index - 1], item)
                            const hasContinuation = sameAssistantTask(item, entries[index + 1])
                            let start = index
                            if (!hasContinuation) {
                                while (sameAssistantTask(entries[start - 1], item)) start -= 1
                            }
                            const copyContent = entries.slice(start, index + 1).map((entry) => entry.content.filter((block) => block.kind === 'text').map((block) => block.text || '').join('')).filter(Boolean).join('\n\n')
                            return <EntryView
                                agentName={agent.name}
                                item={item}
                                events={streamingOutput && streamingOutput.runId === item.runId ? streamingOutput.turns.find((turn) => turn.turnId === item.turnId)?.events : undefined}
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
                            runId={visibleStreamingOutput.runId}
                            run={agent.currentRun?.ID === visibleStreamingOutput.runId ? agent.currentRun : agent.latestRun}
                            turnId={turn.turnId}
                            active={active && index === visibleStreamingOutput.turns.length - 1 && !visibleStreamingOutput.error}
                            continuation={index > 0}
                            hasContinuation={index < visibleStreamingOutput.turns.length - 1}
                            key={`${visibleStreamingOutput.runId}-${turn.turnId}`}
                        />
                    ))}
                    {!visibleStreamingOutput && runError && <AgentActivityView error={runError} />}
                    {visibleStreamingOutput?.error && !entries.some((entry) => entry.runId === visibleStreamingOutput.runId && entry.error) && <AgentActivityView error={visibleStreamingOutput.error} />}
                    {waitingForOutput && <PendingOutputView agentName={agent.name} run={agent.currentRun} />}
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
                onStop={onStop}
                onModelChange={onModelChange}
                onReasoningChange={onReasoningChange}
                commands={commands}
                onCommand={onCommand}
            />
        </div>
    </ToolApprovalsProvider>
}

/** 仅衔接同一次执行中相邻的助手消息；用户消息、错误和不同任务都保留独立边界。 */
function sameAssistantTask(left?: Entry, right?: Entry): boolean {
    return left?.role === 'assistant' && right?.role === 'assistant' &&
        !left.error && !right.error && !!left.runId && left.runId === right.runId
}
