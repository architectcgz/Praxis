import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Activity, CircleAlert, X } from 'lucide-react'
import { Toast } from '../../components/ui'
import { activeRunId, API_ERROR_CODES } from '../../api'
import { AgentSessionPanel } from '../agent/AgentSessionPanel'
import { AgentTaskInput } from '../agent/AgentTaskInput'
import { formatCompactCount, formatUsageDetails } from '../agent/modelUsage'
import { PrimaryAgentTaskInput } from '../agent/PrimaryAgentTaskInput'
import { SubagentDeck } from '../agent/SubagentDeck'
import { SessionPanel } from '../session/SessionPanel'
import { TaskQueue } from '../task/TaskQueue'
import { WORKSPACE_COMMANDS } from './useWorkspaceCommands'
import { useSessionPage, type SessionPageOptions, type SubagentProgressSummary } from './useSessionPage'

export type SessionPageControls = Pick<
    ReturnType<typeof useSessionPage>,
    'busy' | 'refreshing' | 'models' | 'close' | 'status' | 'queuedCount'
> & {
    title: string
    subagentSummary: SubagentProgressSummary
    openOverview: () => void
}

type Props = SessionPageOptions & {
    active: boolean
    projectName?: string
    sessionsCount: number
    projectsCount: number
    modalOpen: boolean
    onOpenProject: () => void
    onOpenSettings: () => void
    onStateChange: (id: string, controls: SessionPageControls) => void
}

/** 隐藏页面只改变展示，保留其 Hook、输入草稿和事件订阅；工作台采用全幅规整单/双面板结构与 T2 齐平底栏。 */
export function SessionWorkspace({
    active,
    projectName,
    sessionsCount,
    projectsCount,
    modalOpen,
    onOpenProject,
    onOpenSettings,
    onStateChange,
    ...options
}: Props) {
    const page = useSessionPage(options)
    const inputRef = useRef<HTMLTextAreaElement>(null)
    const [deckCollapsed, setDeckCollapsed] = useState(false)
    const title = page.session?.title || options.title || ''
    const { busy, refreshing, models, close, status, queuedCount, subagentSummary, selectAgent } = page
    const sessionID = options.sessionID

    const subagents = useMemo(
        () => page.agents.filter((item) => item.Kind === 'subagent'),
        [page.agents],
    )
    const hasSubagents = subagents.length > 0
    const showDeck = hasSubagents && !deckCollapsed
    const rightView: 'overview' | string = page.selectedAgentID && page.selectedAgentID !== sessionID
        ? page.selectedAgentID
        : 'overview'

    const openOverview = useCallback(() => {
        setDeckCollapsed(false)
        selectAgent(sessionID)
    }, [selectAgent, sessionID])

    const openSubagent = useCallback((id: string) => {
        if (id === sessionID) {
            openOverview()
            return
        }
        setDeckCollapsed(false)
        selectAgent(id)
    }, [openOverview, selectAgent, sessionID])

    const closeDeck = useCallback(() => {
        setDeckCollapsed(true)
        selectAgent(sessionID)
    }, [selectAgent, sessionID])

    useEffect(() => {
        onStateChange(sessionID, {
            title,
            busy,
            refreshing,
            models,
            close,
            status,
            queuedCount,
            subagentSummary,
            openOverview,
        })
    }, [sessionID, title, busy, refreshing, models, close, status, queuedCount, subagentSummary, openOverview, onStateChange])

    const mainActive = Boolean(activeRunId(page.mainAgent))
    const sessionUsageDetails = page.sessionUsage.hasUsage
        ? formatUsageDetails(page.sessionUsage, '会话总计用量（主代理 + 全部子代理）')
        : ''

    return (
        <div className="session-workspace" hidden={!active}>
            {active && page.error && createPortal(
                <div className="error-banner" role="alert" aria-atomic="true" inert={modalOpen}>
                    <span className="error-banner-message">
                        <CircleAlert size={18} strokeWidth={1.8} aria-hidden="true" />
                        <span>{page.error}</span>
                    </span>
                    <div className="error-banner-actions">
                        {page.errorCode === API_ERROR_CODES.modelNotConfigured && (
                            <button className="error-banner-action" type="button" onClick={onOpenSettings}>打开设置</button>
                        )}
                        <button className="icon-button error-banner-close" type="button" title="关闭提示" aria-label="关闭错误提示" onClick={page.clearError}>
                            <X size={15} strokeWidth={1.8} aria-hidden="true" />
                        </button>
                    </div>
                </div>,
                document.body,
            )}

            <div className="workbench-sheets">
                <main className={`main-panel workbench-sheet ${showDeck ? 'main-sheet' : ''}`}>
                    {page.session && (
                        <div className="workbench-sheet-head">
                            <div className="workbench-sheet-head-left">
                                <span className={`deck-dot ${mainActive ? 'is-live' : 'is-done'}`} aria-hidden="true" />
                                <strong className="workbench-sheet-title">{page.mainAgent?.name || 'Main Agent'}</strong>
                                <span className="workbench-sheet-subtitle">主对话与任务编排</span>
                            </div>
                            <div className="workbench-sheet-head-right">
                                {projectName && <span className="workbench-sheet-meta">{projectName}</span>}
                                {hasSubagents && !showDeck && (
                                    <button className="quiet-button workbench-reopen-deck" type="button" onClick={openOverview}>
                                        <Activity size={13} strokeWidth={1.9} aria-hidden="true" />
                                        <span>展开总览 ({subagentSummary.done}/{subagentSummary.total})</span>
                                    </button>
                                )}
                            </div>
                        </div>
                    )}

                    <SessionPanel
                        loading={page.sessionLoading}
                        hasSession={Boolean(page.session)}
                        sessionsCount={sessionsCount}
                        projectsCount={projectsCount}
                        onOpenProject={onOpenProject}
                    >
                        {page.session && (
                            <AgentSessionPanel
                                submissionRevision={page.submissionRevision}
                                loading={page.mainLoading}
                                agent={page.mainAgent}
                                entries={page.mainEntries}
                                requests={page.mainRequests}
                                pendingApprovals={page.pendingApprovals}
                                outputStore={page.outputStore}
                                onOpenSubagent={openSubagent}
                                entriesError={page.mainEntriesError}
                                hasEarlier={page.mainHasEarlier}
                                historyLoading={page.mainHistoryLoading}
                                onLoadEarlier={page.loadMainEarlier}
                                onRetryHistory={page.retryMainHistory}
                                busy={page.busy}
                                composer={page.mainAgent ? (
                                    <>
                                        <div className="command-band agent-input-accessory">
                                            <TaskQueue
                                                tasks={page.session.tasks}
                                                paused={page.session.queuePaused}
                                                busy={page.busy}
                                                onCancel={page.cancelQueuedTask}
                                                onChangePaused={page.changeQueue}
                                            />
                                        </div>
                                        <AgentTaskInput
                                            inputRef={inputRef}
                                            sessionId={page.session.id}
                                            approvalMode={page.approvalMode}
                                            onApprovalModeSaved={page.updateApprovalMode}
                                            active={mainActive}
                                            canQueue={page.canQueue}
                                            input={page.input}
                                            setInput={page.setInput}
                                            models={page.models}
                                            selectedProviderID={page.selectedProviderID}
                                            selectedModelID={page.selectedModelID}
                                            reasoning={page.reasoning}
                                            busy={page.busy}
                                            onSend={() => void page.submitInput()}
                                            onStop={() => void page.stopMain()}
                                            onModelChange={page.selectModel}
                                            onReasoningChange={page.selectReasoning}
                                            commands={WORKSPACE_COMMANDS}
                                            onCommand={page.runCommand}
                                        />
                                    </>
                                ) : (
                                    <PrimaryAgentTaskInput
                                        inputRef={inputRef}
                                        input={page.input}
                                        setInput={page.setInput}
                                        busy={page.busy}
                                        onSend={() => void page.submitInput()}
                                        commands={WORKSPACE_COMMANDS}
                                        onCommand={page.runCommand}
                                    />
                                )}
                            />
                        )}
                    </SessionPanel>

                    {page.session && (
                        <footer className="workbench-sheet-footer">
                            <span className="workbench-footer-hint">
                                {page.canQueue ? 'Enter 加入待办队列 · Shift+Enter 换行' : 'Enter 发送 · Shift+Enter 换行'}
                            </span>
                            {page.sessionUsage.hasUsage && (
                                <div className="usage-inline" title={sessionUsageDetails} aria-label={sessionUsageDetails}>
                                    <span>会话总计 <strong>{formatCompactCount(page.sessionUsage.totalTokens)}</strong></span>
                                    {page.sessionUsage.hasInput && <span>输入 {formatCompactCount(page.sessionUsage.inputTokens)}</span>}
                                    {page.sessionUsage.hasOutput && <span>输出 {formatCompactCount(page.sessionUsage.outputTokens)}</span>}
                                    {page.sessionUsage.cacheRate !== null && (
                                        <span className="usage-cache-rate">缓存 {page.sessionUsage.cacheRate}%</span>
                                    )}
                                </div>
                            )}
                        </footer>
                    )}
                </main>

                {showDeck && page.session && (
                    <SubagentDeck
                        subagents={subagents}
                        activeView={rightView}
                        inspectedSubagent={page.subagent}
                        inspectedEntries={page.subagentEntries}
                        inspectedRequests={page.subagentRequests}
                        pendingApprovals={page.pendingApprovals}
                        inspectedLoading={page.subagentLoading}
                        inspectedHistoryLoading={page.subagentHistoryLoading}
                        inspectedHasEarlier={page.subagentHasEarlier}
                        inspectedEntriesError={page.subagentEntriesError}
                        agentEntries={page.agentEntries}
                        agentUsages={page.agentUsages}
                        outputStore={page.outputStore}
                        queuedCount={page.queuedCount}
                        queuePaused={page.session.queuePaused}
                        busy={page.busy}
                        modalOpen={modalOpen}
                        onSelectOverview={openOverview}
                        onSelectSubagent={openSubagent}
                        onCloseDeck={closeDeck}
                        onStopSubagent={() => void page.stopSubagent()}
                        onLoadEarlier={page.loadSubagentEarlier}
                        onRetryHistory={page.retrySubagentHistory}
                    />
                )}
            </div>

            {active && <Toast message={page.error ? '' : page.commandNotice} onDismiss={page.clearCommandNotice} />}
        </div>
    )
}
