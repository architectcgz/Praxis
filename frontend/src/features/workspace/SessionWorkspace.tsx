import { useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import { ArrowLeft, Bot, CircleAlert, Square, X } from 'lucide-react'
import { Toast } from '../../components/ui'
import { API_ERROR_CODES } from '../../api'
import { AgentSessionPanel } from '../agent/AgentSessionPanel'
import { AgentTaskInput } from '../agent/AgentTaskInput'
import { PrimaryAgentTaskInput } from '../agent/PrimaryAgentTaskInput'
import { AgentsPanel } from '../agent/AgentsPanel'
import { SessionPanel } from '../session/SessionPanel'
import { TaskQueue } from '../task/TaskQueue'
import { WORKSPACE_COMMANDS } from './useWorkspaceCommands'
import { useSessionPage, type SessionPageOptions } from './useSessionPage'

export type SessionPageControls = Pick<ReturnType<typeof useSessionPage>, 'busy' | 'refreshing' | 'models' | 'refresh' | 'close' | 'status' | 'queuedCount'> & { title: string }
type Props = SessionPageOptions & {
    active: boolean
    sessionsCount: number
    projectsCount: number
    modalOpen: boolean
    onOpenProject: () => void
    onOpenSettings: () => void
    onStateChange: (id: string, controls: SessionPageControls) => void
}

/** 隐藏页面只改变展示，保留其 Hook、输入草稿和事件订阅。 */
export function SessionWorkspace({ active, sessionsCount, projectsCount, modalOpen, onOpenProject, onOpenSettings, onStateChange, ...options }: Props) {
    const page = useSessionPage(options)
    const inputRef = useRef<HTMLTextAreaElement>(null)
    const title = page.session?.title || options.title || ''
    const { busy, refreshing, models, refresh, close, status, queuedCount } = page
    useEffect(() => {
        onStateChange(options.sessionID, { title, busy, refreshing, models, refresh, close, status, queuedCount })
    }, [options.sessionID, title, busy, refreshing, models, refresh, close, status, queuedCount, onStateChange])

    return <div className="session-workspace" hidden={!active}>
        <main className="main-panel">
            {active && page.error && createPortal(
                <div className="error-banner has-tabs" role="alert" aria-atomic="true" inert={modalOpen}>
                    <span className="error-banner-message"><CircleAlert size={18} strokeWidth={1.8} aria-hidden="true" /><span>{page.error}</span></span>
                    <div className="error-banner-actions">
                        {page.errorCode === API_ERROR_CODES.modelNotConfigured && <button className="error-banner-action" type="button" onClick={onOpenSettings}>打开设置</button>}
                        <button className="icon-button error-banner-close" type="button" title="关闭提示" aria-label="关闭错误提示" onClick={page.clearError}>
                            <X size={15} strokeWidth={1.8} aria-hidden="true" />
                        </button>
                    </div>
                </div>, document.body,
            )}
            <SessionPanel loading={page.sessionLoading} hasSession={Boolean(page.session)} sessionsCount={sessionsCount} projectsCount={projectsCount} onOpenProject={onOpenProject}>
                {page.session && <AgentSessionPanel
                    submissionRevision={page.submissionRevision}
                    loading={page.agentLoading}
                    agents={page.agents}
                    agent={page.agent}
                    entries={page.entries}
                    requests={page.requests}
                    agentEntries={page.agentEntries}
                    agentEntriesErrors={page.agentEntriesErrors}
                    outputStore={page.outputStore}
                    onSelectAgent={page.selectAgent}
                    onStop={() => void page.stop()}
                    entriesError={page.entriesError}
                    hasEarlier={page.hasEarlier}
                    historyLoading={page.historyLoading}
                    onLoadEarlier={page.loadEarlier}
                    onRetryHistory={page.retryHistory}
                    busy={page.busy}
                    composer={page.agent?.Kind === 'subagent' ? <div className="command-band">
                        <div className="subagent-composer-bar">
                            <span className="subagent-composer-copy">
                                <Bot size={15} strokeWidth={1.8} aria-hidden="true" />
                                <span>子代理只接收主代理通过工具委派或补充的指令，不直接从输入框接收任务。</span>
                            </span>
                            <div className="subagent-composer-actions">
                                <button className="quiet-button" type="button" onClick={() => page.selectAgent(page.session!.id)}>
                                    <ArrowLeft size={14} strokeWidth={1.8} aria-hidden="true" />
                                    <span>返回主代理</span>
                                </button>
                                {page.agent.currentRunId && (
                                    <button className="subagent-stop-button" type="button" disabled={page.busy} onClick={() => void page.stop()}>
                                        <Square size={12} strokeWidth={2} aria-hidden="true" />
                                        <span>停止子代理</span>
                                    </button>
                                )}
                            </div>
                        </div>
                    </div> : page.agent ? <>
                        <div className="command-band agent-input-accessory">
                            <TaskQueue tasks={page.session.tasks} paused={page.session.queuePaused} busy={page.busy} onCancel={page.cancelQueuedTask} onChangePaused={page.changeQueue} />
                        </div>
                        <AgentTaskInput
                            inputRef={inputRef}
                            sessionId={page.session.id}
                            usage={page.usage}
                            active={Boolean(page.agent.currentRunId)}
                            canQueue={page.canQueue}
                            input={page.input}
                            setInput={page.setInput}
                            models={page.models}
                            selectedProviderID={page.selectedProviderID}
                            selectedModelID={page.selectedModelID}
                            reasoning={page.reasoning}
                            busy={page.busy}
                            onSend={() => void page.submitInput()}
                            onStop={() => void page.stop()}
                            onModelChange={page.selectModel}
                            onReasoningChange={page.selectReasoning}
                            commands={WORKSPACE_COMMANDS}
                            onCommand={page.runCommand}
                        />
                    </> : <PrimaryAgentTaskInput
                        inputRef={inputRef}
                        input={page.input}
                        setInput={page.setInput}
                        busy={page.busy}
                        onSend={() => void page.submitInput()}
                        commands={WORKSPACE_COMMANDS}
                        onCommand={page.runCommand}
                    />}
                />}
            </SessionPanel>
        </main>
        <AgentsPanel
            loading={page.sessionLoading}
            session={page.session ? { title, agents: page.agents } : null}
            agentEntries={page.agentEntries}
            selectedAgentID={page.selectedAgentID}
            onSelectAgent={page.selectAgent}
        />
        {active && <Toast message={page.error ? '' : page.commandNotice} onDismiss={page.clearCommandNotice} hasTabs />}

    </div>
}
