import { useCallback, useEffect, useRef, useState } from 'react'
import { stopAgent, type AgentView } from '../../api'
import { readableError } from '../../shared/errors'

/** 停止入口捕获具体 RunID，操作反馈不替代后端执行状态。 */
export function useRunControl(agent: AgentView | null) {
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const generation = useRef(0)
    const pending = useRef(false)
    useEffect(() => { generation.current += 1; setError(''); return () => { generation.current += 1 } }, [agent?.ID])
    const stop = useCallback(async () => {
        if (!agent?.currentRunId || pending.current) return
        const request = ++generation.current
        pending.current = true
        setBusy(true); setError('')
        try { await stopAgent({ sessionId: agent.SessionID, agentId: agent.ID, runId: agent.currentRunId }) }
        catch (reason) { if (request === generation.current) setError(readableError(reason)) }
        finally { pending.current = false; setBusy(false) }
    }, [agent])
    return { stop, busy, error }
}
