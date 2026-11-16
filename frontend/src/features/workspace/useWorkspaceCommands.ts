import { useCallback, useEffect, useRef, useState } from 'react'
import { reloadConfig } from '../../api'
import { readableError } from '../../shared/errors'

export const WORKSPACE_COMMANDS = [
    { name: '/new', description: '在当前项目中新建会话' },
    { name: '/reload', description: '重新加载当前会话配置，清理未开始的待办，需先结束执行' },
] as const

type Options = {
    sessionID: string
    projectID: string
    createNewSession: (projectID: string) => Promise<boolean>
    refreshModels: () => Promise<unknown>
    setInput: (value: string) => void
}
export function useWorkspaceCommands({ sessionID, projectID, createNewSession, refreshModels, setInput }: Options) {
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const [commandNotice, setCommandNotice] = useState('')
    const pending = useRef(false)
    const generation = useRef(0)
    useEffect(() => () => { generation.current += 1 }, [])
    const clearCommandNotice = useCallback(() => setCommandNotice(''), [])
    const runCommand = useCallback(async (command: string) => {
        if (pending.current) return false
        if (!WORKSPACE_COMMANDS.some((item) => item.name === command)) { setError(`未知命令：${command}`); return false }
        pending.current = true
        const request = ++generation.current
        setBusy(true); setError(''); setCommandNotice('')
        try {
            if (command === '/new') return await createNewSession(projectID)
            await reloadConfig(sessionID)
            await refreshModels()
            if (request === generation.current) { setInput(''); setCommandNotice('当前会话配置已重新加载') }
            return true
        } catch (reason) { if (request === generation.current) setError(readableError(reason)); return false }
        finally { pending.current = false; if (request === generation.current) setBusy(false) }
    }, [createNewSession, projectID, refreshModels, sessionID, setInput])
    return { busy, error, runCommand, commandNotice, clearCommandNotice }
}
