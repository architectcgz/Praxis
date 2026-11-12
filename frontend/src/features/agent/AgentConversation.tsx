import { useCallback, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ArrowDown, MessageSquare } from 'lucide-react'
import type { Entry, AgentView } from '../../api'
import type { StreamingOutput } from './types'
import { ApprovalGate, ToolApprovalScope, ToolApprovalsProvider } from './ToolApprovals'
import { AgentActivityView, PendingOutputView, StreamingTurnView } from './output/MessageViews'
import { buildEntryHistory, EntryHistory } from './output/EntryHistory'

export type AgentConversationProps = {
    branchId: string
    historyEntryId: string | null
    submissionRevision: number
    agent: AgentView
    entries: Entry[]
    collaboration?: ReactNode
    streamingOutput: StreamingOutput | null
    entriesError: string
    hasEarlier: boolean
    historyLoading: boolean
    onLoadEarlier: () => Promise<void>
    onRetryHistory: () => Promise<void>
    busy: boolean
    composer: ReactNode
}

export function AgentConversation({ branchId, historyEntryId, submissionRevision, agent, entries, collaboration, streamingOutput, entriesError, hasEarlier, historyLoading, onLoadEarlier, onRetryHistory, busy, composer }: AgentConversationProps) {
    const messageThreadRef = useRef<HTMLDivElement>(null)
    const followingOutputRef = useRef(true)
    const previousViewRef = useRef('')
    const previousSubmissionRevisionRef = useRef(submissionRevision)
    const [showScrollButton, setShowScrollButton] = useState(false)
    const historical = historyEntryId !== null
    const viewKey = JSON.stringify([agent.ID, historyEntryId])
    const active = agent.state === 'executing'
    const waitingForOutput = !historical && active && !busy && !streamingOutput
    const history = useMemo(() => buildEntryHistory(entries), [entries])
    // 已落盘回合仍可能等待工具结果；这些事件继续交给对应历史消息，不重复呈现正文。
    const historyOutput = streamingOutput?.turns.some((turn) => history.assistantTurns.has(JSON.stringify([streamingOutput.runId, turn.turnId])))
        ? streamingOutput : null
    const visibleStreamingOutput = streamingOutput ? {
        ...streamingOutput,
        turns: streamingOutput.turns.filter((turn) => !history.assistantTurns.has(JSON.stringify([streamingOutput.runId, turn.turnId]))),
    } : null
    const latestRun = agent.latestRun
    const runError = !historical && !agent.currentRunId && latestRun?.Status === 'ended' && latestRun.Outcome === 'failed' &&
        !history.errorRuns.has(latestRun.ID)
        ? latestRun.FailureMessage || `执行失败：${latestRun.FailureCode}` : ''

    const threadEmpty = entries.length === 0 && !streamingOutput && !waitingForOutput && !collaboration && !entriesError

    useLayoutEffect(() => {
        const messageThread = messageThreadRef.current
        if (!messageThread) {
            return
        }
        const switchingView = previousViewRef.current !== viewKey
        // 任务成功入队才恢复跟随；后台增量和历史分页不能冒充发送意图。
        const justSent = previousSubmissionRevisionRef.current !== submissionRevision
        previousSubmissionRevisionRef.current = submissionRevision
        if (!switchingView && !justSent && !followingOutputRef.current) {
            return
        }
        messageThread.scrollTo({
            top: messageThread.scrollHeight,
            behavior: 'auto',
        })
        followingOutputRef.current = true
        previousViewRef.current = viewKey
    }, [viewKey, submissionRevision, entries, streamingOutput, waitingForOutput, collaboration])

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
            <ToolApprovalScope historical={historical}>
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
                                <h3 className="message-empty-title">{historical ? '此位置没有历史消息' : '开始新对话'}</h3>
                                <p className="message-empty-text">
                                    {historical ? '选择其他历史节点，或返回最新位置。' : `向 ${agent.name} 发送消息以开始协作。您可以提出问题、请求帮助或分配任务。`}
                                </p>
                            </div>
                        </div>
                    ) : <EntryHistory rows={history.rows} agentName={agent.name} streamingOutput={historyOutput} />}
                    {collaboration}
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
                    {visibleStreamingOutput?.error && !history.errorRuns.has(visibleStreamingOutput.runId) && <AgentActivityView error={visibleStreamingOutput.error} />}
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
            </ToolApprovalScope>
            {historical && <div className="command-band agent-input-accessory" role="status">历史浏览中 · 以下为实时操作区{active ? '（当前执行中）' : ''}</div>}
            <ApprovalGate />
            {composer}
        </div>
    </ToolApprovalsProvider>
}
