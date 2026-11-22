import { useCallback, useEffect, useMemo, useState } from 'react'
import { activeRunId } from '../../api'
import { readableError } from '../../shared/errors'
import { useAgentData } from '../agent/useAgentData'
import { useRunControl } from '../run/useRunControl'
import { useSessionData } from '../session/useSessionData'
import { createSessionStore } from '../session/sessionStore'
import { useTaskSubmission } from '../task/useTaskSubmission'
import { useWorkspaceCommands } from './useWorkspaceCommands'

export type SessionPageOptions = {
    sessionID: string
    title?: string
    createNewSession: (projectID: string) => Promise<boolean>
    refreshCatalog: (manual?: boolean, throwOnError?: boolean) => Promise<void>
}

export type SubagentProgressSummary = {
    total: number
    done: number
    live: number
    waiting: number
    failed: number
}

/** 页面只组合身份、动作和操作反馈，不持有其他 Hook 的请求资源。 */
export function useSessionPage({ sessionID, title, createNewSession }: SessionPageOptions) {
    const [store] = useState(() => createSessionStore(sessionID))
    const data = useSessionData(store)
    const { session, updateTitle } = data
    const agents = useAgentData({ store })
    const mainControl = useRunControl(sessionID, activeRunId(agents.mainAgent))
    const subagentControl = useRunControl(sessionID, activeRunId(agents.subagent))
    const submission = useTaskSubmission({
        sessionID,
        input: agents.input,
        mainActive: Boolean(activeRunId(agents.mainAgent)),
        setInput: agents.setInput,
    })
    const commands = useWorkspaceCommands({
        sessionID,
        projectID: session?.projectId || '',
        createNewSession,
        setInput: agents.setInput,
    })
    const [submissionRevision, setSubmissionRevision] = useState(0)

    useEffect(() => { if (title !== undefined) updateTitle(title) }, [title, updateTitle])

    const submitInput = async () => {
        if (agents.input.trim().startsWith('/')) await commands.runCommand(agents.input.trim())
        else if (await submission.submitTask()) setSubmissionRevision((current) => current + 1)
    }

    const operationError = submission.error || mainControl.error || subagentControl.error || commands.error
    const error = operationError || agents.selectionError || (data.sessionError ? readableError(data.sessionError) : '')
    const clearSubmissionError = submission.clearError
    const clearMainControlError = mainControl.clearError
    const clearSubagentControlError = subagentControl.clearError
    const clearCommandError = commands.clearError
    const clearSelectionError = agents.clearSelectionError
    const clearError = useCallback(() => {
        clearSubmissionError()
        clearMainControlError()
        clearSubagentControlError()
        clearCommandError()
        clearSelectionError()
    }, [clearSubmissionError, clearMainControlError, clearSubagentControlError, clearCommandError, clearSelectionError])

    const status = agents.agents.some((item) => item.state === 'waiting') ? 'waiting'
        : agents.agents.some((item) => item.state === 'executing') ? 'executing'
        : agents.mainAgent?.state === 'failed' ? 'failed'
        : agents.mainAgent?.state === 'interrupted' ? 'interrupted'
        : 'idle'

    const subagentSummary = useMemo<SubagentProgressSummary>(() => {
        const subagents = agents.agents.filter((item) => item.Kind === 'subagent')
        let done = 0
        let live = 0
        let waiting = 0
        let failed = 0
        for (const item of subagents) {
            if (item.state === 'waiting') waiting += 1
            else if (item.state === 'executing') live += 1
            else if (item.state === 'failed' || item.state === 'interrupted') failed += 1
            else done += 1
        }
        return { total: subagents.length, done, live, waiting, failed }
    }, [agents.agents])

    const queuedCount = session?.tasks.length ?? 0

    return {
        ...agents,
        session,
        sessionLoading: data.sessionLoading,
        busy: submission.busy || mainControl.busy || subagentControl.busy || commands.busy || data.closing || agents.modelSelectionPending,
        refreshing: data.sessionLoading,
        status,
        queuedCount,
        subagentSummary,
        error,
        errorCode: !operationError && agents.selectionError ? agents.selectionErrorCode : undefined,
        clearError,
        submitInput,
        submissionRevision,
        canQueue: submission.canQueue,
        cancelQueuedTask: submission.cancelQueuedTask,
        changeQueue: submission.changeQueue,
        stopMain: mainControl.stop,
        stopSubagent: subagentControl.stop,
        runCommand: commands.runCommand,
        commandNotice: commands.commandNotice,
        clearCommandNotice: commands.clearCommandNotice,
        close: data.releaseSession,
    }
}
