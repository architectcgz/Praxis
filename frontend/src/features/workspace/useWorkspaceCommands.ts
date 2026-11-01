import { useCallback, useEffect, useRef, useState, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { reloadConfig, type ApiErrorCode, type SessionSnapshot } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'

export const WORKSPACE_COMMANDS = [
    { name: '/new', description: '在当前项目中新建会话' },
    { name: '/reload', description: '重新加载当前会话配置，需先结束执行' },
    { name: '/branch', description: '从当前位置创建并切换分支，可附名称' },
    { name: '/switch', description: '选择分支，可附分支名称' },
    { name: '/tree', description: '浏览历史树，从历史位置独立继续' },
] as const

type CommandContext = {
    createBranch: (name?: string) => Promise<SessionSnapshot>
    switchBranch: (name: string) => Promise<void>
    showBranches: (kind: 'branches' | 'tree') => Promise<void>
    session: SessionSnapshot | null
    bridgeAvailable: boolean
    busy: boolean
    createNewSession: (projectID: string) => Promise<boolean>
    refreshCatalog: (manual?: boolean, throwOnError?: boolean) => Promise<void>
    refreshModels: () => Promise<unknown>
    viewRequestRef: RefObject<number>
    selectedAgentID: string
    getSelectedAgentID: () => string
    setBusy: Dispatch<SetStateAction<boolean>>
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    setInput: (input: string) => void
}

/** 统一处理工作区命令；命令不进入 Agent 消息流。 */
export function useWorkspaceCommands({ createBranch, switchBranch, showBranches, session, bridgeAvailable, busy, createNewSession, refreshCatalog, refreshModels, viewRequestRef, selectedAgentID, getSelectedAgentID, setBusy, setError, setErrorCode, setInput }: CommandContext) {
    const running = useRef(false)
    const [commandNotice, setCommandNotice] = useState('')
    const clearCommandNotice = useCallback(() => setCommandNotice(''), [])
    useEffect(() => setCommandNotice(''), [session?.id, selectedAgentID])

    const runCommand = useCallback(async (name: string): Promise<boolean> => {
        if (busy || running.current) return false
        const parsed = name.trim().match(/^(\/\S+)(?:\s+([\s\S]*))?$/)
        const command = parsed?.[1] || ''
        const argument = parsed?.[2]?.trim() || ''
        if (!WORKSPACE_COMMANDS.some((item) => item.name === command) || argument && command !== '/branch' && command !== '/switch') {
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
        const agentID = getSelectedAgentID()
        const isCurrentView = () => viewRequestRef.current === viewID && getSelectedAgentID() === agentID
        setCommandNotice('')
        try {
            if (command === '/new') {
                return await createNewSession(session.projectId)
            }
            setBusy(true)
            setError('')
            setErrorCode(undefined)
            try {
                if (command === '/branch') {
                    const next = await createBranch(argument)
                    if (isCurrentView()) { setInput(''); setCommandNotice(`已切换到分支 ${next.branch.name}`) }
                    return true
                }
                if (command === '/switch' || command === '/tree') {
                    if (command === '/switch' && argument) await switchBranch(argument)
                    else await showBranches(command === '/tree' ? 'tree' : 'branches')
                    if (isCurrentView()) setInput('')
                    return true
                }
                await reloadConfig(session.id)
                if (!isCurrentView()) return true
                await Promise.all([refreshCatalog(true, true), refreshModels()])
                if (isCurrentView()) {
                    setInput('')
                    setCommandNotice('当前会话配置已重新加载，其他会话保持不变')
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
    }, [bridgeAvailable, busy, createNewSession, createBranch, switchBranch, showBranches, refreshCatalog, refreshModels, session, viewRequestRef, getSelectedAgentID, setBusy, setError, setErrorCode, setInput])

    return { runCommand, commandNotice, clearCommandNotice }
}
