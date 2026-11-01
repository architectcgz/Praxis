import { useEffect } from 'react'
import { createPortal } from 'react-dom'
import { CircleAlert } from 'lucide-react'
import { Toast } from '../../components/ui'
import { API_ERROR_CODES } from '../../api'
import { AgentSessionPanel } from '../agent/AgentSessionPanel'
import { AgentsPanel } from '../agent/AgentsPanel'
import { SessionPanel } from '../session/SessionPanel'
import { BranchDialog, BranchStatus } from '../session/BranchNavigation'
import { TaskQueue } from '../task/TaskQueue'
import { WORKSPACE_COMMANDS } from './useWorkspaceCommands'
import { useSessionPage, type SessionPageOptions } from './useSessionPage'

export type SessionPageControls = Pick<ReturnType<typeof useSessionPage>, 'busy' | 'refreshing' | 'models' | 'refresh' | 'close'> & { title: string }
type Props = SessionPageOptions & {
    active: boolean
    sessionsCount: number
    modalOpen: boolean
    onOpenSettings: () => void
    onStateChange: (id: string, controls: SessionPageControls) => void
}

/** 隐藏页面只改变展示，保留其 Hook、输入草稿和事件订阅。 */
export function SessionWorkspace({ active, sessionsCount, modalOpen, onOpenSettings, onStateChange, ...options }: Props) {
    const page = useSessionPage(options)
    const title = page.session?.title || options.title || ''
    const { busy, refreshing, models, refresh, close } = page
    useEffect(() => {
        onStateChange(options.sessionID, { title, busy, refreshing, models, refresh, close })
    }, [options.sessionID, title, busy, refreshing, models, refresh, close, onStateChange])

    return <div className="session-workspace" hidden={!active}>
        <main className="main-panel">
            {active && page.error && createPortal(
                <div className="error-banner" role="alert" aria-atomic="true" inert={modalOpen}>
                    <span className="error-banner-message"><CircleAlert size={18} strokeWidth={1.8} aria-hidden="true" /><span>{page.error}</span></span>
                    {page.errorCode === API_ERROR_CODES.modelNotConfigured && <button className="error-banner-action" type="button" onClick={onOpenSettings}>打开设置</button>}
                </div>, document.body,
            )}
            <SessionPanel loading={page.sessionLoading} hasSession={Boolean(page.session)} sessionsCount={sessionsCount}>
                {page.session && <AgentSessionPanel
                    branchId={page.session.branch.id}
                    loading={page.agentLoading}
                    agents={page.agents}
                    agent={page.agent}
                    history={page.browsing && page.agent?.Kind === 'main' ? page.browsing.entries : page.history}
                    agentHistories={page.agentHistories}
                    agentHistoryErrors={page.agentHistoryErrors}
                    outputStore={page.outputStore}
                    onSelectAgent={page.selectAgent}
                    historyError={page.historyError}
                    input={page.input}
                    setInput={page.setInput}
                    models={page.models}
                    selectedProviderID={page.selectedProviderID}
                    selectedModelID={page.selectedModelID}
                    reasoning={page.reasoning}
                    busy={page.busy}
                    canQueue={page.canQueue}
                    beforeInput={<>
                        <BranchStatus branch={page.session.branch} browsing={Boolean(page.browsing)} busy={page.busy} onReturn={page.returnToHead} />
                        <TaskQueue tasks={page.session.tasks} busy={page.busy} onCancel={page.cancelQueuedTask} />
                    </>}
                    onSend={page.submitInput}
                    onControl={(kind) => void page.control(kind)}
                    onModelChange={page.selectModel}
                    onReasoningChange={page.selectReasoning}
                    commands={WORKSPACE_COMMANDS}
                    onCommand={page.runCommand}
                />}
            </SessionPanel>
        </main>
        <AgentsPanel
            loading={page.sessionLoading}
            session={page.session ? { title, agents: page.agents } : null}
            histories={page.agentHistories}
            selectedAgentID={page.selectedAgentID}
            onSelectAgent={page.selectAgent}
        />
        {active && <Toast message={page.error ? '' : page.commandNotice} onDismiss={page.clearCommandNotice} />}
        {active && page.branchDialog && page.session && <BranchDialog
            key={`${page.session.branch.id}-${page.branchDialog}`}
            kind={page.branchDialog}
            branches={page.branches}
            tree={page.tree}
            currentBranchId={page.session.branch.id}
            onSwitch={page.switchBranch}
            onBrowse={page.browseHistory}
            onClose={page.closeBranchDialog}
        />}
    </div>
}
