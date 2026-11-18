import { useCallback, useEffect, useState } from 'react'
import { activeRunId } from '../../api'
import { readableError } from '../../shared/errors'
import { useAgentData } from '../agent/useAgentData'
import { useRunControl } from '../run/useRunControl'
import { useSessionData } from '../session/useSessionData'
import { createSessionStore } from '../session/sessionStore'
import { useTaskSubmission } from '../task/useTaskSubmission'
import { useModelCatalog } from '../session/useModelCatalog'
import { useWorkspaceCommands } from './useWorkspaceCommands'

export type SessionPageOptions = {
    sessionID: string
    title?: string
    createNewSession: (projectID: string) => Promise<boolean>
    refreshCatalog: (manual?: boolean, throwOnError?: boolean) => Promise<void>
}
/** 页面只组合身份、动作和操作反馈，不持有其他 Hook 的请求资源。 */
export function useSessionPage({ sessionID, title, createNewSession }: SessionPageOptions) {
    const [store] = useState(() => createSessionStore(sessionID))
    const data = useSessionData(store)
    const { session, updateTitle } = data
    const catalog = useModelCatalog(session?.id || '')
    const { refreshModels } = catalog
    const agents = useAgentData({ store, models: catalog.models })
    const controls = useRunControl(sessionID, activeRunId(agents.agent))
    const main = agents.agents.find((agent) => agent.Kind === 'main')
    const submission = useTaskSubmission({ sessionID, input: agents.input, mainActive: Boolean(activeRunId(main)), setInput: agents.setInput })
    const commands = useWorkspaceCommands({ sessionID, projectID: session?.projectId || '', createNewSession, refreshModels, setInput: agents.setInput })
    const [submissionRevision, setSubmissionRevision] = useState(0)
    useEffect(() => { if (title !== undefined) updateTitle(title) }, [title, updateTitle])
    useEffect(() => { if (data.configRevision) void refreshModels(false) }, [data.configRevision, refreshModels])
    const submitInput = async () => {
        if (agents.input.trim().startsWith('/')) await commands.runCommand(agents.input.trim())
        else if (await submission.submitTask()) setSubmissionRevision((current) => current + 1)
    }
    const operationError = submission.error || controls.error || commands.error
    const error = operationError || agents.selectionError || (data.sessionError ? readableError(data.sessionError) : '') || (catalog.error ? readableError(catalog.error) : '')
    const clearSubmissionError = submission.clearError
    const clearControlError = controls.clearError
    const clearCommandError = commands.clearError
    const clearSelectionError = agents.clearSelectionError
    const clearError = useCallback(() => {
        clearSubmissionError()
        clearControlError()
        clearCommandError()
        clearSelectionError()
    }, [clearSubmissionError, clearControlError, clearCommandError, clearSelectionError])
    const status = agents.agents.some((item) => item.state === 'waiting') ? 'waiting'
        : agents.agents.some((item) => item.state === 'executing') ? 'executing'
        : main?.state === 'failed' ? 'failed'
        : main?.state === 'interrupted' ? 'interrupted'
        : 'idle'
    const queuedCount = session?.tasks.length ?? 0
    return {
        ...agents,
        session, sessionLoading: data.sessionLoading, models: catalog.models,
        busy: submission.busy || controls.busy || commands.busy || data.closing || agents.modelSelectionPending,
        refreshing: catalog.loading,
        status, queuedCount,
        error, errorCode: !operationError && agents.selectionError ? agents.selectionErrorCode : undefined,
        clearError,
        submitInput, submissionRevision,
        canQueue: submission.canQueue, cancelQueuedTask: submission.cancelQueuedTask, changeQueue: submission.changeQueue,
        stop: controls.stop,
        runCommand: commands.runCommand, commandNotice: commands.commandNotice, clearCommandNotice: commands.clearCommandNotice,
        close: data.releaseSession,
    }
}
