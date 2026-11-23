import { useCallback, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ArrowDown, Bot, CircleAlert, MessageSquare } from 'lucide-react'
import { activeRunId, type HistoryMessage, type AgentView, type ToolApproval } from '../../api'
import type { RequestActivities } from './history/entries'
import type { StreamingOutput } from './types'
import { ApprovalGate, ToolApprovalScope, ToolApprovalsProvider } from './ToolApprovals'
import { AgentActivityView, PendingOutputView, StreamingTurnView } from './output/MessageViews'
import type { AgentDirectory } from './output/HandoffViews'
import { buildEntryHistory, EntryHistory } from './output/EntryHistory'

export type AgentConversationProps = {
    submissionRevision: number
    agent: AgentView
    agentDirectory?: AgentDirectory
    entries: HistoryMessage[]
    requests: RequestActivities
    pendingApprovals: ToolApproval[]
    onOpenSubagent: (id: string) => void
    streamingOutput: StreamingOutput | null
    entriesError: string
    hasEarlier: boolean
    historyLoading: boolean
    onLoadEarlier: () => Promise<void>
    onRetryHistory: () => Promise<void>
    busy: boolean
    composer: ReactNode
}

export function AgentConversation({
    submissionRevision,
    agent,
    agentDirectory,
    entries,
    requests,
    pendingApprovals,
    onOpenSubagent,
    streamingOutput,
    entriesError,
    hasEarlier,
    historyLoading,
    onLoadEarlier,
    onRetryHistory,
    busy,
    composer,
}: AgentConversationProps) {
    const messageThreadRef = useRef<HTMLDivElement>(null)
    const followingOutputRef = useRef(true)
    const previousViewRef = useRef('')
    const previousSubmissionRevisionRef = useRef(submissionRevision)
    const [showScrollButton, setShowScrollButton] = useState(false)
    const viewKey = agent.ID
    const runId = activeRunId(agent)
    const active = Boolean(runId)
    const waitingForOutput = active && !busy && !streamingOutput
    const history = useMemo(() => buildEntryHistory(entries, requests), [entries, requests])
    // 已落盘回合仍可能等待工具结果；这些事件继续交给对应历史消息，不重复呈现正文。
    const historyOutput = streamingOutput?.turns.some((turn) => history.assistantTurns.has(JSON.stringify([streamingOutput.runId, turn.turnId])))
        ? streamingOutput : null
    const visibleStreamingOutput = streamingOutput ? {
        ...streamingOutput,
        turns: streamingOutput.turns.filter((turn) => !history.assistantTurns.has(JSON.stringify([streamingOutput.runId, turn.turnId]))),
    } : null
    const runError = agent.state === 'failed' && agent.runId && !history.errorRuns.has(agent.runId) ? agent.runFailure || '执行失败' : undefined

    const threadEmpty = history.rows.length === 0 && !streamingOutput && !waitingForOutput && !entriesError

    useLayoutEffect(() => {
        const messageThread = messageThreadRef.current
        if (!messageThread) return
        const switchingView = previousViewRef.current !== viewKey
        const justSent = previousSubmissionRevisionRef.current !== submissionRevision
        previousSubmissionRevisionRef.current = submissionRevision
        if (!switchingView && !justSent && !followingOutputRef.current) return
        messageThread.scrollTo({ top: messageThread.scrollHeight, behavior: 'auto' })
        followingOutputRef.current = true
        previousViewRef.current = viewKey
    }, [viewKey, submissionRevision, history, streamingOutput, waitingForOutput])

    useLayoutEffect(() => {
        const messageThread = messageThreadRef.current
        if (!messageThread) return
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
        if (!messageThread) return
        const distanceFromBottom = messageThread.scrollHeight - messageThread.scrollTop - messageThread.clientHeight
        followingOutputRef.current = distanceFromBottom <= 24
        setShowScrollButton(distanceFromBottom > 100)
    }, [])

    const scrollToBottom = useCallback(() => {
        const messageThread = messageThreadRef.current
        if (!messageThread) return
        messageThread.scrollTo({ top: messageThread.scrollHeight, behavior: 'smooth' })
        followingOutputRef.current = true
        setShowScrollButton(false)
    }, [])

    const agentApprovals = useMemo(
        () => pendingApprovals.filter((item) => item.agentId === agent.ID && (!runId || item.runId === runId)),
        [pendingApprovals, agent.ID, runId],
    )

    return (
        <ToolApprovalsProvider key={`${agent.SessionID}-${agent.ID}`} pendingApprovals={agentApprovals}>
            <div className="agent-conversation">
                <ToolApprovalScope historical={false}>
                    <div className="message-thread-container">
                        <div
                            className={`message-thread thread ${threadEmpty ? 'empty' : ''}`}
                            ref={messageThreadRef}
                            onScroll={updateOutputFollowing}
                        >
                            {hasEarlier && (
                                <div className="thread-top-slot">
                                    <button
                                        className="quiet-button"
                                        type="button"
                                        disabled={historyLoading}
                                        onClick={() => { followingOutputRef.current = false; void onLoadEarlier() }}
                                        aria-busy={historyLoading}
                                    >
                                        {historyLoading ? '正在加载历史…' : '加载更早的历史'}
                                    </button>
                                </div>
                            )}
                            {entriesError && (
                                <div className="thread-top-slot">
                                    <div className="message-error-box thread-error-box" role="alert">
                                        <CircleAlert size={15} strokeWidth={1.8} aria-hidden="true" />
                                        <span>{entriesError}</span>
                                        <button className="quiet-button" type="button" disabled={historyLoading} onClick={() => void onRetryHistory()}>重试加载</button>
                                    </div>
                                </div>
                            )}
                            {threadEmpty ? (
                                <div className="message-empty" role="status">
                                    <div className="message-empty-content">
                                        <div className="message-empty-icon" aria-hidden="true">
                                            {agent.Kind === 'subagent' ? <Bot size={24} strokeWidth={1.6} /> : <MessageSquare size={24} strokeWidth={1.6} />}
                                        </div>
                                        <h3 className="message-empty-title">
                                            {agent.Kind === 'subagent' ? `${agent.name} 暂无输出记录` : '开始新对话'}
                                        </h3>
                                        <p className="message-empty-text">
                                            {agent.Kind === 'subagent'
                                                ? '子代理由主代理委派启动，使用独立的上下文与工具授权。开始执行后，思考过程、工具活动与汇报会展示在这里。'
                                                : `向 ${agent.name} 发送任务或问题开始推进工作，执行过程中可随时排队追加任务或审批高风险工具。`}
                                        </p>
                                    </div>
                                </div>
                            ) : (
                                <EntryHistory
                                    rows={history.rows}
                                    requests={requests}
                                    agentName={agent.name}
                                    agentDirectory={agentDirectory}
                                    streamingOutput={historyOutput}
                                    onOpenSubagent={onOpenSubagent}
                                />
                            )}
                            {visibleStreamingOutput?.turns.map((turn, index) => (
                                <StreamingTurnView
                                    agentName={agent.name}
                                    agentDirectory={agentDirectory}
                                    events={turn.events}
                                    runStartedAt={runId === visibleStreamingOutput.runId ? agent.runStartedAt : undefined}
                                    active={active && index === visibleStreamingOutput.turns.length - 1 && !visibleStreamingOutput.error}
                                    continuation={index > 0}
                                    hasContinuation={index < visibleStreamingOutput.turns.length - 1}
                                    onOpenSubagent={onOpenSubagent}
                                    key={`${visibleStreamingOutput.runId}-${turn.turnId}`}
                                />
                            ))}
                            {!visibleStreamingOutput && runError && <AgentActivityView error={runError} />}
                            {visibleStreamingOutput?.error && !history.errorRuns.has(visibleStreamingOutput.runId) && <AgentActivityView error={visibleStreamingOutput.error} />}
                            {waitingForOutput && <PendingOutputView agentName={agent.name} runStartedAt={agent.runStartedAt} />}
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
                </ToolApprovalScope>
                <ApprovalGate />
                {composer}
            </div>
        </ToolApprovalsProvider>
    )
}
