import { useCallback, useEffect, useRef, useState, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { reloadConfig, type ApiErrorCode, type SessionSnapshot } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'

export const WORKSPACE_COMMANDS = [
    { name: '/new', description: '在当前项目中新建会话' },
    { name: '/reload', description: '重新加载 Praxis 配置' },
] as const

type CommandContext = {
    session: SessionSnapshot | null
    bridgeAvailable: boolean
    busy: boolean
    createNewSession: (projectID: string, workspaceID: string) => Promise<boolean>
    refreshCatalog: (manual?: boolean, throwOnError?: boolean) => Promise<void>
    viewRequestRef: RefObject<number>
    selectedAgentIDRef: RefObject<string>
    setBusy: Dispatch<SetStateAction<boolean>>
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    setInput: (input: string) => void
}

/** 统一处理工作区命令；命令不进入 Agent 消息流。 */
export function useWorkspaceCommands({ session, bridgeAvailable, busy, createNewSession, refreshCatalog, viewRequestRef, selectedAgentIDRef, setBusy, setError, setErrorCode, setInput }: CommandContext) {
    const running = useRef(false)
    const [commandNotice, setCommandNotice] = useState('')
    const clearCommandNotice = useCallback(() => setCommandNotice(''), [])
    useEffect(() => setCommandNotice(''), [session?.id, selectedAgentIDRef.current])

    const runCommand = useCallback(async (name: string): Promise<boolean> => {
        if (busy || running.current) return false
        if (!WORKSPACE_COMMANDS.some((command) => command.name === name)) {
            setError(`未知命令：${name}`)
            setErrorCode(undefined)
            return false
        }
        if (!bridgeAvailable || !session) {
            setError('当前会话不可用，请先选择一个项目和会话。')
            setErrorCode(undefined)
            return false
        }
        running.current = true
        const viewID = viewRequestRef.current
        const agentID = selectedAgentIDRef.current
        const isCurrentView = () => viewRequestRef.current === viewID && selectedAgentIDRef.current === agentID
        setCommandNotice('')
        try {
            if (name === '/new') {
                return await createNewSession(session.projectId, session.workspaceId)
            }
            setBusy(true)
            setError('')
            setErrorCode(undefined)
            try {
                await reloadConfig()
                await refreshCatalog(true, true)
                if (isCurrentView()) {
                    setInput('')
                    setCommandNotice('Praxis 配置已重新加载')
                }
                return true
            } catch (err) {
                if (isCurrentView()) {
                    setError(readableError(err))
                    setErrorCode(apiErrorCode(err))
                }
                return false
            } finally {
                setBusy(false)
            }
        } finally {
            running.current = false
        }
    }, [bridgeAvailable, busy, createNewSession, refreshCatalog, session, viewRequestRef, selectedAgentIDRef, setBusy, setError, setErrorCode, setInput])

    return { runCommand, commandNotice, clearCommandNotice }
}
