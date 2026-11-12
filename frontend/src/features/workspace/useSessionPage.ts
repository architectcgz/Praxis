import { useCallback, useEffect, useRef, useState } from 'react'
import { API_ERROR_CODES, isBindingAvailable, subscribeAgentEvents, subscribeSessionConfigReloaded, type ApiErrorCode, type SessionSnapshot } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'
import { useAgentData } from '../agent/useAgentData'
import { useAgentCommands } from '../agent/useAgentCommands'
import { useSessionData } from '../session/useSessionData'
import { useBranchNavigation } from '../session/useBranchNavigation'
import { useTaskSubmission } from '../task/useTaskSubmission'
import { useModelCatalog } from '../setting/model-config/useModelCatalog'
import { useWorkspaceCommands } from './useWorkspaceCommands'

export type SessionPageOptions = {
    sessionID: string
    title?: string
    createNewSession: (projectID: string) => Promise<boolean>
    refreshCatalog: (manual?: boolean, throwOnError?: boolean) => Promise<void>
}

/** 页面实例拥有会话状态和订阅，是否显示不改变其生命周期。 */
export function useSessionPage({ sessionID, title, createNewSession, refreshCatalog }: SessionPageOptions) {
    const [busy, setBusy] = useState(false)
    const [submissionRevision, setSubmissionRevision] = useState(0)
    const [error, setError] = useState('')
    const [errorCode, setErrorCode] = useState<ApiErrorCode | undefined>()
    const viewRequestRef = useRef(0)
    const { session, sessionLoading, sessionError, loadSession, releaseSession, adoptSession, bindSubmission, updateTitle } = useSessionData(sessionID)
    const { models, loading: modelsLoading, error: modelsError, refreshModels } = useModelCatalog(session?.id || '')
    const { loadForSession, ...agents } = useAgentData({ sessionID: session?.id || '', models, setError, setErrorCode })

    const initialize = useCallback(async () => {
        try {
            const value = await loadSession()
            if (value) await loadForSession(value)
        } catch (reason) {
            setError(readableError(reason))
            setErrorCode(apiErrorCode(reason))
        }
    }, [loadSession, loadForSession])

    useEffect(() => {
        void initialize()
        return () => { viewRequestRef.current += 1 }
    }, [initialize])
    useEffect(() => { if (title !== undefined) updateTitle(title) }, [title, updateTitle])

    const applySession = useCallback(async (value: SessionSnapshot) => {
        adoptSession(value)
        await loadForSession(value)
    }, [adoptSession, loadForSession])
    const branch = useBranchNavigation({ session, applySession, setError })
    const controls = useAgentCommands({
        agent: agents.agent, busy, getSelectedAgentID: agents.getSelectedAgentID,
        setBusy, setError, setErrorCode, viewRequestRef,
    })
    const submission = useTaskSubmission({
        agent: agents.agent, busy, input: agents.input,
        sessionID, bindingID: session?.bindingId || '',
        prepareSubmission: branch.prepareSubmission, bindSubmission,
        getSelectedAgentID: agents.getSelectedAgentID,
        setBusy, setError, setErrorCode, setInput: agents.setInput, viewRequestRef,
    })
    const commands = useWorkspaceCommands({
        createBranch: branch.createBranch, switchBranch: branch.switchBranch, showBranches: branch.showBranches,
        session, bridgeAvailable: isBindingAvailable(), busy, createNewSession,
        refreshCatalog, refreshModels, viewRequestRef,
        selectedAgentID: agents.selectedAgentID, getSelectedAgentID: agents.getSelectedAgentID,
        setBusy, setError, setErrorCode, setInput: agents.setInput,
    })

    useEffect(() => subscribeAgentEvents((event) => {
        if (event.kind === 'error' && event.sessionId === sessionID && event.branchId === session?.branch.id) {
            setError(event.data.message)
            setErrorCode(undefined)
        }
    }), [sessionID, session?.branch.id])
    useEffect(() => subscribeSessionConfigReloaded((id) => {
        if (id === sessionID) void refreshModels(false)
    }), [sessionID, refreshModels])
    useEffect(() => {
        if (agents.modelSelectionReady && errorCode === API_ERROR_CODES.modelNotConfigured) { setError(''); setErrorCode(undefined) }
    }, [agents.modelSelectionReady, errorCode])

    const refresh = useCallback(async () => {
        if (!session) { await initialize(); return }
        await refreshModels(false)
    }, [session, initialize, refreshModels])
    const close = useCallback(async () => {
        setBusy(true)
        try {
            await releaseSession()
            return true
        } catch (reason) {
            setError(readableError(reason)); setErrorCode(apiErrorCode(reason))
            return false
        } finally { setBusy(false) }
    }, [releaseSession])
    const submitInput = async () => {
        if (agents.input.trim().startsWith('/')) {
            await commands.runCommand(agents.input.trim())
        } else if (await submission.submitTask()) {
            branch.returnToHead()
            setSubmissionRevision((value) => value + 1)
        }
    }
    const dataError = sessionError ?? modelsError
    return {
        ...agents, ...branch, ...controls, ...commands,
        session, sessionLoading, models,
        busy: busy || agents.modelSelectionPending, refreshing: modelsLoading,
        error: error || (dataError === undefined ? '' : readableError(dataError)),
        errorCode: error ? errorCode : apiErrorCode(dataError),
        submitInput, submissionRevision, canQueue: submission.canQueue, cancelQueuedTask: submission.cancelQueuedTask,
        refresh, close,
    }
}
